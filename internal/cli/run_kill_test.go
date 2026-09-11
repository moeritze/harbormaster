package cli_test

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

func TestRunRefusesForeignPort(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3000, PID: 11, Session: "s2", Worktree: "/wt/b", Label: "theirs"})
	err := h.run("run", "--port", "3000", "--", "true")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "theirs") {
		t.Fatalf("code %d err %v", c, err)
	}
}

func TestRunRefusesUnregisteredListener(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.prober.listening[3000] = true
	h.app.PidOnPort = func(int) (int, string, bool) { return 555, "node", true }
	err := h.run("run", "--port", "3000", "--", "true")
	if c := exitCode(err); c != 2 || !strings.Contains(err.Error(), "555") {
		t.Fatalf("code %d err %v", c, err)
	}
}

// TestRunRefusesDuplicateOwnRegistration covers Finding 1: a second `run` on
// a port this same session already registered must be refused, not appended
// as a second entry.
func TestRunRefusesDuplicateOwnRegistration(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "mine", Port: 3000, PID: 123, Session: "s1"})
	err := h.run("run", "--port", "3000", "--", "true")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("code %d err %v", c, err)
	}
}

func TestRunRequiresCommand(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if c := exitCode(h.run("run", "--port", "3000")); c != 3 {
		t.Fatalf("code %d", c)
	}
}

func TestKillForeignDeniedOwnAllowed(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3001, PID: 11, Session: "s2"})
	if c := exitCode(h.run("kill", "3001")); c != 1 {
		t.Fatalf("foreign should be denied, got %d", c)
	}
	if c := exitCode(h.run("kill", "3999")); c != 2 {
		t.Fatalf("unregistered free port should be exit 2, got %d", c)
	}
}

// TestKillOwnEntryTerminatesRealProcess exercises terminateEntry end-to-end
// (runner.Guard + runner.Terminate) against a real child process, since those
// only do anything meaningful against a live pid.
func TestKillOwnEntryTerminatesRealProcess(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	// Reap in the background as soon as the signal lands, otherwise the
	// process sits as a zombie (still "alive" to kill(pid, 0)) until this
	// test calls Wait, which would make Terminate spin for its full timeout.
	waitDone := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitDone)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-waitDone
	})
	h.seed(t, registry.Entry{ID: "own", Port: 3999, PID: pid, Session: "s1"})

	if err := h.run("kill", "3999"); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !strings.Contains(h.out.String(), "killed pid") {
		t.Fatalf("output %q", h.out.String())
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("entry not removed: %+v", f.Entries)
	}
	hist, _ := h.app.Store.History(0)
	if len(hist) != 1 || hist[0].Reason != "killed" {
		t.Fatalf("history %+v", hist)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && syscall.Kill(pid, 0) == nil {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("process still alive")
	}
}

// TestResolveRunPortUsesPortEnv covers resolveRunPort's $PORT branch (via the
// real os.Getenv behind the package's getenv indirection).
func TestResolveRunPortUsesPortEnv(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	t.Setenv("PORT", "4321")
	if err := h.run("run", "--", "sh", "-c", "exit 0"); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestRunUsesDeterministicPortByDefault covers resolveRunPort's fallback to
// a.MyPort() when neither --port nor $PORT is set.
func TestRunUsesDeterministicPortByDefault(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if err := h.run("run", "--", "sh", "-c", "exit 0"); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestRunPropagatesExitCode covers newRun's non-zero exit path end-to-end
// with a real child process.
func TestRunPropagatesExitCode(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	err := h.run("run", "--port", "3999", "--", "sh", "-c", "exit 3")
	if c := exitCode(err); c != 3 {
		t.Fatalf("code %d err %v", c, err)
	}
}
