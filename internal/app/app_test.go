package app_test

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/gitctx"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/ports"
	"github.com/moeritze/harbormaster/internal/registry"
)

func getenvMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// StateDir must never touch the real filesystem: none of these branches
// call os.UserHomeDir's fallback path in a way that reads real state.
func TestStateDirHarbormasterHomeSet(t *testing.T) {
	getenv := getenvMap(map[string]string{"HARBORMASTER_HOME": "/custom/hm"})
	if got := app.StateDir(getenv); got != "/custom/hm" {
		t.Fatalf("got %q", got)
	}
}

func TestStateDirXDGStateHomeSet(t *testing.T) {
	getenv := getenvMap(map[string]string{"XDG_STATE_HOME": "/xdg/state"})
	want := filepath.Join("/xdg/state", "harbormaster")
	if got := app.StateDir(getenv); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStateDirNeitherSet(t *testing.T) {
	getenv := getenvMap(map[string]string{})
	got := app.StateDir(getenv)
	if got == "" {
		t.Fatal("expected a non-empty default state dir")
	}
	if filepath.Base(got) != "harbormaster" {
		t.Fatalf("got %q, want it to end in harbormaster", got)
	}
}

func TestClockUsesInjectedNow(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a := &app.App{Now: func() time.Time { return fixed }}
	if got := a.Clock(); !got.Equal(fixed) {
		t.Fatalf("got %v want %v", got, fixed)
	}
}

func TestClockFallsBackWhenNowNil(t *testing.T) {
	a := &app.App{}
	if got := a.Clock(); got.IsZero() {
		t.Fatal("expected a non-zero fallback time")
	}
}

func TestWorktreeKeyPrefersGitWorktree(t *testing.T) {
	a := &app.App{Git: gitctx.Context{Worktree: "/wt/a"}, Cwd: "/somewhere/else"}
	if got := a.WorktreeKey(); got != "/wt/a" {
		t.Fatalf("got %q", got)
	}
}

func TestWorktreeKeyFallsBackToCwd(t *testing.T) {
	a := &app.App{Cwd: "/somewhere/else"}
	if got := a.WorktreeKey(); got != "/somewhere/else" {
		t.Fatalf("got %q", got)
	}
}

type fakeProber struct{ listening map[int]bool }

func (f *fakeProber) PidAlive(int) bool        { return true }
func (f *fakeProber) PortListening(p int) bool { return f.listening[p] }

func TestMyPortResolvesAndSkipsForeign(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	pr := &fakeProber{listening: map[int]bool{}}
	dir := filepath.Join(t.TempDir(), "hm")
	st, err := registry.Open(dir, pr, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	a := &app.App{
		Store:  st,
		Prober: pr,
		Ident:  ident.Identity{Agent: "claude", Session: "s1"},
		Git:    gitctx.Context{Worktree: "/wt/a"},
		Ports:  ports.Config{Base: 3000, Range: 10},
		Cwd:    "/wt/a",
	}
	p1, err := a.MyPort()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := a.MyPort()
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatalf("expected stable port, got %d then %d", p1, p2)
	}

	// Occupy the deterministic port with a foreign entry; MyPort must move on.
	if err := st.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries, registry.Entry{Port: p1, PID: 99, Session: "other", StartedAt: now})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p3, err := a.MyPort()
	if err != nil {
		t.Fatal(err)
	}
	if p3 == p1 {
		t.Fatalf("expected MyPort to skip the foreign-held port %d", p1)
	}
}

// TestNewWrapsConfigErrorsAsUsage: a bad HARBORMASTER_BASE is something the
// caller can fix, so it must reach main as ErrUsage (exit 3), not as a
// registry failure (exit 4).
func TestNewWrapsConfigErrorsAsUsage(t *testing.T) {
	getenv := getenvMap(map[string]string{
		"HARBORMASTER_HOME": filepath.Join(t.TempDir(), "hm"),
		"HARBORMASTER_BASE": "abc",
	})
	if _, err := app.New(getenv, io.Discard, io.Discard); err == nil {
		t.Fatal("expected an error for HARBORMASTER_BASE=abc")
	} else if !errors.Is(err, app.ErrUsage) {
		t.Fatalf("%v is not app.ErrUsage", err)
	} else if !strings.Contains(err.Error(), "HARBORMASTER_BASE") {
		t.Fatalf("wrapping lost the cause: %v", err)
	}
}
