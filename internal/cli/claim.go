package cli

import (
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

func newClaim(a *app.App) *cobra.Command {
	var pid int
	var label string
	var force, asJSON bool
	cmd := &cobra.Command{
		Use:   "claim <port>",
		Short: "Register a server you started without `run`",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			port, err := parsePort(args[0])
			if err != nil {
				return err
			}
			if pid == 0 {
				p, _, ok := a.PidOnPort(port)
				if !ok {
					return exitf(ExitUsage, "cannot find a process on port %d, pass --pid", port)
				}
				pid = p
			} else if !force {
				if err := verifyPidOnPort(a, port, pid); err != nil {
					return err
				}
			}
			if !a.Prober.PidAlive(pid) {
				return exitf(ExitUsage, "pid %d is not running", pid)
			}
			var created registry.Entry
			err = a.Store.Update(func(f *registry.File) error {
				if e, ok := findByPort(f, port); ok {
					if ident.Owns(a.Ident, e, a.Git.Worktree) {
						return exitf(ExitDenied, "port %d already registered by you (pid %d)", port, e.PID)
					}
					return exitf(ExitDenied, "port %d owned by %s", port, ownerLine(e, a.Clock()))
				}
				created = newEntry(a, port, pid, fmt.Sprintf("claimed pid %d", pid), label)
				f.Entries = append(f.Entries, created)
				return nil
			})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(a.Stdout, created)
			}
			_, err = fmt.Fprintf(a.Stdout, "claimed port %d (pid %d, id %s)\n", port, pid, created.ID)
			return err
		},
	}
	cmd.Flags().IntVar(&pid, "pid", 0, "pid listening on the port (default: detect)")
	cmd.Flags().StringVar(&label, "label", "", "what this server is for")
	cmd.Flags().BoolVar(&force, "force", false, "register --pid even if it is not the process listening on the port")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

// verifyPidOnPort refuses a --pid the OS does not show listening on the
// port. Registration is what licenses `kill` to signal a process, so
// claiming an arbitrary live pid against a port would aim harbormaster at a
// process that has nothing to do with it.
func verifyPidOnPort(a *app.App, port, pid int) error {
	actual, _, ok := a.PidOnPort(port)
	if !ok {
		return exitf(ExitUsage, "cannot confirm pid %d is listening on port %d; use --force to claim anyway", pid, port)
	}
	if actual != pid {
		return exitf(ExitUsage, "pid %d is not listening on port %d (pid %d is); use --force to claim anyway", pid, port, actual)
	}
	return nil
}

// newEntry builds a registry entry for the current identity and worktree.
func newEntry(a *app.App, port, pid int, cmdLine, label string) registry.Entry {
	return registry.Entry{
		ID:        ulid.Make().String(),
		Port:      port,
		PID:       pid,
		Cmd:       ident.Sanitize(cmdLine),
		Repo:      a.Git.Repo,
		Worktree:  a.Git.Worktree,
		Branch:    a.Git.Branch,
		Agent:     a.Ident.Agent,
		Session:   a.Ident.Session,
		Label:     ident.Sanitize(label),
		StartedAt: a.Clock(),
		HostUser:  a.Ident.HostUser,
	}
}
