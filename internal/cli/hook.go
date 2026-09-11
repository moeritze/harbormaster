package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/hooks"
	"github.com/moeritze/harbormaster/internal/hooks/adapters/claude"
	"github.com/moeritze/harbormaster/internal/hooks/adapters/cursor"
)

// newHook is the hidden agent-hook entrypoint: `harbormaster hook <agent>
// <event>` reads the agent's JSON payload from stdin (capped at
// claude.MaxStdin) and always exits 0. Every failure — unreadable/oversize
// stdin, a malformed payload, an unknown agent — is recorded via
// core.Logf (hook-errors.log in the state dir); an unreadable/oversize
// payload or an unknown agent additionally gets one stderr line, while a
// malformed (but readable) payload for a known agent fails open silently on
// both stdout and stderr. Nothing on stdout ever blocks the agent it is
// wired into.
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
				if err == nil {
					err = fmt.Errorf("payload exceeds %d bytes", claude.MaxStdin)
				}
				core.Logf("hook %s %s: %v", agent, event, err)
				_, _ = fmt.Fprintln(a.Stderr, "harbormaster hook: payload unreadable or over 1 MiB; allowing")
				return nil
			}
			switch agent {
			case "claude":
				ev, ok, err := claude.Parse(event, payload)
				if err != nil {
					core.Logf("hook %s %s: %v", agent, event, err)
					// A malformed payload from the agent is routine noise (a
					// version skew, an unexpected tool shape), not a
					// configuration problem worth surfacing on every call: fail
					// open quietly on stderr (same as the !ok "not for us"
					// case) and rely on hook-errors.log for the diagnostic.
					return nil
				}
				if !ok {
					return nil
				}
				if out := claude.Format(event, core.Handle(ev)); out != nil {
					_, _ = a.Stdout.Write(append(out, '\n'))
				}
				return nil
			case "cursor":
				ev, ok, err := cursor.Parse(event, payload)
				if err != nil {
					core.Logf("hook %s %s: %v", agent, event, err)
					return nil
				}
				if !ok {
					return nil
				}
				res := core.Handle(ev)
				var out []byte
				if event == "sessionStart" {
					out = cursor.FormatSession(event, ev.Session, res)
				} else {
					out = cursor.Format(event, res)
				}
				if out != nil {
					_, _ = a.Stdout.Write(append(out, '\n'))
				}
				return nil
			default:
				core.Logf("hook %s %s: unknown agent", agent, event)
				_, _ = fmt.Fprintf(a.Stderr, "harbormaster hook: unknown agent %q; allowing\n", agent)
				return nil
			}
		},
	}
	return cmd
}
