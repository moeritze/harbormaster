package cli

import (
	"errors"
	"fmt"
	"regexp"
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
			if err := validateEnvNames(envNames); err != nil {
				return err
			}
			resolved, err := resolveRunPort(a, port)
			if err != nil {
				return err
			}
			reg := func(p, pid int, cmdLine, lbl string) (registry.Entry, error) {
				e := newEntry(a, p, pid, cmdLine, lbl)
				// Only `run` puts the child in its own process group, so only
				// entries it creates may later be signaled as a group.
				e.Spawned = true
				err := a.Store.Update(func(f *registry.File) error {
					if err := denyIfRegistered(a, f, p); err != nil {
						return err
					}
					f.Entries = append(f.Entries, e)
					return nil
				})
				return e, err
			}
			// The banner goes through the same redaction as the stored cmd
			// field: a secret passed on the command line must not be echoed
			// into a terminal or an agent's captured output.
			_, _ = fmt.Fprintf(a.Stderr, "harbormaster: port %d, %s\n", resolved, ident.RedactCmd(args))
			code, err := runner.Run(cmd.Context(), a, runner.Options{
				Port: resolved, Label: label, EnvNames: envNames, Args: args,
				ListenTimeout: registry.ListenGrace, KillTimeout: 10 * time.Second,
			}, reg)
			return runResultToErr(code, err, render(args[0]))
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port to use (default: $PORT, then this worktree's deterministic port)")
	cmd.Flags().StringVar(&label, "label", "", "what this server is for")
	cmd.Flags().StringArrayVar(&envNames, "env", nil, "additional env var name to set to the port (PORT is always set)")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// dangerousEnvNames are variables that change how the child resolves
// programs and libraries. Setting one of them to a port number would at best
// break the command and at worst redirect what it loads, so --env refuses
// them outright even though they are valid names.
var dangerousEnvNames = map[string]bool{
	"PATH":                  true,
	"HOME":                  true,
	"LD_PRELOAD":            true,
	"DYLD_INSERT_LIBRARIES": true,
	"DYLD_LIBRARY_PATH":     true,
	"LD_LIBRARY_PATH":       true,
}

// validateEnvNames rejects anything that is not a plain environment variable
// name. The value harbormaster assigns is always the port number, but the
// name lands in the child's environment verbatim.
func validateEnvNames(names []string) error {
	for _, n := range names {
		if !envNamePattern.MatchString(n) || dangerousEnvNames[n] {
			return exitf(ExitUsage, "invalid --env name %q", n)
		}
	}
	return nil
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
