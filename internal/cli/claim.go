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
			}
			if !a.Prober.PidAlive(pid) {
				return exitf(ExitUsage, "pid %d is not running", pid)
			}
			return a.Store.Update(func(f *registry.File) error {
				if e, ok := findByPort(f, port); ok {
					if ident.Owns(a.Ident, e, a.Git.Worktree) {
						return exitf(ExitDenied, "port %d already registered by you (pid %d)", port, e.PID)
					}
					return exitf(ExitDenied, "port %d owned by %s", port, ownerLine(e, a.Clock()))
				}
				f.Entries = append(f.Entries, newEntry(a, port, pid, fmt.Sprintf("claimed pid %d", pid), label))
				return nil
			})
		},
	}
	cmd.Flags().IntVar(&pid, "pid", 0, "pid listening on the port (default: detect)")
	cmd.Flags().StringVar(&label, "label", "", "what this server is for")
	return cmd
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
