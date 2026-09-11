// Package cli defines the harbormaster cobra commands.
package cli

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

// Version is set via -ldflags at build time.
var Version = "dev"

// version reports the build's version. Releases have it stamped in by
// -ldflags; a `go install module@version` build has no ldflags, so fall back
// to the module version the toolchain recorded in the binary.
func version() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return Version
}

// getenv is a package-level indirection over os.Getenv so tests can override it.
var getenv = os.Getenv

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
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(a.Stdout, "harbormaster %s\n", version())
			return err
		},
	})
	root.AddCommand(newLs(a), newPort(a), newCheck(a), newClaim(a), newRelease(a), newGc(a), newHistory(a), newRun(a), newKill(a), newHook(a), newInstall(a, false), newInstall(a, true))
	return root
}
