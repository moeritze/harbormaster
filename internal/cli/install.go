package cli

import (
	"fmt"
	"os"
	"os/exec"
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

func cursorConfigDir(project string) (string, error) {
	if project != "" {
		return filepath.Join(project, ".cursor"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor"), nil
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

const supportedAgents = "claude, cursor, agents-md"

// guiPathDirs is the PATH a desktop-launched app inherits when it is started
// from the Dock, Spotlight or a .desktop entry: no login shell has run, so
// none of ~/.local/bin, ~/go/bin or a version manager's shims are on it.
// Claude Code and Cursor both run hooks with that PATH, which is why a
// --project install writing the bare name can leave hooks that never fire.
var guiPathDirs = []string{"/usr/bin", "/bin", "/usr/local/bin", "/opt/homebrew/bin"}

// lookPath is exec.LookPath, indirected so a test can decide what a GUI
// app's PATH holds.
var lookPath = exec.LookPath

// onGUIPath reports whether a bare command name resolves in guiPathDirs.
// LookPath on a path with a separator checks exactly that file, which is the
// per-directory probe a minimal PATH would do.
func onGUIPath(name string) bool {
	for _, dir := range guiPathDirs {
		if _, err := lookPath(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// commandPathWarning is printed after a --project install that wired up the
// bare name and could not find it where the agent will look.
const commandPathWarning = `warning: "harbormaster" is not on a GUI app's default PATH; use --command <absolute path> if hooks do not fire`

func newInstall(a *app.App, uninstall bool) *cobra.Command {
	var project, command string
	var dryRun bool
	use, short := "install <agent>", "Install hooks and the skill for an agent ("+supportedAgents+")"
	if uninstall {
		use, short = "uninstall <agent>", "Remove the hooks and skill harbormaster installed ("+supportedAgents+")"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			// A --project install lands in a file that gets committed and
			// shared, where this machine's absolute path is worse than
			// useless. Bare "harbormaster" is what every checkout can run.
			if project != "" && !c.Flags().Changed("command") {
				command = "harbormaster"
			}
			var r install.Report
			var err error
			restart, checkPath := "", false
			switch args[0] {
			case "claude":
				dir, derr := claudeConfigDir(project)
				if derr != nil {
					return exitf(ExitUsage, "%v", derr)
				}
				o := install.Options{ConfigDir: dir, Command: command, Skill: skills.Harbormaster, DryRun: dryRun, Now: a.Clock}
				if uninstall {
					r, err = install.ClaudeUninstall(o)
				} else {
					r, err = install.Claude(o)
				}
				restart, checkPath = "Restart Claude Code sessions to pick up the hooks.", true
			case "cursor":
				dir, derr := cursorConfigDir(project)
				if derr != nil {
					return exitf(ExitUsage, "%v", derr)
				}
				o := install.CursorOptions{ConfigDir: dir, Command: command, DryRun: dryRun, Now: a.Clock}
				if project != "" {
					o.Rule = skills.CursorRule()
				}
				if uninstall {
					r, err = install.CursorUninstall(o)
				} else {
					r, err = install.Cursor(o)
				}
				restart, checkPath = "Restart Cursor to pick up the hooks.", true
			case "agents-md":
				dir := project
				if dir == "" {
					dir = a.Cwd
				}
				path := filepath.Join(dir, "AGENTS.md")
				if uninstall {
					r, err = install.AgentsMDUninstall(path, dryRun)
				} else {
					r, err = install.AgentsMD(path, dryRun)
				}
			default:
				return exitf(ExitUsage, "unsupported agent %q (supported: %s)", args[0], supportedAgents)
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
			if r.Backup != "" && !dryRun {
				_, _ = fmt.Fprintf(a.Stdout, "%s reformatted; backup at %s\n", filepath.Base(r.Settings), r.Backup)
			}
			if !uninstall && !dryRun && checkPath && command == "harbormaster" && !onGUIPath(command) {
				_, _ = fmt.Fprintln(a.Stderr, commandPathWarning)
			}
			if !uninstall && !dryRun && restart != "" {
				_, _ = fmt.Fprintln(a.Stdout, "done. "+restart)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "install into <DIR>/.claude, <DIR>/.cursor or <DIR>/AGENTS.md instead of the user-level location")
	cmd.Flags().StringVar(&command, "command", defaultCommand(), "command hooks invoke (absolute path of this binary by default)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would change without writing")
	return cmd
}
