package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/liveness"
	"github.com/moeritze/harbormaster/internal/registry"
	"github.com/moeritze/harbormaster/internal/runner"
)

func newKill(a *app.App) *cobra.Command {
	var force, asJSON bool
	cmd := &cobra.Command{
		Use:   "kill <port>",
		Short: "Stop the registered server on a port (yours only, unless --force)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			port, err := parsePort(args[0])
			if err != nil {
				return err
			}
			f, err := a.Store.Load()
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			e, ok := findByPort(f, port)
			if !ok {
				return exitf(ExitUnregistered, "port %d is not registered; harbormaster only kills servers it knows about", port)
			}
			if err := requireSession(a, force); err != nil {
				return err
			}
			if !force && !ident.Owns(a.Ident, e, a.Git.Worktree) {
				return exitf(ExitDenied, "port %d owned by %s. Use --force to override ownership.", port, ownerLine(e, a.Clock()))
			}
			if err := guardEntry(a, e); err != nil {
				return exitf(ExitDenied, "%v", err)
			}
			// Remove and record BEFORE signalling. A `run`-supervised child
			// is unregistered by its own wrapper the instant it dies, and
			// that wrapper would otherwise win the race and log "exited"
			// for a shutdown this command caused. The guard above already
			// decided the signal may be sent; if it then fails, the entry
			// is put back (see restoreAfterFailedKill) so the registry
			// keeps describing a live process.
			_, found, err := a.Store.Remove(e.ID)
			if err != nil {
				return exitf(ExitRegistry, "registry: %v", err)
			}
			if found {
				if err := a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "killed", At: a.Clock()}); err != nil {
					_, _ = fmt.Fprintf(a.Stderr, "warn: history: %v\n", err)
				}
			}
			if err := terminateFn(a)(e.PID, e.Spawned, 10*time.Second); err != nil {
				restoreAfterFailedKill(a, e, found)
				return exitf(ExitDenied, "signal pid %d: %v", e.PID, err)
			}
			if asJSON {
				return writeJSON(a.Stdout, e)
			}
			_, _ = fmt.Fprintf(a.Stdout, "killed pid %d on port %d\n", e.PID, port)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "kill even if owned by another session")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

// requireSession refuses write commands to a caller with no session id.
// ident.Owns falls back to worktree equality for such a caller, which is the
// right answer for the read-only conflict checks in check/ls/run, but as a
// kill permission it would mean anyone who unsets HARBORMASTER_SESSION and
// cd's into the right worktree can stop another session's server. Signalling
// a process needs a positive claim of ownership, so the fallback does not
// apply here: say who you are, or pass --force.
func requireSession(a *app.App, force bool) error {
	if force || a.Ident.Session != "" {
		return nil
	}
	return exitf(ExitDenied, "no session id in the environment; set HARBORMASTER_SESSION or use --force")
}

// guardEntry applies the safety guard (spec §10) to an entry about to be
// signalled: never pid 1, never our own pid, never another user's process,
// never a pid that has been reused since the entry was written, and never a
// process GROUP whose entry has no start time to check against.
func guardEntry(a *app.App, e registry.Entry) error {
	if err := runner.Guard(e.PID, liveness.PidUID); err != nil {
		return err
	}
	return runner.CheckStartTime(e.PID, e.StartTime, e.Spawned, a.PidStartTime)
}

// restoreAfterFailedKill puts an entry back after the signal it was removed
// for failed to land, so the registry keeps describing a live process.
//
// Two cases skip the restore. If the entry was already gone when this
// command removed it (found == false), something else -- the `run` wrapper,
// a session-end hook, a concurrent release -- has already finished with it,
// and re-adding would resurrect a row nobody owns. And if another entry
// holds the port by now, that entry is the truth about the port; a second
// row for it would leave two entries claiming one port, which every lookup
// here resolves by taking whichever comes first. Either way the user is
// told, because a registry that no longer lists a process they started is
// exactly the surprise harbormaster exists to prevent.
func restoreAfterFailedKill(a *app.App, e registry.Entry, found bool) {
	if !found {
		_, _ = fmt.Fprintf(a.Stderr, "warning: the entry for port %d was already gone; not restoring it after the failed signal\n", e.Port)
		return
	}
	restored := false
	if err := a.Store.Update(func(f *registry.File) error {
		if _, taken := findByPort(f, e.Port); taken {
			return nil
		}
		f.Entries = append(f.Entries, e)
		restored = true
		return nil
	}); err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "warning: could not restore the entry for port %d: %v\n", e.Port, err)
		return
	}
	if !restored {
		_, _ = fmt.Fprintf(a.Stderr, "warning: port %d is registered to another entry now; not restoring this one after the failed signal\n", e.Port)
	}
}

// startTimeOf records the process start time for a new entry; empty when
// the platform cannot tell, which disables the reuse check for that entry.
// Losing that check deserves a word: it is what stands between a later
// `kill` and a process that merely inherited the pid. The warning prints
// once because newEntry runs exactly once per registration.
func startTimeOf(a *app.App, pid int) string {
	if a.PidStartTime == nil {
		return ""
	}
	st, err := a.PidStartTime(pid)
	if err != nil {
		_, _ = fmt.Fprintln(a.Stderr, "warning: could not record the process start time; pid-reuse protection is off for this entry")
		return ""
	}
	return st
}

// terminateFn is the signal sender, injectable so tests can exercise what
// happens after a signal that fails without having to find a live process
// that refuses to die.
func terminateFn(a *app.App) func(pid int, group bool, timeout time.Duration) error {
	if a.Terminate != nil {
		return a.Terminate
	}
	return runner.Terminate
}

// terminateEntry applies the safety guard, then terminates. Entries created by
// `run` own a process group and are signaled as one; every other entry (a
// claimed pid, or a row written by an older build) is signaled individually.
func terminateEntry(a *app.App, e registry.Entry) error {
	if err := guardEntry(a, e); err != nil {
		return err
	}
	return terminateFn(a)(e.PID, e.Spawned, 10*time.Second)
}
