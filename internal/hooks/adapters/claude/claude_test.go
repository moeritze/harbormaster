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
	if !ok || ev.Kind != hooks.SessionEnd || ev.Reason != "logout" {
		t.Fatalf("%+v", ev)
	}
}

// TestParseSessionEndReason: the core needs the reason to tell a real end
// from a /clear or a resume, which keep the same servers running.
func TestParseSessionEndReason(t *testing.T) {
	for _, reason := range []string{"clear", "resume", "logout", "prompt_input_exit", "other"} {
		ev, ok, err := claude.Parse("SessionEnd", []byte(`{"session_id":"s1","reason":"`+reason+`"}`))
		if err != nil || !ok {
			t.Fatal(reason, err, ok)
		}
		if ev.Reason != reason {
			t.Fatalf("%q: got %q", reason, ev.Reason)
		}
	}
	ev, _, _ := claude.Parse("SessionEnd", []byte(`{"session_id":"s1"}`))
	if ev.Reason != "" {
		t.Fatalf("a payload with no reason must leave it empty: %q", ev.Reason)
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
	// An allow must never carry a permissionDecision: "allow" would skip
	// the user's own permission prompt. Omitting the field is "no opinion",
	// which still delivers additionalContext.
	out = claude.Format("PreToolUse", hooks.Result{Decision: hooks.Allow, Context: "hint"})
	if strings.Contains(string(out), "permissionDecision") {
		t.Fatalf("an allow must not auto-approve anything: %s", out)
	}
	if !strings.Contains(string(out), `"additionalContext":"hint"`) || !strings.Contains(string(out), `"hookEventName":"PreToolUse"`) {
		t.Fatalf("%s", out)
	}
	out = claude.Format("PreToolUse", hooks.Result{Decision: hooks.Ask, Reason: "who owns 3100?"})
	if !strings.Contains(string(out), `"permissionDecision":"ask"`) || !strings.Contains(string(out), `"permissionDecisionReason":"who owns 3100?"`) {
		t.Fatalf("%s", out)
	}
	if strings.Contains(string(out), "additionalContext") {
		t.Fatalf("an ask carries its reason, not context: %s", out)
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

// TestFormatCapsReason: same cap as the Cursor adapter — a reason built
// from a large registry must not become a multi-kilobyte permission prompt.
func TestFormatCapsReason(t *testing.T) {
	long := strings.Repeat("ü", 4000) // 8000 bytes
	out := claude.Format("PreToolUse", hooks.Result{Decision: hooks.Deny, Reason: long})
	var m map[string]map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	reason := m["hookSpecificOutput"]["permissionDecisionReason"].(string)
	if len(reason) > hooks.MaxMessage || !strings.HasSuffix(reason, "…") {
		t.Fatalf("reason not capped: %d bytes", len(reason))
	}
}
