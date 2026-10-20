package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ports"
)

func newPort(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "port",
		Short: "Print this worktree's deterministic port",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Always the deterministic port: users paste it into .env.local.
			det := ports.Deterministic(a.WorktreeKey(), a.Ports)
			p, err := a.MyPort()
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			if p != det {
				_, _ = fmt.Fprintf(a.Stderr, "harbormaster: port %d is taken; `run` would use %d\n", det, p)
			}
			_, err = fmt.Fprintln(a.Stdout, det)
			return err
		},
	}
}
