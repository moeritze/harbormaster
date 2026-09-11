package cli

import (
	"errors"
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
					if err := denyIfRegistered(a, f, p); err != nil {
						return err
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
			return runResultToErr(code, err, args[0])
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port to use (default: $PORT, then this worktree's deterministic port)")
	cmd.Flags().StringVar(&label, "label", "", "what this server is for")
	cmd.Flags().StringArrayVar(&envNames, "env", nil, "additional env var name to set to the port (PORT is always set)")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// resolveRunPort applies spec §6.1 steps 1-2. Whether the port came from
// --port, $PORT, or this worktree's deterministic default, it is always run
// through checkPort so an already-running own entry on that port is refused
// rather than silently double-registered (Finding 1).
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
		flag = p
	}
	res, code := checkPort(a, flag)
	switch {
	case code == ExitOK && res.Status == "own":
		return 0, alreadyRunningErr(flag, res.PID)
	case code == ExitOK:
		return flag, nil
	case code == ExitDenied:
		return 0, exitf(ExitDenied, "port %d owned by %s. Run `harbormaster port` for this worktree's port.", flag, res.Owner)
	default:
		return 0, exitf(code, "port %d is held by %s", flag, res.Owner)
	}
}

// alreadyRunningErr reports that port is already registered to this
// session. Finding 1 (ruling): `run` must refuse ANY existing entry on the
// target port, own included, rather than appending a second row for the
// same server.
func alreadyRunningErr(port, pid int) error {
	return exitf(ExitDenied, "port %d already registered by this session (pid %d); run `harbormaster kill %d` first", port, pid, port)
}

// denyIfRegistered is the authoritative check applied under the registry
// lock at register time: even if resolveRunPort saw the port as free, a
// concurrent `run` may have registered it in the meantime. Own entries are
// refused just like foreign ones (Finding 1).
func denyIfRegistered(a *app.App, f *registry.File, port int) error {
	x, ok := findByPort(f, port)
	if !ok {
		return nil
	}
	if ident.Owns(a.Ident, x, a.Git.Worktree) {
		return alreadyRunningErr(port, x.PID)
	}
	return exitf(ExitDenied, "port %d owned by %s", port, ownerLine(x, a.Clock()))
}

// runResultToErr converts runner.Run's (code, err) into the command's
// returned error. An error from register (e.g. denyIfRegistered) arrives
// wrapped by runner.Run as a generic exit-4 registry failure; unwrapping to
// the embedded *ExitError preserves its real exit code and message
// (Finding 2) instead of masking it as ExitRegistry.
func runResultToErr(code int, err error, cmdName string) error {
	if err != nil {
		var ee *ExitError
		if errors.As(err, &ee) {
			return ee
		}
		return exitf(code, "%v", err)
	}
	if code != 0 {
		return &ExitError{Code: code, Msg: fmt.Sprintf("%s exited with %d", cmdName, code)}
	}
	return nil
}
