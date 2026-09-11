package claude_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/internal/hooks"
	"github.com/moeritze/harbormaster/internal/hooks/adapters/claude"
)

func TestParsePreToolUseBash(t *testing.T) {
	in := `{"session_id":"s1","cwd":"/wt/a","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"npm run dev"}}`
	ev, ok, err := claude.Parse("PreToolUse", []byte(in))
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if ev.Kind != hooks.PreShell || ev.Session != "s1" || ev.Cwd != "/wt/a" || ev.Command != "npm run dev" || ev.Agent != "claude" {
		t.Fatalf("%+v", ev)
	}
}

func TestParseIgnoresNonBashTools(t *testing.T) {
	in := `{"session_id":"s1","cwd":"/wt/a","tool_name":"Edit","tool_input":{"file_path":"x"}}`
	_, ok, err := claude.Parse("PreToolUse", []byte(in))
	if err != nil || ok {
		t.Fatal(err, ok)
	}
}

func TestParseSessionStartAndEnd(t *testing.T) {
	ev, ok, _ := claude.Parse("SessionStart", []byte(`{"session_id":"s1","cwd":"/wt/a","source":"startup"}`))
	if !ok || ev.Kind != hooks.SessionStart {
		t.Fatalf("%+v", ev)
	}
	ev, ok, _ = claude.Parse("SessionEnd", []byte(`{"session_id":"s1","cwd":"/wt/a","reason":"logout"}`))
	if !ok || ev.Kind != hooks.SessionEnd {
		t.Fatalf("%+v", ev)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, _, err := claude.Parse("PreToolUse", []byte(`{not json`)); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := claude.Parse("Nope", []byte(`{}`)); err == nil {
		t.Fatal("expected unknown event error")
	}
}

func TestFormatShapes(t *testing.T) {
	out := claude.Format("PreToolUse", hooks.Result{Decision: hooks.Deny, Reason: "no"})
	var m map[string]map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err, string(out))
	}
	h := m["hookSpecificOutput"]
	if h["hookEventName"] != "PreToolUse" || h["permissionDecision"] != "deny" || h["permissionDecisionReason"] != "no" {
		t.Fatalf("%v", h)
	}
	if got := claude.Format("PreToolUse", hooks.Result{Decision: hooks.Allow}); got != nil {
		t.Fatalf("silent allow must print nothing, got %s", got)
	}
	out = claude.Format("PreToolUse", hooks.Result{Decision: hooks.Allow, Context: "hint"})
	if !strings.Contains(string(out), `"permissionDecision":"allow"`) || !strings.Contains(string(out), `"additionalContext":"hint"`) {
		t.Fatalf("%s", out)
	}
	out = claude.Format("SessionStart", hooks.Result{Decision: hooks.Allow, Context: "ctx"})
	if !strings.Contains(string(out), `"hookEventName":"SessionStart"`) || !strings.Contains(string(out), `"additionalContext":"ctx"`) {
		t.Fatalf("%s", out)
	}
	if got := claude.Format("SessionEnd", hooks.Result{Decision: hooks.Allow}); got != nil {
		t.Fatalf("session end prints nothing, got %s", got)
	}
	out = claude.Format("PostToolUse", hooks.Result{Decision: hooks.Allow, Context: "c"})
	if !strings.Contains(string(out), `"hookEventName":"PostToolUse"`) {
		t.Fatalf("%s", out)
	}
}
