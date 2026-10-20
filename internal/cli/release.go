package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

func newRelease(a *app.App) *cobra.Command {
	var session string
	var allMine, force, kill, asJSON bool
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
			// Every form of release rests on ownership, so a caller with
			// no session id owns nothing here (see requireSession).
			// --session is no exception: naming an id is a claim about
			// whose entries these are, and the per-entry check below falls
			// back to worktree equality for a caller without a session,
			// which would hand anyone in the right directory a release of
			// somebody else's servers.
			if err := requireSession(a, force); err != nil {
				return err
			}
			var removed, released []registry.Entry
			var failed []string
			err := registryErr(a.Store.Update(func(f *registry.File) error {
				kept := f.Entries[:0]
				for _, e := range f.Entries {
					match := (port != 0 && e.Port == port) ||
						(session != "" && e.Session == session) ||
						(allMine && ident.Owns(a.Ident, e, a.Git.Worktree))
					if !match {
						kept = append(kept, e)
						continue
					}
					// Ownership is checked for every matched entry, whichever
					// selector matched it: naming a session id, even your own,
					// is no claim on another session's entry on that port.
					if !force && !ident.Owns(a.Ident, e, a.Git.Worktree) {
						return exitf(ExitDenied, "port %d owned by %s. Use --force to override ownership.", e.Port, ownerLine(e, a.Clock()))
					}
					// An entry we may not signal stays registered: removing it
					// would leave its server running unknown to the registry.
					if kill {
						if err := guardEntry(a, e); err != nil {
							failed = append(failed, fmt.Sprintf("port %d: %v", e.Port, err))
							kept = append(kept, e)
							continue
						}
					}
					removed = append(removed, e)
				}
				f.Entries = kept
				return nil
			}))
			if err != nil {
				return err
			}
			// Every matched entry is dealt with before the first history
			// failure is reported: the removals are already committed, so
			// stopping half way would leave processes running that the
			// registry no longer knows about. The failure still ends the
			// command with the registry exit code.
			var histErr error
			for _, e := range removed {
				if kill {
					if err := terminateFn(a)(e.PID, e.Spawned, 10*time.Second); err != nil {
						restoreAfterFailedKill(a, e, true)
						failed = append(failed, fmt.Sprintf("port %d: signal pid %d: %v", e.Port, e.PID, err))
						continue
					}
				}
				released = append(released, e)
				if err := a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "released", At: a.Clock()}); err != nil && histErr == nil {
					histErr = err
				}
			}
			if asJSON {
				if err := writeJSON(a.Stdout, released); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(a.Stdout, "released %d\n", len(released)); err != nil {
				return err
			}
			if len(failed) > 0 {
				return exitf(ExitDenied, "could not stop: %s", strings.Join(failed, "; "))
			}
			return registryErr(histErr)
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "release every entry of this session id")
	cmd.Flags().BoolVar(&allMine, "all-mine", false, "release every entry you own")
	cmd.Flags().BoolVar(&force, "force", false, "release entries owned by others")
	cmd.Flags().BoolVar(&kill, "kill", false, "also terminate the processes")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}
