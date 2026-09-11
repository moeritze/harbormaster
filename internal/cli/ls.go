package cli

import (
	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newLs(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List registered servers",
		RunE: func(_ *cobra.Command, _ []string) error {
			f, err := a.Store.Load()
			if err != nil {
				return exitf(ExitRegistry, "registry: %v", err)
			}
			if asJSON {
				return writeJSON(a.Stdout, f.Entries)
			}
			return writeTable(a.Stdout, f.Entries, a.Clock())
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}
