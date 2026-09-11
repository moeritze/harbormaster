// Package cli defines the harbormaster cobra commands.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

// Version is set via -ldflags at build time.
var Version = "dev"

// NewRoot builds the root command with all subcommands attached.
func NewRoot(a *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:           "harbormaster",
		Short:         "Registry of local dev servers: who owns which port, from which worktree",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(a.Stdout, "harbormaster %s\n", Version)
			return err
		},
	})
	return root
}
