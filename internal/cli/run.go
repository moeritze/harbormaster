package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
	"github.com/moeritze/harbormaster/internal/runner"
)

func newRun(a *app.App) *cobra.Command {
	var port int
	var label string
	var envNames []string
	cmd := &cobra.Command{
		Use:   "run [--port N] [--label TEXT] [--env NAME] -- <command...>",
		Short: "Start a dev server on this worktree's port and register it",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return exitf(ExitUsage, "usage: harbormaster run [flags] -- <command...>")
			}
			resolved, err := resolveRunPort(a, port)
			if err != nil {
				return err
			}
			reg := func(p, pid int, cmdLine, lbl string) (registry.Entry, error) {
				e := newEntry(a, p, pid, cmdLine, lbl)
				err := a.Store.Update(func(f *registry.File) error {
					if x, ok := findByPort(f, p); ok && !ident.Owns(a.Ident, x, a.Git.Worktree) {
						return exitf(ExitDenied, "port %d owned by %s", p, ownerLine(x, a.Clock()))
					}
					f.Entries = append(f.Entries, e)
					return nil
				})
				return e, err
			}
			_, _ = fmt.Fprintf(a.Stderr, "harbormaster: port %d, %s\n", resolved, strings.Join(args, " "))
			code, err := runner.Run(cmd.Context(), a, runner.Options{
				Port: resolved, Label: label, EnvNames: envNames, Args: args,
				ListenTimeout: registry.ListenGrace, KillTimeout: 10 * time.Second,
			}, reg)
			if err != nil {
				return exitf(code, "%v", err)
			}
			if code != 0 {
				return &ExitError{Code: code, Msg: fmt.Sprintf("%s exited with %d", args[0], code)}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port to use (default: $PORT, then this worktree's deterministic port)")
	cmd.Flags().StringVar(&label, "label", "", "what this server is for")
	cmd.Flags().StringArrayVar(&envNames, "env", nil, "additional env var name to set to the port (PORT is always set)")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// resolveRunPort applies spec §6.1 steps 1-2.
func resolveRunPort(a *app.App, flag int) (int, error) {
	if flag == 0 {
		if v := getenv("PORT"); v != "" {
			p, err := parsePort(v)
			if err != nil {
				return 0, err
			}
			flag = p
		}
	}
	if flag == 0 {
		p, err := a.MyPort()
		if err != nil {
			return 0, exitf(ExitRegistry, "%v", err)
		}
		return p, nil
	}
	res, code := checkPort(a, flag)
	switch code {
	case ExitOK:
		return flag, nil
	case ExitDenied:
		return 0, exitf(ExitDenied, "port %d owned by %s. Run `harbormaster port` for this worktree's port.", flag, res.Owner)
	default:
		return 0, exitf(code, "port %d is held by %s", flag, res.Owner)
	}
}
