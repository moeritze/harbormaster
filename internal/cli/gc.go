package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newGc(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Prune dead entries now",
		RunE: func(_ *cobra.Command, _ []string) error {
			pruned, err := a.Store.Prune()
			if err != nil {
				return registryErr(err)
			}
			if asJSON {
				return writeJSON(a.Stdout, pruned)
			}
			_, _ = fmt.Fprintf(a.Stdout, "pruned %d\n", len(pruned))
			for _, r := range pruned {
				_, _ = fmt.Fprintf(a.Stdout, "%d\t%s\t%s\n", r.Port, r.Reason, orDash(render(r.Worktree)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}
