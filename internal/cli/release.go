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
			// The port form and --all-mine both rest on ownership, so a
			// caller with no session id owns nothing here (see
			// requireSession). --session names an id explicitly and is
			// checked per entry below.
			if port != 0 || allMine {
				if err := requireSession(a, force); err != nil {
					return err
				}
			}
			var removed []registry.Entry
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
					// Ownership is checked for every matched entry, not just
					// port matches: `release --session X` from another session
					// would otherwise kill X's servers with no --force. A
					// session id the caller actually owns still passes.
					mine := ident.Owns(a.Ident, e, a.Git.Worktree) ||
						(session != "" && session == a.Ident.Session)
					if !force && !mine {
						return exitf(ExitDenied, "port %d owned by %s. Use --force to release anyway.", e.Port, ownerLine(e, a.Clock()))
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
					if err := terminateEntry(a, e); err != nil {
						_, _ = fmt.Fprintf(a.Stderr, "warn: kill pid %d: %v\n", e.PID, err)
					}
				}
				if err := a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "released", At: a.Clock()}); err != nil && histErr == nil {
					histErr = err
				}
			}
			if asJSON {
				if err := writeJSON(a.Stdout, removed); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(a.Stdout, "released %d\n", len(removed)); err != nil {
				return err
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
