// Package runner starts a dev server under harbormaster's supervision.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

// Options controls one supervised run.
type Options struct {
	Port          int
	Label         string
	EnvNames      []string // extra env names to set to the port, PORT is always set
	Args          []string
	ListenTimeout time.Duration
	KillTimeout   time.Duration
}

// Register creates the registry entry for the spawned child.
type Register func(port, pid int, cmdLine, label string) (registry.Entry, error)

// Run spawns opts.Args in its own process group with the port injected,
// registers it, forwards termination signals, and removes the entry on exit.
func Run(ctx context.Context, a *app.App, opts Options, register Register) (int, error) {
	if len(opts.Args) == 0 {
		return 3, errors.New("no command given")
	}
	if opts.ListenTimeout == 0 {
		opts.ListenTimeout = registry.ListenGrace
	}
	if opts.KillTimeout == 0 {
		opts.KillTimeout = 10 * time.Second
	}

	cmd := exec.Command(opts.Args[0], opts.Args[1:]...) //nolint:gosec // running the user's command is the purpose
	cmd.Stdin = os.Stdin
	cmd.Stdout = a.Stdout
	cmd.Stderr = a.Stderr
	cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(opts.Port))
	for _, n := range opts.EnvNames {
		cmd.Env = append(cmd.Env, n+"="+strconv.Itoa(opts.Port))
	}
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return 3, fmt.Errorf("start %q: %w", opts.Args[0], err)
	}
	pid := cmd.Process.Pid

	// Start reaping immediately, before register: if register fails below,
	// Terminate must race a concurrent Wait rather than an unreaped zombie.
	// alive(pid) cannot tell a zombie from a live process, so without this
	// the register-failure path would burn the full KillTimeout every time.
	waitErr := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		waitErr <- cmd.Wait()
		close(done)
	}()

	entry, err := register(opts.Port, pid, ident.RedactCmd(opts.Args), opts.Label)
	if err != nil {
		_ = Terminate(pid, true, opts.KillTimeout)
		<-waitErr
		return 4, fmt.Errorf("register: %w", err)
	}
	defer unregister(a, entry)

	go warnIfNotListening(a, opts.Port, opts.ListenTimeout, done)

	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	select {
	case err := <-waitErr:
		// The child exited on its own; spec §6.1 step 5 ("on exit or on a
		// signal, terminate the process group") applies here too, so a dev
		// server's worker processes (e.g. Next.js) don't get orphaned when
		// only the leader exits. Best-effort and cheap in the common case:
		// the leader is already reaped, so kill(-pid, SIGTERM) either
		// reaches surviving group members or returns ESRCH, and alive(pid)
		// is false immediately -- no KillTimeout is burned. There is a
		// theoretical pid-reuse window between the leader exiting and this
		// call where -pid could in principle now name an unrelated process
		// group; the same risk is already accepted by the signal/ctx.Done
		// path below.
		_ = Terminate(pid, true, opts.KillTimeout)
		return exitCode(err), nil
	case <-sigs:
	case <-ctx.Done():
	}
	_ = Terminate(pid, true, opts.KillTimeout)
	err = <-waitErr
	return exitCode(err), nil
}

func unregister(a *app.App, e registry.Entry) {
	_ = a.Store.Update(func(f *registry.File) error {
		kept := f.Entries[:0]
		for _, x := range f.Entries {
			if x.ID != e.ID {
				kept = append(kept, x)
			}
		}
		f.Entries = kept
		return nil
	})
	_ = a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "exited", At: a.Clock()})
}

// warnIfNotListening watches the port until it starts listening, the child
// exits (signaled via done), or timeout elapses. done is a close-only signal
// (not the error channel) so it can have any number of receivers.
func warnIfNotListening(a *app.App, port int, timeout time.Duration, done <-chan struct{}) {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		case <-tick.C:
			if a.Prober.PortListening(port) {
				return
			}
		}
	}
	_, _ = fmt.Fprintf(a.Stderr, "harbormaster: warning: nothing listening on port %d after %s; the entry stays registered while the process lives\n", port, timeout)
}

// exitCode is platform-specific: see terminate_unix.go / terminate_windows.go.
// The unix build inspects syscall.WaitStatus to report 128+signal for a
// signaled child; the windows build has no such concept and just forwards
// ee.ExitCode().
