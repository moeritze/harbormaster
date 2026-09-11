package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
)

type checkResult struct {
	Port   int    `json:"port"`
	Status string `json:"status"` // free | own | foreign | unregistered
	Owner  string `json:"owner,omitempty"`
	PID    int    `json:"pid,omitempty"`
}

func newCheck(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check <port>",
		Short: "Report whether a port is free, yours, foreign, or held by an unregistered process",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			port, err := parsePort(args[0])
			if err != nil {
				return err
			}
			res, code := checkPort(a, port)
			// --json is the machine contract: the document is always on
			// stdout, and a non-zero result carries the exit code alone so
			// main has no message to echo to stderr.
			if asJSON {
				if err := writeJSON(a.Stdout, res); err != nil {
					return err
				}
				if code != ExitOK {
					return &ExitError{Code: code}
				}
				return nil
			}
			// Human output says it once: free and own print a line on
			// stdout, everything else is the single actionable stderr line
			// main prints for the returned ExitError.
			if code != ExitOK {
				return exitf(code, "port %d %s: %s", port, res.Status, res.Owner)
			}
			_, _ = fmt.Fprintf(a.Stdout, "port %d: %s\n", port, res.Status)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func checkPort(a *app.App, port int) (checkResult, int) {
	f, err := a.Store.Load()
	if err != nil {
		return checkResult{Port: port, Status: "error", Owner: err.Error()}, ExitRegistry
	}
	if e, ok := findByPort(f, port); ok {
		if ident.Owns(a.Ident, e, a.Git.Worktree) {
			return checkResult{Port: port, Status: "own", PID: e.PID}, ExitOK
		}
		return checkResult{Port: port, Status: "foreign", Owner: ownerLine(e, a.Clock()), PID: e.PID}, ExitDenied
	}
	if a.Prober.PortListening(port) {
		pid, cmdName, ok := a.PidOnPort(port)
		owner := "unknown process"
		if ok {
			owner = fmt.Sprintf("pid %d (%s), not registered", pid, render(cmdName))
		}
		return checkResult{Port: port, Status: "unregistered", Owner: owner, PID: pid}, ExitUnregistered
	}
	return checkResult{Port: port, Status: "free"}, ExitOK
}
