//go:build unix

package cli_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/ports"
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
	h.seed(t, registry.Entry{ID: "own", Port: 3999, PID: pid, Session: "s1", Spawned: true})

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
	hist, _ := h.app.Store.History(0)
	if len(hist) != 1 || hist[0].Port != 4321 || hist[0].Reason != "exited" {
		t.Fatalf("expected the run to register port 4321 and log exited: %+v", hist)
	}
	if !strings.Contains(h.out.String(), "using $PORT=4321") {
		t.Fatalf("expected a warning that $PORT overrides the worktree port: %q", h.out.String())
	}
}

// TestRunUsesDeterministicPortByDefault covers resolveRunPort's fallback to
// a.MyPort() when neither --port nor $PORT is set.
func TestRunUsesDeterministicPortByDefault(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if err := h.run("run", "--", "sh", "-c", "exit 0"); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := ports.Deterministic("/wt/a", h.app.Ports)
	hist, _ := h.app.Store.History(0)
	if len(hist) != 1 || hist[0].Port != want {
		t.Fatalf("expected the run to register the deterministic port %d: %+v", want, hist)
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

// TestReleaseKillTerminatesRealProcess is the `release --kill` half of the
// session-end path: the entry goes away and the process actually dies.
func TestReleaseKillTerminatesRealProcess(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	// Reap as soon as the signal lands; an unreaped zombie still answers
	// kill(pid, 0) and would make Terminate spin for its whole timeout.
	waitDone := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitDone)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-waitDone
	})
	h.seed(t, registry.Entry{ID: "own", Port: 3998, PID: pid, Session: "s1", Spawned: true})

	if err := h.run("release", "3998", "--kill"); err != nil {
		t.Fatalf("release --kill: %v", err)
	}
	f, _ := h.app.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("entry not removed: %+v", f.Entries)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && syscall.Kill(pid, 0) == nil {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("process still alive after release --kill")
	}
}

// TestRunMarksEntrySpawned pins the flag that decides group signalling:
// only entries `run` created may later be killed as a process group.
func TestRunMarksEntrySpawned(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.prober.allAlive = true // the child's pid is not known up front
	gate := filepath.Join(t.TempDir(), "stop")
	done := make(chan error, 1)
	go func() {
		done <- h.run("run", "--port", "3997", "--",
			"sh", "-c", fmt.Sprintf("while [ ! -f %q ]; do sleep 0.05; done", gate))
	}()

	var got registry.Entry
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f, err := h.app.Store.Load()
		if err == nil && len(f.Entries) == 1 {
			got = f.Entries[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.PID == 0 {
		t.Fatal("run never registered an entry")
	}
	if !got.Spawned {
		t.Fatalf("entry registered by run is not marked spawned: %+v", got)
	}
}

// TestRunBannerRedactsSensitiveArgs covers H5: the banner harbormaster
// prints before starting the child must not echo a secret that was passed on
// the command line.
func TestRunBannerRedactsSensitiveArgs(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	if err := h.run("run", "--port", "3999", "--", "true", "--password=hunter2"); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := h.out.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("secret leaked to stderr: %q", out)
	}
	if !strings.Contains(out, "--password=***") {
		t.Fatalf("banner should show the redacted form: %q", out)
	}
}

// TestRunRejectsInvalidEnvNames covers H6.
func TestRunRejectsInvalidEnvNames(t *testing.T) {
	bad := []string{"BAD-NAME", "1PORT", "with space", "", "PATH", "HOME", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "LD_LIBRARY_PATH"}
	for _, n := range bad {
		h := newHarness(t, "s1", "/wt/a")
		err := h.run("run", "--port", "3999", "--env", n, "--", "true")
		if c := exitCode(err); c != 3 || !strings.Contains(err.Error(), "invalid --env name") {
			t.Fatalf("--env %q: code %d err %v", n, c, err)
		}
	}
	h := newHarness(t, "s1", "/wt/a")
	if err := h.run("run", "--port", "3999", "--env", "API_PORT", "--", "true"); err != nil {
		t.Fatalf("a valid name must be accepted: %v", err)
	}
}

// TestKillRefusesReusedPid: an entry whose recorded start time no longer
// matches the live process is a reused pid and must not be signalled.
func TestKillRefusesReusedPid(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.app.PidStartTime = func(int) (string, error) { return "Thu Sep 11 10:00:00 2026", nil }
	// The parent shell is a real process of our uid, so runner.Guard passes
	// and the start-time comparison is what refuses the kill.
	h.seed(t, registry.Entry{ID: "reused", Port: 3300, PID: os.Getppid(), Session: "s1", StartTime: "Mon Jan  1 00:00:00 2024"})
	err := h.run("kill", "3300")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "reused") {
		t.Fatalf("code %d err %v", c, err)
	}
	f, _ := h.app.Store.Peek()
	if len(f.Entries) != 1 {
		t.Fatalf("entry must survive a refused kill: %+v", f.Entries)
	}
}

// TestNewEntryRecordsStartTime: claim writes the process start time.
func TestNewEntryRecordsStartTime(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.app.PidStartTime = func(pid int) (string, error) { return "start-" + strconv.Itoa(pid), nil }
	h.app.PidOnPort = func(p int) (int, string, bool) { return 77, "node", p == 3301 }
	h.prober.alive[77] = true
	h.prober.listening[3301] = true
	if err := h.run("claim", "3301", "--pid", "77"); err != nil {
		t.Fatal(err)
	}
	f, _ := h.app.Store.Peek()
	if len(f.Entries) != 1 || f.Entries[0].StartTime != "start-77" {
		t.Fatalf("%+v", f.Entries)
	}
}

// TestKillRefusesGroupSignalForEntryWithoutStartTime and the backfill that
// repairs it: an entry with no recorded start time cannot be checked for pid
// reuse, and a process-GROUP signal against it could hit every process in
// whatever group holds that id now. `ls` prunes, pruning backfills, and the
// kill then goes through.
func TestKillRefusesGroupSignalForEntryWithoutStartTime(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.app.PidStartTime = func(int) (string, error) { return "t1", nil }
	h.app.Store.StartTimeOf = func(int) string { return "t1" }
	h.app.Terminate = func(int, bool, time.Duration) error { return nil }
	// The parent shell is a real process of our uid, so runner.Guard passes
	// and the missing start time is what decides.
	h.seed(t, registry.Entry{ID: "nostart", Port: 3310, PID: os.Getppid(), Session: "s1", Spawned: true})

	err := h.run("kill", "3310")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "no recorded start time") {
		t.Fatalf("code %d err %v", c, err)
	}
	if !strings.Contains(err.Error(), "harbormaster ls") {
		t.Fatalf("the refusal must name the repair: %v", err)
	}
	f, _ := h.app.Store.Peek()
	if len(f.Entries) != 1 {
		t.Fatalf("entry must survive a refused kill: %+v", f.Entries)
	}
	// The read that refused also repaired the row -- for the NEXT read.
	if f.Entries[0].StartTime != "t1" {
		t.Fatalf("the start time must have been backfilled and persisted: %+v", f.Entries)
	}

	// `harbormaster ls` is the step the message asks for; afterwards the
	// entry has a start time that predates the kill, and the kill works.
	if err := h.run("ls"); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if err := h.run("kill", "3310"); err != nil {
		t.Fatalf("kill must work once the start time is recorded: %v", err)
	}
	f, _ = h.app.Store.Peek()
	if len(f.Entries) != 0 {
		t.Fatalf("the entry must be gone after the kill: %+v", f.Entries)
	}
}

// TestKillRestoresEntryAfterFailedSignal: the entry is removed before the
// signal, so a signal that fails has to put it back -- the process is still
// running and the registry must keep saying so.
func TestKillRestoresEntryAfterFailedSignal(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.app.PidStartTime = func(int) (string, error) { return "t1", nil }
	h.app.Terminate = func(int, bool, time.Duration) error { return errors.New("operation not permitted") }
	h.seed(t, registry.Entry{ID: "own", Port: 3501, PID: os.Getppid(), Session: "s1", StartTime: "t1"})

	err := h.run("kill", "3501")
	if c := exitCode(err); c != 1 || !strings.Contains(err.Error(), "signal pid") {
		t.Fatalf("code %d err %v", c, err)
	}
	f, _ := h.app.Store.Peek()
	if len(f.Entries) != 1 || f.Entries[0].ID != "own" {
		t.Fatalf("the entry must be restored after a failed signal: %+v", f.Entries)
	}
}

// TestKillDoesNotRestoreOverAnotherEntry: if something else registered the
// port while the signal was being sent, that entry is the truth about the
// port. Restoring would leave two rows claiming one port, so the user gets a
// warning instead.
func TestKillDoesNotRestoreOverAnotherEntry(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.app.PidStartTime = func(int) (string, error) { return "t1", nil }
	h.app.Terminate = func(int, bool, time.Duration) error {
		// Someone else claims the port while the signal is in flight.
		_ = h.app.Store.Update(func(f *registry.File) error {
			f.Entries = append(f.Entries, registry.Entry{ID: "other", Port: 3502, PID: os.Getppid(), Session: "s2", StartedAt: h.now})
			return nil
		})
		return errors.New("operation not permitted")
	}
	h.seed(t, registry.Entry{ID: "own", Port: 3502, PID: os.Getppid(), Session: "s1", StartTime: "t1"})

	if c := exitCode(h.run("kill", "3502")); c != 1 {
		t.Fatalf("code %d", c)
	}
	f, _ := h.app.Store.Peek()
	if len(f.Entries) != 1 || f.Entries[0].ID != "other" {
		t.Fatalf("the newer entry must stand alone on the port: %+v", f.Entries)
	}
	if !strings.Contains(h.out.String(), "not restoring") {
		t.Fatalf("expected a warning that the entry was dropped: %q", h.out.String())
	}
}

// TestClaimWarnsWhenStartTimeCannotBeRecorded: registering without a start
// time silently turns the pid-reuse guard off for that entry, which the user
// has to be told about.
func TestClaimWarnsWhenStartTimeCannotBeRecorded(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	h.app.PidStartTime = func(int) (string, error) { return "", errors.New("ps not found") }
	h.app.PidOnPort = func(p int) (int, string, bool) { return 77, "node", p == 3303 }
	h.prober.alive[77] = true
	h.prober.listening[3303] = true
	if err := h.run("claim", "3303", "--pid", "77"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "pid-reuse protection is off for this entry") {
		t.Fatalf("expected a warning, got %q", h.out.String())
	}
	f, _ := h.app.Store.Peek()
	if len(f.Entries) != 1 || f.Entries[0].StartTime != "" {
		t.Fatalf("%+v", f.Entries)
	}
}

// TestStoredCmdKeepsTheWholeRedactedCommand: the stored cmd used to be cut
// at the 256-byte identifier cap, which hid the tail of any real command
// line -- including the very redaction markers that prove a secret was
// masked. It is capped at 2 KiB now, and marked only when it really is cut.
func TestStoredCmdKeepsTheWholeRedactedCommand(t *testing.T) {
	h := newHarness(t, "s1", "/wt/a")
	filler := strings.Repeat("a", 850)
	if err := h.run("run", "--port", "3996", "--", "true", filler, "--password=x"); err != nil {
		t.Fatalf("run: %v", err)
	}
	hist, _ := h.app.Store.History(0)
	if len(hist) != 1 {
		t.Fatalf("history %+v", hist)
	}
	cmd := hist[0].Cmd
	if !strings.Contains(cmd, "--password=***") {
		t.Fatalf("the tail of the command was cut off: %q", cmd)
	}
	if strings.Contains(cmd, "x") && strings.Contains(cmd, "password=x") {
		t.Fatalf("secret leaked: %q", cmd)
	}
	if len(cmd) > 2048 {
		t.Fatalf("stored cmd is %d bytes, over the 2 KiB cap", len(cmd))
	}
	if strings.HasSuffix(cmd, "…") {
		t.Fatalf("nothing was over the cap, so nothing may be marked as cut: %q", cmd)
	}

	// Over the cap the line is cut and says so.
	h2 := newHarness(t, "s1", "/wt/a")
	args := []string{"run", "--port", "3995", "--", "true"}
	for i := 0; i < 20; i++ {
		args = append(args, strings.Repeat("b", 200))
	}
	if err := h2.run(args...); err != nil {
		t.Fatalf("run: %v", err)
	}
	hist2, _ := h2.app.Store.History(0)
	if len(hist2) != 1 || !strings.HasSuffix(hist2[0].Cmd, "…") {
		t.Fatalf("an over-long command must be marked as cut: %+v", hist2)
	}
	if len(hist2[0].Cmd) > 2048 {
		t.Fatalf("stored cmd is %d bytes, over the 2 KiB cap", len(hist2[0].Cmd))
	}
}
