package hooks_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	core *hooks.Core
	st   *registry.Store
	now  time.Time
	log  *bytes.Buffer
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
	return &h{core: c, st: st, now: now, log: log}
}

func (x *h) seed(t *testing.T, e registry.Entry) {
	t.Helper()
	if e.StartedAt.IsZero() {
		e.StartedAt = x.now.Add(-5 * time.Minute)
	}
	if err := x.st.Update(func(f *registry.File) error { f.Entries = append(f.Entries, e); return nil }); err != nil {
		t.Fatal(err)
	}
}

func ev(kind hooks.Kind, session, cmd string) hooks.Event { //nolint:unparam // brief's fixed test-harness signature; session happens to be "me" in every case in this file but documents which field callers are setting
	return hooks.Event{Kind: kind, Agent: "claude", Session: session, Cwd: "/wt/x", Command: cmd}
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

func TestPreShellBlindKillGetsContextNotDeny(t *testing.T) {
	x := newH(t)
	x.seed(t, registry.Entry{ID: "a", Port: 3100, PID: 41, Agent: "claude", Session: "other", Label: "api"})
	r := x.core.Handle(ev(hooks.PreShell, "me", "pkill -f node"))
	if r.Decision != hooks.Allow || !strings.Contains(r.Context, "3100") {
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
	r := x.core.Handle(ev(hooks.SessionEnd, "me", ""))
	if r.Decision != hooks.Allow {
		t.Fatalf("%+v", r)
	}
	f, _ := x.st.Peek()
	if len(f.Entries) != 1 || f.Entries[0].ID != "theirs" {
		t.Fatalf("%+v", f.Entries)
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
