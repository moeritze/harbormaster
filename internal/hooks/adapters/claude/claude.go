// Package claude translates Claude Code hook payloads to and from the
// harbormaster hook model. Formats verified against the Claude Code docs
// (hooks reference) on 2026-09-11.
package claude

import (
	"encoding/json"
	"fmt"

	"github.com/moeritze/harbormaster/internal/hooks"
)

// MaxStdin caps the hook payload we are willing to parse.
const MaxStdin = 1 << 20

// Events lists the Claude Code events harbormaster handles.
var Events = []string{"SessionStart", "PreToolUse", "PostToolUse", "SessionEnd"}

type payload struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
	// Reason is SessionEnd's: clear|resume|logout|prompt_input_exit|other.
	Reason string `json:"reason"`
}

// Parse maps a Claude Code payload to an Event. ok=false means the event
// is not for us (a non-Bash tool) and the caller prints nothing.
func Parse(event string, stdin []byte) (hooks.Event, bool, error) {
	var p payload
	if err := json.Unmarshal(stdin, &p); err != nil {
		return hooks.Event{}, false, fmt.Errorf("parse hook payload: %w", err)
	}
	ev := hooks.Event{Agent: "claude", Session: p.SessionID, Cwd: p.Cwd}
	switch event {
	case "SessionStart":
		ev.Kind = hooks.SessionStart
	case "SessionEnd":
		ev.Kind = hooks.SessionEnd
		ev.Reason = p.Reason
	case "PreToolUse", "PostToolUse":
		if p.ToolName != "Bash" {
			return hooks.Event{}, false, nil
		}
		ev.Command = p.ToolInput.Command
		if event == "PreToolUse" {
			ev.Kind = hooks.PreShell
		} else {
			ev.Kind = hooks.PostShell
		}
	default:
		return hooks.Event{}, false, fmt.Errorf("unknown claude hook event %q", event)
	}
	return ev, true, nil
}

type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

type output struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

// Format renders the stdout Claude Code expects. nil means print nothing.
//
// On PreToolUse a permissionDecision of "allow" *skips* the user's own
// permission prompt, so harbormaster never emits one: an allow with context
// omits permissionDecision entirely ("no opinion" — normal permission flow),
// which still delivers additionalContext. Only Deny ("deny") and Ask
// ("ask", which forces the prompt with the reason shown) set the field.
// The reason is capped at hooks.MaxMessage.
func Format(event string, r hooks.Result) []byte {
	o := hookSpecificOutput{HookEventName: event}
	switch event {
	case "SessionStart", "PostToolUse":
		if r.Context == "" {
			return nil
		}
		o.AdditionalContext = r.Context
	case "PreToolUse":
		switch r.Decision {
		case hooks.Deny:
			o.PermissionDecision = "deny"
			o.PermissionDecisionReason = r.Reason
		case hooks.Ask:
			o.PermissionDecision = "ask"
			o.PermissionDecisionReason = r.Reason
		case hooks.Allow:
			if r.Context == "" {
				return nil
			}
			o.AdditionalContext = r.Context
		}
	default:
		return nil
	}
	o.PermissionDecisionReason = hooks.Clip(o.PermissionDecisionReason, hooks.MaxMessage)
	b, err := json.Marshal(output{o})
	if err != nil {
		return nil
	}
	return b
}
