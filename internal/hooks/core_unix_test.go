//go:build unix

package hooks_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/hooks"
	"github.com/moeritze/harbormaster/internal/registry"
)

// child is a real process this test started and owns. A direct child stays
// visible to kill(pid, 0) as a zombie after it dies, so "did it exit?" is
// answered by Wait, not by signalling.
type child struct {
	pid  int
	done chan struct{}
}

func startSleep(t *testing.T) *child {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a child process: %v", err)
	}
	c := &child{pid: cmd.Process.Pid, done: make(chan struct{})}
	go func() { _, _ = cmd.Process.Wait(); close(c.done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
		}
	})
	return c
}

// exited reports whether the child has ended within d.
func (c *child) exited(d time.Duration) bool {
	select {
	case <-c.done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestCursorSessionEndLeavesTheProcessRunning exercises the real
// runner.Terminate path (no injected stub): a Cursor conversation ending
// must release the registry entry and leave the server process alone.
func TestCursorSessionEndLeavesTheProcessRunning(t *testing.T) {
	for _, reason := range []string{"completed", "error", "user_close"} {
		x := newH(t)
		c := startSleep(t)
		x.seed(t, registry.Entry{ID: "mine", Port: 3100, PID: c.pid, Agent: "cursor", Session: "c1", Worktree: "/wt/x"})
		if r := x.core.Handle(hooks.Event{Kind: hooks.SessionEnd, Agent: "cursor", Session: "c1", Cwd: "/wt/x", Reason: reason}); r.Decision != hooks.Allow {
			t.Fatalf("%s: %+v", reason, r)
		}
		if f, _ := x.st.Peek(); len(f.Entries) != 0 {
			t.Fatalf("%s: entry must be released, got %+v", reason, f.Entries)
		}
		if c.exited(300 * time.Millisecond) {
			t.Fatalf("%s: Cursor session end killed pid %d", reason, c.pid)
		}
	}
}

// TestClaudeSessionEndStopsTheProcess is the contrast: Claude Code's session
// end really does stop what it started, through the same code path.
func TestClaudeSessionEndStopsTheProcess(t *testing.T) {
	x := newH(t)
	c := startSleep(t)
	x.seed(t, registry.Entry{ID: "mine", Port: 3100, PID: c.pid, Agent: "claude", Session: "me"})
	x.core.Handle(endEv("me", "logout"))
	if !c.exited(5 * time.Second) {
		t.Fatalf("claude session end must stop pid %d (log: %s)", c.pid, x.log.String())
	}
}
