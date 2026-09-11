package hooks_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/gitctx"
	"github.com/moeritze/harbormaster/internal/hooks"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/ports"
	"github.com/moeritze/harbormaster/internal/registry"
)

type fakeProber struct{ alive, listening map[int]bool }

func (f *fakeProber) PidAlive(p int) bool      { return f.alive[p] }
func (f *fakeProber) PortListening(p int) bool { return f.listening[p] }

type h struct {
	core   *hooks.Core
	st     *registry.Store
	now    time.Time
	log    *bytes.Buffer
	prober *fakeProber
	// git counts how often the core asked for a git context. The hot path
	// (a shell command that is not about a port) must never make it move.
	git *int
}

func newH(t *testing.T) *h {
	t.Helper()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	pr := &fakeProber{alive: map[int]bool{}, listening: map[int]bool{}}
	st, err := registry.Open(filepath.Join(t.TempDir(), "hm"), pr, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	a := &app.App{
		Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Now: func() time.Time { return now },
		Store: st, Prober: pr, Ident: ident.Identity{Agent: "human"}, Git: gitctx.Context{}, Ports: ports.Config{Base: 3000, Range: 1000}, Cwd: "/wt/x",
		PidOnPort: func(int) (int, string, bool) { return 0, "", false },
	}
	log := &bytes.Buffer{}
	c := hooks.New(a)
	c.Log = log
	c.KillTimeout = 200 * time.Millisecond
	gitCalls := 0
	c.GitDiscover = func(string) gitctx.Context { gitCalls++; return gitctx.Context{} }
	return &h{core: c, st: st, now: now, log: log, prober: pr, git: &gitCalls}
}

func (x *h) seed(t *testing.T, e registry.Entry) {
	t.Helper()
	if e.StartedAt.IsZero() {
		e.StartedAt = x.now.Add(-5 * time.Minute)
	}
	// Keep the pruner out of the way: these tests are about hook decisions,
	// not liveness.
	x.prober.alive[e.PID] = true
	x.prober.listening[e.Port] = true
	if err := x.st.Update(func(f *registry.File) error { f.Entries = append(f.Entries, e); return nil }); err != nil {
		t.Fatal(err)
	}
}

func ev(kind hooks.Kind, session, cmd string) hooks.Event { //nolint:unparam // brief's fixed test-harness signature; session happens to be "me" in every case in this file but documents which field callers are setting
	return hooks.Event{Kind: kind, Agent: "claude", Session: session, Cwd: "/wt/x", Command: cmd}
}

func endEv(session, reason string) hooks.Event { //nolint:unparam // same fixed harness shape as ev: the session is always "me" here, the reason is what varies
	return hooks.Event{Kind: hooks.SessionEnd, Agent: "claude", Session: session, Cwd: "/wt/x", Reason: reason}
}

func TestSessionStartContextListsServersAndPort(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 1, Agent: "claude", Session: "other", Worktree: "/wt/y", Label: "api"})
	r := x.core.Handle(ev(hooks.SessionStart, "me", ""))
	if r.Decision != hooks.Allow {
		t.Fatal("session start must allow")
	}
	for _, want := range []string{"3100", "other", "api", "harbormaster run", "this worktree"} {
		if !strings.Contains(r.Context, want) {
			t.Fatalf("missing %q in %q", want, r.Context)
		}
	}
}

func TestSessionStartEmptyRegistry(t *testing.T) {
	x := newH(t)
	r := x.core.Handle(ev(hooks.SessionStart, "me", ""))
	if !strings.Contains(r.Context, "no registered") {
		t.Fatalf("%q", r.Context)
	}
}

func TestPreShellDeniesKillOfForeignPort(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "other", Worktree: "/wt/y", Label: "api"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "lsof -ti:3100 | xargs kill"))
	if r.Decision != hooks.Deny || !strings.Contains(r.Reason, "3100") || !strings.Contains(r.Reason, "other") {
		t.Fatalf("%+v", r)
	}
}

func TestPreShellAllowsKillOfOwnPort(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "me"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "kill $(lsof -ti:3100)"))
	if r.Decision != hooks.Allow {
		t.Fatalf("%+v", r)
	}
}

func TestPreShellDeniesKillOfForeignPid(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 4242, Agent: "claude", Session: "other"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "kill -9 4242"))
	if r.Decision != hooks.Deny || !strings.Contains(r.Reason, "4242") {
		t.Fatalf("%+v", r)
	}
}

// TestPreShellBlindKillAsks: a kill with no port and no pid in its text may
// or may not hit somebody else's server. harbormaster cannot tell, so it
// hands the decision to the human ("ask") instead of allowing it -- and an
// allow would have been worse than useless here, since an allow that
// carried a permissionDecision would have skipped the prompt entirely.
func TestPreShellBlindKillAsks(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "other", Label: "api"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "pkill -f node"))
	if r.Decision != hooks.Ask || !strings.Contains(r.Reason, "3100") || r.Context != "" {
		t.Fatalf("%+v", r)
	}
}

// TestPreShellBlindKillWithNoForeignServersIsSilent guards the other half:
// nothing to protect means no prompt at all.
func TestPreShellBlindKillWithNoForeignServersIsSilent(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "me"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "pkill -f node"))
	if r.Decision != hooks.Allow || r.Reason != "" || r.Context != "" {
		t.Fatalf("%+v", r)
	}
}

func TestPreShellUnwrappedServerStartNudges(t *testing.T) {
	x := newH(t)
	r := x.core.Handle(ev(hooks.PreShell, "me", "npm run dev"))
	if r.Decision != hooks.Allow || !strings.Contains(r.Context, "harbormaster run") {
		t.Fatalf("%+v", r)
	}
}

func TestPreShellStrictDeniesUnwrappedServerStart(t *testing.T) {
	x := newH(t)
	x.core.Strict = true
	r := x.core.Handle(ev(hooks.PreShell, "me", "npm run dev"))
	if r.Decision != hooks.Deny || !strings.Contains(r.Reason, "harbormaster run") {
		t.Fatalf("%+v", r)
	}
}

func TestPreShellServerStartOnForeignPortDenied(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "other"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "PORT=3100 npm run dev"))
	if r.Decision != hooks.Deny || !strings.Contains(r.Reason, "3100") {
		t.Fatalf("%+v", r)
	}
}

func TestPreShellWrappedAndUnrelatedAllowSilently(t *testing.T) {
	x := newH(t)
	for _, cmd := range []string{"hm run -- npm run dev", "git status", "hm ls"} {
		r := x.core.Handle(ev(hooks.PreShell, "me", cmd))
		if r.Decision != hooks.Allow || r.Context != "" || r.Reason != "" {
			t.Fatalf("%q: %+v", cmd, r)
		}
	}
}

func TestPostShellUnwrappedServerStartNudges(t *testing.T) {
	x := newH(t)
	r := x.core.Handle(ev(hooks.PostShell, "me", "npm run dev &"))
	if r.Decision != hooks.Allow || !strings.Contains(r.Context, "hm ls") {
		t.Fatalf("%+v", r)
	}
}

func TestSessionEndReleasesOwnEntriesOnly(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "mine", Port: 3100, PID: 1, Agent: "claude", Session: "me"})
	x.seed(t, registry.Entry{ID: "theirs", Port: 3101, PID: 1, Agent: "claude", Session: "other"})
	r := x.core.Handle(endEv("me", "logout"))
	if r.Decision != hooks.Allow {
		t.Fatalf("%+v", r)
	}
	f, _ := x.st.Peek()
	if len(f.Entries) != 1 || f.Entries[0].ID != "theirs" {
		t.Fatalf("foreign entries must be untouched: %+v", f.Entries)
	}
	recs, err := x.st.History(20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rec := range recs {
		if rec.Port == 3100 {
			if rec.Reason != "session_end" {
				t.Fatalf("history reason %q, want session_end", rec.Reason)
			}
			found = true
		}
		if rec.Port == 3101 {
			t.Fatalf("foreign entry must not reach history: %+v", rec)
		}
	}
	if !found {
		t.Fatalf("no history record for the released entry: %+v", recs)
	}
}

// TestSessionEndKeepsServersOnClearAndResume: "clear" and "resume" do not
// end a session, they only swap its transcript. Releasing there killed the
// servers the very next prompt was about to use.
func TestSessionEndKeepsServersOnClearAndResume(t *testing.T) {
	for _, reason := range []string{"clear", "resume"} {
		x := newH(t)
		x.seed(t, registry.Entry{ID: "mine", Port: 3100, PID: 1, Agent: "claude", Session: "me"})
		if r := x.core.Handle(endEv("me", reason)); r.Decision != hooks.Allow {
			t.Fatalf("%s: %+v", reason, r)
		}
		f, _ := x.st.Peek()
		if len(f.Entries) != 1 {
			t.Fatalf("%s must release nothing, got %+v", reason, f.Entries)
		}
		if recs, _ := x.st.History(20); len(recs) != 0 {
			t.Fatalf("%s must write no history, got %+v", reason, recs)
		}
	}
}

// TestSessionEndOtherReasonsRelease covers the reasons that really do end a
// session.
func TestSessionEndOtherReasonsRelease(t *testing.T) {
	for _, reason := range []string{"logout", "prompt_input_exit", "other", ""} {
		x := newH(t)
		x.seed(t, registry.Entry{ID: "mine", Port: 3100, PID: 1, Agent: "claude", Session: "me"})
		if r := x.core.Handle(endEv("me", reason)); r.Decision != hooks.Allow {
			t.Fatalf("%s: %+v", reason, r)
		}
		f, _ := x.st.Peek()
		if len(f.Entries) != 0 {
			t.Fatalf("%q must release, got %+v", reason, f.Entries)
		}
	}
}

func TestErrorsFailOpenAndLog(t *testing.T) {
	x := newH(t)
	x.core.App.Store = nil // simulate a broken store
	r := x.core.Handle(ev(hooks.PreShell, "me", "kill 1"))
	if r.Decision != hooks.Allow || x.log.Len() == 0 {
		t.Fatalf("%+v log=%q", r, x.log.String())
	}
}

// TestPreShellDoesNotDiscoverGitOnTheHotPath: gitctx.Discover forks git
// three times. The PreToolUse hook runs before every shell command a
// session issues, so neither the silent-allow path nor a deny decided by
// session id may pay for it; session start, which prints this worktree's
// port, legitimately does.
func TestPreShellDoesNotDiscoverGitOnTheHotPath(t *testing.T) {
	x := newH(t)
	if r := x.core.Handle(ev(hooks.PreShell, "me", "git status")); r.Decision != hooks.Allow || r.Context != "" {
		t.Fatalf("%+v", r)
	}
	if *x.git != 0 {
		t.Fatalf("silent allow discovered git %d times", *x.git)
	}
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "other"})
	if r := x.core.Handle(ev(hooks.PreShell, "me", "kill -9 41")); r.Decision != hooks.Deny {
		t.Fatalf("%+v", r)
	}
	if *x.git != 0 {
		t.Fatalf("deny by session discovered git %d times", *x.git)
	}
	if r := x.core.Handle(ev(hooks.SessionStart, "me", "")); r.Decision != hooks.Allow {
		t.Fatalf("%+v", r)
	}
	if *x.git == 0 {
		t.Fatal("session start must resolve git to report this worktree's port")
	}
}

// TestOverrideTurnsDenyAndAskIntoAllow covers HARBORMASTER_HOOKS=0.
func TestOverrideTurnsDenyAndAskIntoAllow(t *testing.T) {
	const hint = " (override: set HARBORMASTER_HOOKS=0 in Claude Code's environment)"
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "other", Label: "api"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "lsof -ti:3100 | xargs kill"))
	if r.Decision != hooks.Deny || !strings.HasSuffix(r.Reason, hint) {
		t.Fatalf("every deny must advertise the override: %+v", r)
	}
	r = x.core.Handle(ev(hooks.PreShell, "me", "pkill -f node"))
	if r.Decision != hooks.Ask || !strings.HasSuffix(r.Reason, hint) {
		t.Fatalf("every ask must advertise the override: %+v", r)
	}
	x.core.Disabled = true
	r = x.core.Handle(ev(hooks.PreShell, "me", "lsof -ti:3100 | xargs kill"))
	if r.Decision != hooks.Allow || !strings.HasPrefix(r.Context, "harbormaster (override active): ") || !strings.Contains(r.Context, "3100") {
		t.Fatalf("override must allow with the reason as context: %+v", r)
	}
	if strings.Contains(r.Context, hint) {
		t.Fatalf("an active override must not still advertise itself: %q", r.Context)
	}
	r = x.core.Handle(ev(hooks.PreShell, "me", "pkill -f node"))
	if r.Decision != hooks.Allow || !strings.HasPrefix(r.Context, "harbormaster (override active): ") {
		t.Fatalf("ask must degrade too: %+v", r)
	}
}

func TestNewReadsOverrideFromTheEnvironment(t *testing.T) {
	t.Setenv("HARBORMASTER_HOOKS", "0")
	if c := hooks.New(nil); !c.Disabled {
		t.Fatal("HARBORMASTER_HOOKS=0 must disable the hooks")
	}
	t.Setenv("HARBORMASTER_HOOKS", "")
	if c := hooks.New(nil); c.Disabled {
		t.Fatal("hooks must be on by default")
	}
}

// TestSessionContextCapsTheTable keeps a machine with many registered
// servers from pushing a wall of mostly irrelevant rows into every new
// session's context.
func TestSessionContextCapsTheTable(t *testing.T) {
	x := newH(t)
	for i := range 35 {
		x.seed(t, registry.Entry{ID: fmt.Sprint(i), Port: 3100 + i, PID: 100 + i, Agent: "claude", Session: "other"})
	}
	r := x.core.Handle(ev(hooks.SessionStart, "me", ""))
	if !strings.Contains(r.Context, "… and 5 more (run harbormaster ls)") {
		t.Fatalf("missing the truncation line:\n%s", r.Context)
	}
	if strings.Contains(r.Context, "3134") {
		t.Fatalf("row past the cap was rendered:\n%s", r.Context)
	}
}

// cursorEv builds a Cursor hook event. Cursor's identity is the
// conversation id, so the session field is the same shape as Claude's.
func cursorEv(kind hooks.Kind, session, cwd, cmd string) hooks.Event {
	return hooks.Event{Kind: kind, Agent: "cursor", Session: session, Cwd: cwd, Command: cmd}
}

// TestCursorSessionEndReleasesButNeverTerminates covers the whole Cursor
// reason set. Cursor's sessionEnd fires once per conversation — "completed"
// is the ordinary end of a piece of work, and the per-turn event is `stop`,
// which harbormaster does not hook — so ending a conversation must never
// stop a dev server the user is still looking at.
func TestCursorSessionEndReleasesButNeverTerminates(t *testing.T) {
	for _, reason := range []string{"completed", "aborted", "error", "window_close", "user_close", ""} {
		x := newH(t)
		killed := 0
		x.core.Terminate = func(int, bool, time.Duration) error { killed++; return nil }
		x.seed(t, registry.Entry{ID: "mine", Port: 3100, PID: 4242, Agent: "cursor", Session: "c1", Worktree: "/wt/x"})
		if r := x.core.Handle(hooks.Event{Kind: hooks.SessionEnd, Agent: "cursor", Session: "c1", Cwd: "/wt/x", Reason: reason}); r.Decision != hooks.Allow {
			t.Fatalf("%q: %+v", reason, r)
		}
		f, _ := x.st.Peek()
		if len(f.Entries) != 0 {
			t.Fatalf("%q must release the entry, got %+v", reason, f.Entries)
		}
		recs, err := x.st.History(20)
		if err != nil {
			t.Fatal(err)
		}
		if len(recs) != 1 || recs[0].Reason != "session_end" {
			t.Fatalf("%q: history %+v", reason, recs)
		}
		if killed != 0 {
			t.Fatalf("%q: Cursor session end must not terminate anything (called %d times)", reason, killed)
		}
	}
}

// TestClaudeSessionEndStillTerminates is the other half: the injected
// Terminate is really the one the core calls, so the Cursor test above
// proves an absence and not a broken wiring.
func TestClaudeSessionEndStillTerminates(t *testing.T) {
	x := newH(t)
	var got []int
	x.core.Terminate = func(pid int, _ bool, _ time.Duration) error { got = append(got, pid); return nil }
	// A pid Guard accepts: alive, ours, and not this process itself.
	pid := os.Getppid()
	x.seed(t, registry.Entry{ID: "mine", Port: 3100, PID: pid, Agent: "claude", Session: "me"})
	x.core.Handle(endEv("me", "logout"))
	if len(got) != 1 || got[0] != pid {
		t.Fatalf("claude session end must terminate its own entries, got %v (log: %s)", got, x.log.String())
	}
}

// TestCursorOwnsHumanEntriesInTheSameWorktree covers the identity gap:
// Cursor's sessionStart env never reaches the agent's shell, so a server the
// Cursor agent starts with a plain `npm run dev` is registered as a human
// shell session. The conversation must still be allowed to manage it — but
// only in its own worktree, and never somebody else's agent session.
func TestCursorOwnsHumanEntriesInTheSameWorktree(t *testing.T) {
	cases := []struct {
		name    string
		entry   registry.Entry
		myTree  string
		wantDec hooks.Decision
	}{
		{"same worktree", registry.Entry{ID: "h", Port: 3100, PID: 41, Agent: "human", Session: "shell:4242", Worktree: "/wt/x"}, "/wt/x", hooks.Allow},
		{"other worktree", registry.Entry{ID: "h", Port: 3100, PID: 41, Agent: "human", Session: "shell:4242", Worktree: "/wt/y"}, "/wt/x", hooks.Deny},
		{"foreign agent, same worktree", registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "other", Worktree: "/wt/x"}, "/wt/x", hooks.Deny},
		{"human entry with no worktree", registry.Entry{ID: "h", Port: 3100, PID: 41, Agent: "human", Session: "shell:4242"}, "/wt/x", hooks.Deny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newH(t)
			x.core.GitDiscover = func(string) gitctx.Context { return gitctx.Context{Repo: "/r", Worktree: tc.myTree, Branch: "b"} }
			x.seed(t, tc.entry)
			r := x.core.Handle(cursorEv(hooks.PreShell, "c1", tc.myTree, "lsof -ti:3100 | xargs kill"))
			if r.Decision != tc.wantDec {
				t.Fatalf("got %v want %v (%q)", r.Decision, tc.wantDec, r.Reason)
			}
		})
	}
}

// TestClaudeDoesNotInheritTheCursorWorktreeRule: the relaxation is Cursor's
// alone; Claude Code's own env does carry the session id, so a human entry
// in the same worktree stays foreign there.
func TestClaudeDoesNotInheritTheCursorWorktreeRule(t *testing.T) {
	x := newH(t)
	x.core.GitDiscover = func(string) gitctx.Context { return gitctx.Context{Worktree: "/wt/x"} }
	x.seed(t, registry.Entry{ID: "h", Port: 3100, PID: 41, Agent: "human", Session: "shell:4242", Worktree: "/wt/x"})
	if r := x.core.Handle(ev(hooks.PreShell, "me", "lsof -ti:3100 | xargs kill")); r.Decision != hooks.Deny {
		t.Fatalf("%+v", r)
	}
}

// TestOverrideHintIsAgentAware: pointing a Cursor user at "set
// HARBORMASTER_HOOKS=0 in Claude Code's environment" is advice they cannot
// follow.
func TestOverrideHintIsAgentAware(t *testing.T) {
	const claudeHint = " (override: set HARBORMASTER_HOOKS=0 in Claude Code's environment)"
	const cursorHint = " (override: remove the harbormaster entries from ~/.cursor/hooks.json or launch Cursor with HARBORMASTER_HOOKS=0)"
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "other", Worktree: "/wt/y", Label: "api"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "lsof -ti:3100 | xargs kill"))
	if r.Decision != hooks.Deny || !strings.HasSuffix(r.Reason, claudeHint) {
		t.Fatalf("claude: %+v", r)
	}
	r = x.core.Handle(cursorEv(hooks.PreShell, "c1", "/wt/x", "lsof -ti:3100 | xargs kill"))
	if r.Decision != hooks.Deny || !strings.HasSuffix(r.Reason, cursorHint) {
		t.Fatalf("cursor: %+v", r)
	}
	r = x.core.Handle(cursorEv(hooks.PreShell, "c1", "/wt/x", "pkill -f node"))
	if r.Decision != hooks.Ask || !strings.HasSuffix(r.Reason, cursorHint) {
		t.Fatalf("cursor ask: %+v", r)
	}
}

func TestClipCapsOnARuneBoundary(t *testing.T) {
	if got := hooks.Clip("abc", 10); got != "abc" {
		t.Fatalf("%q", got)
	}
	long := strings.Repeat("ü", 4000) // 8000 bytes
	got := hooks.Clip(long, hooks.MaxMessage)
	if len(got) > hooks.MaxMessage || !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
		t.Fatalf("len %d valid %v: %q…", len(got), utf8.ValidString(got), got[:20])
	}
}
