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
			if !force && !ident.Owns(a.Ident, e, a.Git.Worktree) {
				return exitf(ExitDenied, "port %d owned by %s. Use --force only if you are sure.", port, ownerLine(e, a.Clock()))
			}
			if err := terminateEntry(a, e); err != nil {
				return exitf(ExitDenied, "%v", err)
			}
			_, found, err := a.Store.Remove(e.ID)
			if err != nil {
				_, _ = fmt.Fprintf(a.Stderr, "warn: registry: %v\n", err)
			}
			// Only whoever actually removed the entry logs it. A
			// `run`-supervised child is unregistered by its own wrapper the
			// moment it dies, and one shutdown should not leave two records.
			if found {
				if err := a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "killed", At: a.Clock()}); err != nil {
					_, _ = fmt.Fprintf(a.Stderr, "warn: history: %v\n", err)
				}
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

// terminateEntry applies the safety guard, then terminates. Entries created by
// `run` own a process group and are signaled as one; every other entry (a
// claimed pid, or a row written by an older build) is signaled individually.
func terminateEntry(_ *app.App, e registry.Entry) error {
	if err := runner.Guard(e.PID, liveness.PidUID); err != nil {
		return err
	}
	return runner.Terminate(e.PID, e.Spawned, 10*time.Second)
}
