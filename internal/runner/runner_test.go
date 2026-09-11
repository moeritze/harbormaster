package runner_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/liveness"
	"github.com/moeritze/harbormaster/internal/registry"
	"github.com/moeritze/harbormaster/internal/runner"
)

// TestMain turns the test binary into a tiny TCP listener when HM_TEST_LISTENER=1.
func TestMain(m *testing.M) {
	if os.Getenv("HM_TEST_LISTENER") == "1" {
		port := os.Getenv("PORT")
		if alt := os.Getenv("HM_TEST_ENV_NAME"); alt != "" {
			port = os.Getenv(alt)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			fmt.Fprintln(os.Stderr, "listen:", err)
			os.Exit(7)
		}
		defer func() { _ = ln.Close() }()
		fmt.Println("listening on", port)
		for {
			// A bare `select {}` here would deadlock: with no Accept loop
			// running, the runtime sees nothing that could ever wake the
			// process and panics with "all goroutines are asleep". Accepting
			// (and dropping) connections keeps this goroutine legitimately
			// blocked in a network syscall until the process is killed.
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}
	os.Exit(m.Run())
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func newApp(t *testing.T) (*app.App, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	st, err := registry.Open(filepath.Join(t.TempDir(), "hm"), liveness.OS{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &app.App{Stdout: out, Stderr: out, Store: st, Prober: liveness.OS{}}, out
}

// listenerArgs ignores its would-be parameter by design (the re-exec'd test
// binary always runs zero tests and falls into TestMain's listener branch);
// it takes none so unparam has nothing to flag.
func listenerArgs() []string {
	return []string{os.Args[0], "-test.run=^$"}
}

func register(a *app.App) func(port, pid int, cmdLine, label string) (registry.Entry, error) {
	return func(port, pid int, cmdLine, label string) (registry.Entry, error) {
		e := registry.Entry{ID: strconv.Itoa(pid), Port: port, PID: pid, Cmd: cmdLine, Label: label, Agent: "test", StartedAt: time.Now().UTC()}
		err := a.Store.Update(func(f *registry.File) error { f.Entries = append(f.Entries, e); return nil })
		return e, err
	}
}

func TestRunRegistersInjectsPortAndUnregistersOnSignal(t *testing.T) {
	a, out := newApp(t)
	port := freePort(t)
	t.Setenv("HM_TEST_LISTENER", "1")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // don't leak the re-exec'd listener child if an assertion below fails first
	done := make(chan struct{})
	var code int
	var runErr error
	go func() {
		code, runErr = runner.Run(ctx, a, runner.Options{Port: port, Label: "t", Args: listenerArgs(), ListenTimeout: 5 * time.Second, KillTimeout: 2 * time.Second}, register(a))
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !(liveness.OS{}).PortListening(port) {
		time.Sleep(50 * time.Millisecond)
	}
	if !(liveness.OS{}).PortListening(port) {
		t.Fatalf("child never listened; output:\n%s", out.String())
	}
	f, _ := a.Store.Load()
	if len(f.Entries) != 1 || f.Entries[0].Port != port {
		t.Fatalf("entry missing: %+v", f.Entries)
	}
	childPid := f.Entries[0].PID

	cancel() // simulates SIGTERM to the wrapper
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancel")
	}
	if runErr != nil {
		t.Fatalf("run err: %v", runErr)
	}
	if code == 0 {
		t.Log("exit code 0 after signal is acceptable only if the child exited cleanly")
	}
	f, _ = a.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("entry not removed: %+v", f.Entries)
	}
	if (liveness.OS{}).PidAlive(childPid) {
		t.Fatalf("child %d still alive", childPid)
	}
}

func TestRunCustomEnvName(t *testing.T) {
	a, _ := newApp(t)
	port := freePort(t)
	t.Setenv("HM_TEST_LISTENER", "1")
	t.Setenv("HM_TEST_ENV_NAME", "VITE_PORT")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // don't leak the re-exec'd listener child if an assertion below fails first
	done := make(chan struct{})
	go func() {
		_, _ = runner.Run(ctx, a, runner.Options{Port: port, EnvNames: []string{"VITE_PORT"}, Args: listenerArgs(), ListenTimeout: 5 * time.Second, KillTimeout: 2 * time.Second}, register(a))
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !(liveness.OS{}).PortListening(port) {
		time.Sleep(50 * time.Millisecond)
	}
	listening := (liveness.OS{}).PortListening(port)
	cancel()
	<-done
	if !listening {
		t.Fatal("VITE_PORT was not injected")
	}
}

func TestRunPropagatesChildExitCode(t *testing.T) {
	a, _ := newApp(t)
	code, err := runner.Run(context.Background(), a, runner.Options{Port: freePort(t), Args: []string{"sh", "-c", "exit 3"}, ListenTimeout: time.Second, KillTimeout: time.Second}, register(a))
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Fatalf("code %d", code)
	}
	f, _ := a.Store.Load()
	if len(f.Entries) != 0 {
		t.Fatalf("entry should be removed after exit: %+v", f.Entries)
	}
}

// TestRunTerminatesProcessGroupOnNaturalExit covers spec §6.1 step 5 for the
// natural-exit path: when the group leader exits on its own (not via signal
// or ctx cancellation), harbormaster must still terminate the process group
// so worker processes a dev server spawned (e.g. Next.js) aren't orphaned.
func TestRunTerminatesProcessGroupOnNaturalExit(t *testing.T) {
	a, _ := newApp(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// The backgrounded sleep redirects its own stdio to /dev/null rather than
	// inheriting the test's pipe: exec.Cmd.Wait joins the goroutine copying a
	// non-*os.File Stdout/Stderr, so if the worker kept that pipe's write end
	// open past sh's exit, Wait would block for the worker's full lifetime
	// (a well-known os/exec gotcha, golang.org/issue/23019) -- the opposite
	// of what this test needs to observe promptly.
	args := []string{"sh", "-c", fmt.Sprintf("sleep 30 </dev/null >/dev/null 2>&1 & echo $! > '%s'; exit 0", pidFile)}
	code, err := runner.Run(context.Background(), a, runner.Options{Port: freePort(t), Args: args, ListenTimeout: time.Second, KillTimeout: 2 * time.Second}, register(a))
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("code %d, want 0", code)
	}

	b, err := os.ReadFile(pidFile) //nolint:gosec // test-owned tmp path, not user input
	if err != nil {
		t.Fatal(err)
	}
	workerPid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("parse worker pid from %q: %v", b, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (liveness.OS{}).PidAlive(workerPid) {
		time.Sleep(50 * time.Millisecond)
	}
	if (liveness.OS{}).PidAlive(workerPid) {
		t.Fatalf("worker pid %d still alive after group leader exited on its own", workerPid)
	}
}

// TestRunRegisterFailureReapsPromptly covers Finding 2 from review round 1:
// the reaper goroutine must start before register is called, so a failed
// register terminates and drains a live child instead of burning the full
// KillTimeout polling an unreaped zombie that alive() can't tell from live.
func TestRunRegisterFailureReapsPromptly(t *testing.T) {
	a, _ := newApp(t)
	failRegister := func(int, int, string, string) (registry.Entry, error) {
		return registry.Entry{}, errors.New("boom")
	}
	opts := runner.Options{Port: freePort(t), Args: []string{"sleep", "30"}, ListenTimeout: time.Second, KillTimeout: 5 * time.Second}

	done := make(chan struct{})
	var code int
	var runErr error
	start := time.Now()
	go func() {
		code, runErr = runner.Run(context.Background(), a, opts, failRegister)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not return within 1s on register failure")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("run took %s after register failure, want < 1s", elapsed)
	}
	if runErr == nil {
		t.Fatal("expected a non-nil error on register failure")
	}
	if code != 4 {
		t.Fatalf("code %d, want 4", code)
	}
}

func TestGuard(t *testing.T) {
	sameUID := func(int) (int, error) { return os.Getuid(), nil }
	if err := runner.Guard(1, sameUID); err == nil {
		t.Fatal("pid 1 must be refused")
	}
	if err := runner.Guard(os.Getpid(), sameUID); err == nil {
		t.Fatal("own pid must be refused")
	}
	if err := runner.Guard(99999, func(int) (int, error) { return os.Getuid() + 1, nil }); err == nil {
		t.Fatal("other uid must be refused")
	}
	if err := runner.Guard(99999, sameUID); err != nil {
		t.Fatalf("same uid should pass: %v", err)
	}
}

func TestTerminateKillsProcessGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := runner.Terminate(cmd.Process.Pid, true, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if (liveness.OS{}).PidAlive(cmd.Process.Pid) {
		t.Fatal("still alive")
	}
}

func TestCheckStartTime(t *testing.T) {
	same := func(int) (string, error) { return "t1", nil }
	if err := runner.CheckStartTime(5, "t1", same); err != nil {
		t.Fatalf("matching start time must pass: %v", err)
	}
	if err := runner.CheckStartTime(5, "t0", same); err == nil {
		t.Fatal("different start time must be refused")
	}
	if err := runner.CheckStartTime(5, "", same); err != nil {
		t.Fatal("empty stored value disables the check")
	}
	if err := runner.CheckStartTime(5, "t1", nil); err != nil {
		t.Fatal("nil lookup disables the check")
	}
	bad := func(int) (string, error) { return "", errors.New("no ps") }
	if err := runner.CheckStartTime(5, "t1", bad); err == nil {
		t.Fatal("lookup failure must refuse (fail closed)")
	}
}

// TestTerminateGroupFallsBackForNonLeader: a child that is not a process
// group leader (no Setpgid) is still terminated when asked as a group.
func TestTerminateGroupFallsBackForNonLeader(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := runner.Terminate(cmd.Process.Pid, true, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if (liveness.OS{}).PidAlive(cmd.Process.Pid) {
		t.Fatal("still alive")
	}
}
