package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/install"
	"github.com/moeritze/harbormaster/skills"
)

func claudeConfigDir(project string) (string, error) {
	if project != "" {
		return filepath.Join(project, ".claude"), nil
	}
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

func defaultCommand() string {
	if exe, err := os.Executable(); err == nil {
		if abs, err := filepath.EvalSymlinks(exe); err == nil {
			return abs
		}
		return exe
	}
	return "harbormaster"
}

func newInstall(a *app.App, uninstall bool) *cobra.Command {
	var project, command string
	var dryRun bool
	use, short := "install <agent>", "Install hooks and the skill for an agent (claude)"
	if uninstall {
		use, short = "uninstall <agent>", "Remove the hooks and skill harbormaster installed"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if args[0] != "claude" {
				return exitf(ExitUsage, "unsupported agent %q (supported: claude)", args[0])
			}
			dir, err := claudeConfigDir(project)
			if err != nil {
				return exitf(ExitUsage, "%v", err)
			}
			o := install.Options{ConfigDir: dir, Command: command, Skill: skills.Harbormaster, DryRun: dryRun, Now: a.Clock}
			var r install.Report
			if uninstall {
				r, err = install.ClaudeUninstall(o)
			} else {
				r, err = install.Claude(o)
			}
			if err != nil {
				return exitf(ExitUsage, "%v", err)
			}
			verb := "would "
			if !dryRun {
				verb = ""
			}
			for _, act := range r.Actions {
				_, _ = fmt.Fprintf(a.Stdout, "%s%s\n", verb, act)
			}
			if r.Backup != "" {
				_, _ = fmt.Fprintf(a.Stdout, "backup: %s\n", r.Backup)
			}
			if !uninstall && !dryRun {
				_, _ = fmt.Fprintln(a.Stdout, "done. Restart Claude Code sessions to pick up the hooks.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "install into <DIR>/.claude instead of the user-level config")
	cmd.Flags().StringVar(&command, "command", defaultCommand(), "command hooks invoke (absolute path of this binary by default)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would change without writing")
	return cmd
}
