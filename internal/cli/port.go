package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newPort(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "port",
		Short: "Print this worktree's deterministic port",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := a.MyPort()
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			_, err = fmt.Fprintln(a.Stdout, p)
			return err
		},
	}
}
