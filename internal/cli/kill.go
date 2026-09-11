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
				return exitf(ExitDenied, "port %d owned by %s. Use --force only if you are sure.", port, ownerLine(e, a.Clock()))
			}
			if err := guardEntry(e); err != nil {
				return exitf(ExitDenied, "%v", err)
			}
			// Remove and record BEFORE signalling. A `run`-supervised child
			// is unregistered by its own wrapper the instant it dies, and
			// that wrapper would otherwise win the race and log "exited"
			// for a shutdown this command caused. The guard above already
			// decided the signal may be sent; if it then fails, the entry
			// is put back so the registry keeps describing a live process.
			_, found, err := a.Store.Remove(e.ID)
			if err != nil {
				return exitf(ExitRegistry, "registry: %v", err)
			}
			if found {
				if err := a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "killed", At: a.Clock()}); err != nil {
					_, _ = fmt.Fprintf(a.Stderr, "warn: history: %v\n", err)
				}
			}
			if err := runner.Terminate(e.PID, e.Spawned, 10*time.Second); err != nil {
				_ = a.Store.Update(func(f *registry.File) error { f.Entries = append(f.Entries, e); return nil })
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
// signalled: never pid 1, never our own pid, never another user's process.
func guardEntry(e registry.Entry) error {
	return runner.Guard(e.PID, liveness.PidUID)
}

// terminateEntry applies the safety guard, then terminates. Entries created by
// `run` own a process group and are signaled as one; every other entry (a
// claimed pid, or a row written by an older build) is signaled individually.
func terminateEntry(_ *app.App, e registry.Entry) error {
	if err := guardEntry(e); err != nil {
		return err
	}
	return runner.Terminate(e.PID, e.Spawned, 10*time.Second)
}
