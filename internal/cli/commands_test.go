package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/cli"
	"github.com/moeritze/harbormaster/internal/gitctx"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/ports"
	"github.com/moeritze/harbormaster/internal/registry"
)

type fakeProber struct {
	alive, listening map[int]bool
	// allAlive answers PidAlive for pids the test cannot know up front,
	// such as a child `run` spawned.
	allAlive bool
}

func (f *fakeProber) PidAlive(p int) bool      { return f.allAlive || f.alive[p] }
func (f *fakeProber) PortListening(p int) bool { return f.listening[p] }

type harness struct {
	app    *app.App
	out    *bytes.Buffer
	prober *fakeProber
	now    time.Time
}

//nolint:unparam // worktree is a fixed "/wt/a" for most of this suite; kept as a param for clarity at each call site
func newHarness(t *testing.T, session, worktree string) *harness {
	t.Helper()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	pr := &fakeProber{alive: map[int]bool{}, listening: map[int]bool{}}
	dir := filepath.Join(t.TempDir(), "hm")
	st, err := registry.Open(dir, pr, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	a := &app.App{
		Stdout: out, Stderr: out,
		Now:   func() time.Time { return now },
		Store: st, Prober: pr,
		Ident:     ident.Identity{Agent: "claude", Session: session, HostUser: "m"},
		Git:       gitctx.Context{Repo: "/r", Worktree: worktree, Branch: "feat"},
		Ports:     ports.Config{Base: 3000, Range: 1000},
		Cwd:       worktree,
		PidOnPort: func(int) (int, string, bool) { return 0, "", false },
	}
	return &harness{app: a, out: out, prober: pr, now: now}
}

func (h *harness) run(args ...string) error {
	h.out.Reset()
	root := cli.NewRoot(h.app)
	root.SetArgs(args)
	return root.Execute()
}

func (h *harness) seed(t *testing.T, e registry.Entry) {
	t.Helper()
	h.prober.alive[e.PID] = true
	h.prober.listening[e.Port] = true
	if e.StartedAt.IsZero() {
		e.StartedAt = h.now.Add(-12 * time.Minute)
	}
	if err := h.app.Store.Update(func(f *registry.File) error { f.Entries = append(f.Entries, e); return nil }); err != nil {
		t.Fatal(err)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*cli.ExitError); ok {
		return ee.Code
	}
	return -1
}

func TestLsEmptyAndTable(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if err := h.run("ls"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "no registered servers") {
		t.Fatalf("%q", h.out.String())
	}
	_ = h.run("ls", "--json")
	if got := strings.TrimSpace(h.out.String()); got != "[]" {
		t.Fatalf("empty ls --json = %q, want %q", got, "[]")
	}
	h.seed(t, registry.Entry{ID: "x", Port: 3000, PID: 10, Agent: "claude", Session: "s2", Worktree: "/wt/b", Branch: "other", Label: "login flow"})
	_ = h.run("ls")
	s := h.out.String()
	for _, want := range []string{"3000", "claude", "/wt/b", "login flow", "12m"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
	_ = h.run("ls", "--json")
	var rows []registry.Entry
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("json: %v %s", err, h.out.String())
	}
}

func TestPortIsDeterministic(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	_ = h.run("port")
	first := strings.TrimSpace(h.out.String())
	_ = h.run("port")
	if first != strings.TrimSpace(h.out.String()) || first == "" {
		t.Fatal("port not stable")
	}
}

func TestCheckExitCodes(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if c := exitCode(h.run("check", "3000")); c != 0 {
		t.Fatalf("free: %d", c)
	}
	if !strings.Contains(h.out.String(), "port 3000: free") {
		t.Fatalf("free check should print one stdout line, got %q", h.out.String())
	}
	h.seed(t, registry.Entry{ID: "mine", Port: 3000, PID: 10, Session: "s1"})
	if c := exitCode(h.run("check", "3000")); c != 0 {
		t.Fatalf("own: %d", c)
	}
	if !strings.Contains(h.out.String(), "port 3000: own") {
		t.Fatalf("own check should print one stdout line, got %q", h.out.String())
	}
	h.seed(t, registry.Entry{ID: "theirs", Port: 3001, PID: 11, Session: "s2", Worktree: "/wt/b", Label: "x"})
	err := h.run("check", "3001")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "s2") {
		t.Fatalf("foreign: %d %v", c, err)
	}
	// The ExitError is the whole answer; main prints it once, on stderr.
	if h.out.Len() != 0 {
		t.Fatalf("non-zero check must not also print to stdout: %q", h.out.String())
	}
	// --json always prints the document and carries the code with no message.
	err = h.run("check", "3001", "--json")
	var ee *cli.ExitError
	if !errors.As(err, &ee) || ee.Code != 1 || ee.Msg != "" {
		t.Fatalf("foreign --json: %#v", err)
	}
	var res struct {
		Status string `json:"status"`
	}
	if e := json.Unmarshal(h.out.Bytes(), &res); e != nil || res.Status != "foreign" {
		t.Fatalf("json: %v %s", e, h.out.String())
	}
	h.app.PidOnPort = func(p int) (int, string, bool) { return 999, "node", p == 3002 }
	h.prober.listening[3002] = true
	if c := exitCode(h.run("check", "3002")); c != 2 {
		t.Fatalf("unregistered: %d", c)
	}
	if c := exitCode(h.run("check", "abc")); c != 3 {
		t.Fatalf("usage: %d", c)
	}
	if c := exitCode(h.run("check", "3000abc")); c != 3 {
		t.Fatalf("usage (trailing garbage): %d", c)
	}
}

func TestClaimAndRelease(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.prober.alive[42] = true
	h.prober.listening[3005] = true
	h.app.PidOnPort = func(p int) (int, string, bool) { return 42, "node", p == 3005 }
	if err := h.run("claim", "3005", "--pid", "42", "--label", "manual"); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	if !strings.Contains(out, "claimed port 3005") || !strings.Contains(out, "pid 42") {
		t.Fatalf("claim output %q", out)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 1 || f.Entries[0].PID != 42 || f.Entries[0].Session != "s1" || f.Entries[0].Label != "manual" {
		t.Fatalf("%+v", f.Entries)
	}
	if c := exitCode(h.run("claim", "3005", "--pid", "42")); c != 1 {
		t.Fatalf("double claim should be denied, got %d", c)
	}
	if err := h.run("release", "3005"); err != nil {
		t.Fatal(err)
	}
	f, _ = h.app.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatal("not released")
	}
	hist, _ := h.app.Store.History(0)
	if len(hist) != 1 || hist[0].Reason != "released" {
		t.Fatalf("history %+v", hist)
	}
}

func TestClaimJSON(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.prober.alive[43] = true
	h.prober.listening[3006] = true
	h.app.PidOnPort = func(p int) (int, string, bool) { return 43, "node", p == 3006 }
	if err := h.run("claim", "3006", "--pid", "43", "--json"); err != nil {
		t.Fatal(err)
	}
	var e registry.Entry
	if err := json.Unmarshal(h.out.Bytes(), &e); err != nil || e.Port != 3006 || e.PID != 43 {
		t.Fatalf("json: %v %s", err, h.out.String())
	}
}

func TestReleaseJSON(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "a", Port: 3001, PID: 11, Session: "s1"})
	if err := h.run("release", "3001", "--json"); err != nil {
		t.Fatal(err)
	}
	var rows []registry.Entry
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil || len(rows) != 1 || rows[0].Port != 3001 {
		t.Fatalf("json: %v %s", err, h.out.String())
	}

	if err := h.run("release", "--session", "nomatch", "--json"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(h.out.String()); got != "[]" {
		t.Fatalf("empty release --json = %q, want %q", got, "[]")
	}
}

func TestReleaseForeignDeniedUnlessForce(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3001, PID: 11, Session: "s2"})
	if c := exitCode(h.run("release", "3001")); c != 1 {
		t.Fatalf("got %d", c)
	}
	if err := h.run("release", "3001", "--force"); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseBySessionAndAllMine(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "a", Port: 3001, PID: 11, Session: "s1"})
	h.seed(t, registry.Entry{ID: "b", Port: 3002, PID: 12, Session: "s1"})
	h.seed(t, registry.Entry{ID: "c", Port: 3003, PID: 13, Session: "s2"})
	if err := h.run("release", "--all-mine"); err != nil {
		t.Fatal(err)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 1 || f.Entries[0].ID != "c" {
		t.Fatalf("%+v", f.Entries)
	}
	// Naming someone else's session id is not ownership: without --force the
	// release is denied and their entry stays put.
	if c := exitCode(h.run("release", "--session", "s2")); c != 1 {
		t.Fatalf("releasing another session should be denied, got %d", c)
	}
	f, _ = h.app.Store.Load()
	if len(f.Entries) != 1 || f.Entries[0].ID != "c" {
		t.Fatalf("foreign entry should survive: %+v", f.Entries)
	}
}

// TestReleaseOwnSessionSucceeds is the session-end hook's own call:
// `release --session <my id>` from the session that owns the entries.
func TestReleaseOwnSessionSucceeds(t *testing.T) {
	h := newHarness(t, "s2", "/wt/b")
	h.seed(t, registry.Entry{ID: "c", Port: 3003, PID: 13, Session: "s2", Worktree: "/wt/b"})
	if err := h.run("release", "--session", "s2"); err != nil {
		t.Fatal(err)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("%+v", f.Entries)
	}
}

func TestGcReportsPruned(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "dead", Port: 3001, PID: 11, Session: "s1"})
	h.prober.alive[11] = false
	if err := h.run("gc"); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	if !strings.Contains(out, "pruned 1") || !strings.Contains(out, "3001") || !strings.Contains(out, "pid_dead") {
		t.Fatalf("%q", out)
	}

	h.seed(t, registry.Entry{ID: "dead2", Port: 3002, PID: 12, Session: "s1"})
	h.prober.alive[12] = false
	if err := h.run("gc", "--json"); err != nil {
		t.Fatal(err)
	}
	var recs []registry.HistoryRecord
	if err := json.Unmarshal(h.out.Bytes(), &recs); err != nil || len(recs) != 1 || recs[0].Port != 3002 {
		t.Fatalf("json: %v %s", err, h.out.String())
	}
}

func TestHistoryLists(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "a", Port: 3001, PID: 11, Session: "s1"})
	_ = h.run("release", "3001")
	_ = h.run("history")
	if !strings.Contains(h.out.String(), "released") || !strings.Contains(h.out.String(), "3001") {
		t.Fatalf("%q", h.out.String())
	}
}

// TestClaimRefusesPidThatDoesNotOwnThePort covers H3: registering an
// arbitrary live pid against a port somebody else is listening on would let
// `kill` be pointed at an unrelated process.
func TestClaimRefusesPidThatDoesNotOwnThePort(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.prober.alive[42] = true
	h.prober.listening[3005] = true
	h.app.PidOnPort = func(p int) (int, string, bool) { return 99, "node", p == 3005 }

	err := h.run("claim", "3005", "--pid", "42")
	if c := exitCode(err); c != 3 || !strings.Contains(err.Error(), "is not listening on port 3005 (pid 99 is)") {
		t.Fatalf("code %d err %v", c, err)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("refused claim must not register: %+v", f.Entries)
	}

	if err := h.run("claim", "3005", "--pid", "42", "--force"); err != nil {
		t.Fatalf("--force should claim anyway: %v", err)
	}
	f, _ = h.app.Store.Load()
	if len(f.Entries) != 1 || f.Entries[0].PID != 42 {
		t.Fatalf("%+v", f.Entries)
	}
}

// TestClaimRefusesUnverifiablePid is the other half of H3: when PidOnPort
// cannot answer (lsof unavailable, or nothing listening) the claim is
// refused too, unless --force.
func TestClaimRefusesUnverifiablePid(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.prober.alive[42] = true
	// The harness default PidOnPort reports !ok for every port.
	err := h.run("claim", "3005", "--pid", "42")
	if c := exitCode(err); c != 3 || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("code %d err %v", c, err)
	}
	if err := h.run("claim", "3005", "--pid", "42", "--force"); err != nil {
		t.Fatalf("--force should claim anyway: %v", err)
	}
}

// TestKillWithoutSessionRequiresForce covers H7: a caller with no session id
// owns nothing for write commands, otherwise unsetting HARBORMASTER_SESSION
// would be a way around ownership.
func TestKillWithoutSessionRequiresForce(t *testing.T) {
	h := newHarness(t, "", "/wt/a")
	h.seed(t, registry.Entry{ID: "x", Port: 3001, PID: 11, Session: "s2", Worktree: "/wt/a"})

	err := h.run("kill", "3001")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "no session id in the environment") {
		t.Fatalf("code %d err %v", c, err)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 1 {
		t.Fatalf("entry must survive a refused kill: %+v", f.Entries)
	}
	// --force gets past the session rule; whatever happens next, it is not
	// the sessionless refusal any more.
	if err := h.run("kill", "3001", "--force"); err != nil && strings.Contains(err.Error(), "no session id") {
		t.Fatalf("--force still refused for the session reason: %v", err)
	}
}

// TestReleaseWithoutSessionRequiresForce is H7 for release, in both the port
// form and --all-mine.
func TestReleaseWithoutSessionRequiresForce(t *testing.T) {
	h := newHarness(t, "", "/wt/a")
	h.seed(t, registry.Entry{ID: "x", Port: 3001, PID: 11, Session: "s2", Worktree: "/wt/a"})

	err := h.run("release", "3001")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "no session id in the environment") {
		t.Fatalf("port form: code %d err %v", c, err)
	}
	err = h.run("release", "--all-mine")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "no session id in the environment") {
		t.Fatalf("--all-mine: code %d err %v", c, err)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 1 {
		t.Fatalf("entry must survive a refused release: %+v", f.Entries)
	}

	if err := h.run("release", "3001", "--force"); err != nil {
		t.Fatalf("--force should release: %v", err)
	}
	f, _ = h.app.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("%+v", f.Entries)
	}
}

// TestReleaseRegistryErrorExitsFour covers H8: a registry failure from a
// write command is exit 4, not the usage code main falls back to.
func TestReleaseRegistryErrorExitsFour(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "a", Port: 3001, PID: 11, Session: "s1"})

	reg := filepath.Join(h.app.Store.Dir(), "registry.json")
	if err := os.Remove(reg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere.json"), reg); err != nil {
		t.Fatal(err)
	}

	err := h.run("release", "3001")
	if c := exitCode(err); c != 4 || !strings.Contains(err.Error(), "registry:") {
		t.Fatalf("code %d err %v", c, err)
	}
}
