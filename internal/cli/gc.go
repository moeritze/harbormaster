package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newGc(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Prune dead entries now",
		RunE: func(_ *cobra.Command, _ []string) error {
			before, err := a.Store.History(0)
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			if _, err := a.Store.Load(); err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			after, _ := a.Store.History(0)
			_, _ = fmt.Fprintf(a.Stdout, "pruned %d\n", len(after)-len(before))
			return nil
		},
	}
}
