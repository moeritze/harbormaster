package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/hooks"
	"github.com/moeritze/harbormaster/internal/hooks/adapters/claude"
)

// newHook is the hidden agent-hook entrypoint: `harbormaster hook <agent>
// <event>` reads the agent's JSON payload from stdin (capped at
// claude.MaxStdin) and always exits 0. An unreadable or oversize payload, or
// an unknown agent, logs one line to stderr and prints nothing; a malformed
// (but readable) payload for a known agent fails open silently. Either way
// nothing on stdout ever blocks the agent it is wired into.
func newHook(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook <agent> <event>",
		Short:  "Agent hook entrypoint (reads the agent's JSON on stdin, always exits 0)",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			agent, event := args[0], args[1]
			core := hooks.New(a)
			in := a.Stdin
			if in == nil {
				in = os.Stdin
			}
			payload, err := io.ReadAll(io.LimitReader(in, claude.MaxStdin+1))
			if err != nil || len(payload) > claude.MaxStdin {
				_, _ = fmt.Fprintln(a.Stderr, "harbormaster hook: payload unreadable or over 1 MiB; allowing")
				return nil
			}
			switch agent {
			case "claude":
				ev, ok, err := claude.Parse(event, payload)
				if err != nil {
					// A malformed payload from the agent is routine noise (a
					// version skew, an unexpected tool shape), not a
					// configuration problem worth surfacing on every call:
					// fail open quietly, same as the !ok "not for us" case.
					return nil
				}
				if !ok {
					return nil
				}
				if out := claude.Format(event, core.Handle(ev)); out != nil {
					_, _ = a.Stdout.Write(append(out, '\n'))
				}
				return nil
			default:
				_, _ = fmt.Fprintf(a.Stderr, "harbormaster hook: unknown agent %q; allowing\n", agent)
				return nil
			}
		},
	}
	return cmd
}
