package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/gitctx"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

// TestDenyIfRegisteredCatchesOwnEntryRace covers Finding 1's race case
// directly: even when resolveRunPort saw the registry as empty, the
// authoritative check applied under the lock at register time must still
// refuse an own entry that shows up on the target port by then.
func TestDenyIfRegisteredCatchesOwnEntryRace(t *testing.T) {
	a := &app.App{
		Ident: ident.Identity{Agent: "claude", Session: "s1"},
		Git:   gitctx.Context{Worktree: "/wt/a"},
		Now:   func() time.Time { return time.Now() },
	}
	f := &registry.File{Entries: []registry.Entry{{ID: "race", Port: 3000, PID: 77, Session: "s1"}}}

	err := denyIfRegistered(a, f, 3000)
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != ExitDenied || !strings.Contains(ee.Msg, "already registered") {
		t.Fatalf("got %v", err)
	}
}

func TestDenyIfRegisteredAllowsFreePort(t *testing.T) {
	a := &app.App{Ident: ident.Identity{Agent: "claude", Session: "s1"}, Now: time.Now}
	f := &registry.File{}
	if err := denyIfRegistered(a, f, 3000); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestDenyIfRegisteredRefusesForeignEntry(t *testing.T) {
	a := &app.App{
		Ident: ident.Identity{Agent: "claude", Session: "s1"},
		Git:   gitctx.Context{Worktree: "/wt/a"},
		Now:   time.Now,
	}
	f := &registry.File{Entries: []registry.Entry{{ID: "x", Port: 3000, PID: 5, Session: "s2"}}}

	err := denyIfRegistered(a, f, 3000)
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != ExitDenied || strings.Contains(ee.Msg, "already registered") {
		t.Fatalf("got %v", err)
	}
}

// TestRunResultToErrUnwrapsExitError covers Finding 2: runner.Run wraps a
// register-time failure as a generic exit-4 error via fmt.Errorf("register:
// %w", err); the command must unwrap it to recover the original exit code
// (e.g. 1 for "already registered") instead of reporting exit 4.
func TestRunResultToErrUnwrapsExitError(t *testing.T) {
	inner := &ExitError{Code: ExitDenied, Msg: "port 3000 already registered by this session (pid 5); run `harbormaster kill 3000` first"}
	// Mirrors runner.Run's actual wrapping of a register error: fmt.Errorf("register: %w", err).
	wrapped := fmt.Errorf("register: %w", inner)

	got := runResultToErr(4, wrapped, "npm")
	ee, ok := got.(*ExitError)
	if !ok || ee != inner {
		t.Fatalf("got %v, want the unwrapped inner *ExitError", got)
	}
	if ee.Code != ExitDenied {
		t.Fatalf("code %d, want %d", ee.Code, ExitDenied)
	}
}

func TestRunResultToErrFallsBackForNonExitError(t *testing.T) {
	got := runResultToErr(4, errors.New("boom"), "npm")
	ee, ok := got.(*ExitError)
	if !ok || ee.Code != 4 || ee.Msg != "boom" {
		t.Fatalf("got %v", got)
	}
}

func TestRunResultToErrReportsNonZeroExit(t *testing.T) {
	got := runResultToErr(3, nil, "npm")
	ee, ok := got.(*ExitError)
	if !ok || ee.Code != 3 || ee.Msg != "npm exited with 3" {
		t.Fatalf("got %v", got)
	}
}

func TestRunResultToErrSuccess(t *testing.T) {
	if err := runResultToErr(0, nil, "npm"); err != nil {
		t.Fatalf("got %v", err)
	}
}
