// Package cursor translates Cursor hook payloads to and from the
// harbormaster hook model. Formats verified against
// https://cursor.com/docs/hooks on 2026-09-11: every hook receives the
// common fields (conversation_id, generation_id, hook_event_name,
// workspace_roots, …); beforeShellExecution adds command/cwd/sandbox and
// answers with {permission: allow|deny|ask, user_message, agent_message};
// sessionStart answers with {env, additional_context}; afterShellExecution
// and sessionEnd use no output fields. Exit 0 means "use the JSON"; any
// other exit code fails open.
//
// Two facts shape everything below. sessionStart's `env` is handed to
// *subsequent hook executions* only — it never reaches the shell the agent
// runs commands in, so it cannot make the agent's own `harbormaster run`
// calls carry the conversation id (see FormatSession). And sessionEnd fires
// once per conversation, with reason ∈ completed | aborted | error |
// window_close | user_close; the per-turn event is `stop`, which
// harbormaster does not hook.
package cursor

import (
	"encoding/json"
	"fmt"

	"github.com/moeritze/harbormaster/internal/hooks"
)

// Events lists the Cursor hook events harbormaster handles.
var Events = []string{"sessionStart", "beforeShellExecution", "afterShellExecution", "sessionEnd"}

type payload struct {
	ConversationID string   `json:"conversation_id"`
	SessionID      string   `json:"session_id"`
	WorkspaceRoots []string `json:"workspace_roots"`
	Command        string   `json:"command"`
	Cwd            string   `json:"cwd"`
	Reason         string   `json:"reason"`
}

// Parse maps a Cursor payload to an Event. The session is the
// conversation_id (present on every hook), falling back to sessionStart's
// session_id; cwd falls back to the first workspace root.
func Parse(event string, stdin []byte) (hooks.Event, bool, error) {
	var p payload
	if err := json.Unmarshal(stdin, &p); err != nil {
		return hooks.Event{}, false, fmt.Errorf("parse hook payload: %w", err)
	}
	ev := hooks.Event{Agent: "cursor", Session: p.ConversationID, Cwd: p.Cwd}
	if ev.Session == "" {
		ev.Session = p.SessionID
	}
	if ev.Cwd == "" && len(p.WorkspaceRoots) > 0 {
		ev.Cwd = p.WorkspaceRoots[0]
	}
	switch event {
	case "sessionStart":
		ev.Kind = hooks.SessionStart
	case "sessionEnd":
		ev.Kind = hooks.SessionEnd
		ev.Reason = p.Reason
	case "beforeShellExecution":
		ev.Kind = hooks.PreShell
		ev.Command = p.Command
	case "afterShellExecution":
		ev.Kind = hooks.PostShell
		ev.Command = p.Command
	default:
		return hooks.Event{}, false, fmt.Errorf("unknown cursor hook event %q", event)
	}
	return ev, true, nil
}

type shellOutput struct {
	Permission   string `json:"permission"`
	UserMessage  string `json:"user_message,omitempty"`
	AgentMessage string `json:"agent_message,omitempty"`
}

type sessionOutput struct {
	Env               map[string]string `json:"env,omitempty"`
	AdditionalContext string            `json:"additional_context,omitempty"`
}

// Format renders the stdout Cursor expects for shell events. nil means
// print nothing.
//
// Cursor documents no "no opinion" answer for beforeShellExecution, and a
// {permission: "allow"} could bypass the user's own approval settings, so
// an allow — with or without context — prints nothing; the nudge is
// dropped for Cursor and the session-start context carries the guidance
// instead. Deny and Ask map to permission deny/ask with the reason as the
// agent_message and a short user_message, each capped at hooks.MaxMessage.
func Format(event string, r hooks.Result) []byte {
	if event != "beforeShellExecution" {
		return nil
	}
	var o shellOutput
	switch r.Decision {
	case hooks.Deny:
		o = shellOutput{Permission: "deny", UserMessage: "harbormaster: blocked; see the agent message", AgentMessage: r.Reason}
	case hooks.Ask:
		o = shellOutput{Permission: "ask", UserMessage: "harbormaster: please confirm; see the agent message", AgentMessage: r.Reason}
	default:
		return nil
	}
	o.AgentMessage = hooks.Clip(o.AgentMessage, hooks.MaxMessage)
	o.UserMessage = hooks.Clip(o.UserMessage, hooks.MaxMessage)
	b, err := json.Marshal(o)
	if err != nil {
		return nil
	}
	return b
}

// FormatSession renders sessionStart's stdout: the registry context plus an
// env block. nil means print nothing.
//
// The env block does NOT reach the agent's shell — Cursor hands it to later
// hook executions only — so it cannot make the agent's own `harbormaster
// run` calls carry the conversation id. It is still worth setting, because
// the hook processes that read it are the ones deciding ownership. What
// closes the gap for the agent is the extra context line below, which shows
// the one command that does attribute a server to this conversation; failing
// that, harbormaster falls back to treating servers started from a plain
// shell in the same worktree as this conversation's (see scope.owns).
func FormatSession(event, session string, r hooks.Result) []byte {
	if event != "sessionStart" {
		return nil
	}
	o := sessionOutput{AdditionalContext: r.Context}
	if session != "" {
		o.Env = map[string]string{"HARBORMASTER_AGENT": "cursor", "HARBORMASTER_SESSION": session}
		line := "To attribute servers to this conversation, start them with: HARBORMASTER_SESSION=" + session + ` harbormaster run --label "<task>" -- <command>`
		if o.AdditionalContext == "" {
			o.AdditionalContext = line
		} else {
			o.AdditionalContext += "\n" + line
		}
	}
	if o.Env == nil && o.AdditionalContext == "" {
		return nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return nil
	}
	return b
}
