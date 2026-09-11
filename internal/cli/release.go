package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

func newRelease(a *app.App) *cobra.Command {
	var session string
	var allMine, force, kill bool
	cmd := &cobra.Command{
		Use:   "release [port]",
		Short: "Remove registry entries (yours by default)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var port int
			if len(args) == 1 {
				p, err := parsePort(args[0])
				if err != nil {
					return err
				}
				port = p
			}
			if port == 0 && session == "" && !allMine {
				return exitf(ExitUsage, "specify a port, --session ID, or --all-mine")
			}
			var removed []registry.Entry
			err := a.Store.Update(func(f *registry.File) error {
				kept := f.Entries[:0]
				for _, e := range f.Entries {
					match := (port != 0 && e.Port == port) ||
						(session != "" && e.Session == session) ||
						(allMine && ident.Owns(a.Ident, e, a.Git.Worktree))
					if !match {
						kept = append(kept, e)
						continue
					}
					if port != 0 && !force && !ident.Owns(a.Ident, e, a.Git.Worktree) {
						return exitf(ExitDenied, "port %d owned by %s. Use --force to release anyway.", port, ownerLine(e, a.Clock()))
					}
					removed = append(removed, e)
				}
				f.Entries = kept
				return nil
			})
			if err != nil {
				return err
			}
			for _, e := range removed {
				if kill {
					if err := terminateEntry(a, e); err != nil {
						_, _ = fmt.Fprintf(a.Stderr, "warn: kill pid %d: %v\n", e.PID, err)
					}
				}
				_ = a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "released", At: a.Clock()})
			}
			_, _ = fmt.Fprintf(a.Stdout, "released %d\n", len(removed))
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "release every entry of this session id")
	cmd.Flags().BoolVar(&allMine, "all-mine", false, "release every entry you own")
	cmd.Flags().BoolVar(&force, "force", false, "release entries owned by others")
	cmd.Flags().BoolVar(&kill, "kill", false, "also terminate the processes")
	return cmd
}

// terminateEntry is implemented in Task 11 (runner.Terminate + Guard).
//
//nolint:unparam // stub until Task 11
func terminateEntry(_ *app.App, _ registry.Entry) error { return nil }
