package cursor_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/internal/hooks"
	"github.com/moeritze/harbormaster/internal/hooks/adapters/cursor"
)

func TestParseBeforeShellExecution(t *testing.T) {
	in := `{"conversation_id":"c1","hook_event_name":"beforeShellExecution","workspace_roots":["/wt/a"],"command":"npm run dev","cwd":"/wt/a/web","sandbox":false}`
	ev, ok, err := cursor.Parse("beforeShellExecution", []byte(in))
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if ev.Kind != hooks.PreShell || ev.Agent != "cursor" || ev.Session != "c1" || ev.Cwd != "/wt/a/web" || ev.Command != "npm run dev" {
		t.Fatalf("%+v", ev)
	}
}

func TestParseFallsBackToSessionIDAndWorkspaceRoot(t *testing.T) {
	in := `{"session_id":"s9","workspace_roots":["/wt/root"],"is_background_agent":false}`
	ev, ok, err := cursor.Parse("sessionStart", []byte(in))
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if ev.Kind != hooks.SessionStart || ev.Session != "s9" || ev.Cwd != "/wt/root" {
		t.Fatalf("%+v", ev)
	}
}

func TestParseAfterShellAndSessionEnd(t *testing.T) {
	ev, ok, _ := cursor.Parse("afterShellExecution", []byte(`{"conversation_id":"c1","command":"npm run dev &","output":"","duration":12}`))
	if !ok || ev.Kind != hooks.PostShell || ev.Command != "npm run dev &" {
		t.Fatalf("%+v", ev)
	}
	ev, ok, _ = cursor.Parse("sessionEnd", []byte(`{"conversation_id":"c1","session_id":"s1","reason":"user_close","duration_ms":5}`))
	if !ok || ev.Kind != hooks.SessionEnd || ev.Reason != "user_close" || ev.Session != "c1" {
		t.Fatalf("%+v", ev)
	}
}

func TestParseRejectsGarbageAndUnknownEvent(t *testing.T) {
	if _, _, err := cursor.Parse("beforeShellExecution", []byte(`{nope`)); err == nil {
		t.Fatal("expected parse error")
	}
	if _, _, err := cursor.Parse("afterFileEdit", []byte(`{}`)); err == nil {
		t.Fatal("expected unknown event error")
	}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return m
}

func TestFormatShapes(t *testing.T) {
	m := decode(t, cursor.Format("beforeShellExecution", hooks.Result{Decision: hooks.Deny, Reason: "no way"}))
	if m["permission"] != "deny" || m["agent_message"] != "no way" || !strings.Contains(m["user_message"].(string), "harbormaster") {
		t.Fatalf("%v", m)
	}
	m = decode(t, cursor.Format("beforeShellExecution", hooks.Result{Decision: hooks.Ask, Reason: "sure?"}))
	if m["permission"] != "ask" || m["agent_message"] != "sure?" {
		t.Fatalf("%v", m)
	}
	// Cursor documents no "no opinion" form, so an allow prints nothing even with context.
	if got := cursor.Format("beforeShellExecution", hooks.Result{Decision: hooks.Allow, Context: "hint"}); got != nil {
		t.Fatalf("allow must print nothing, got %s", got)
	}
	if got := cursor.Format("beforeShellExecution", hooks.Result{Decision: hooks.Allow}); got != nil {
		t.Fatalf("silent allow must print nothing, got %s", got)
	}
	m = decode(t, cursor.FormatSession("sessionStart", "c1", hooks.Result{Decision: hooks.Allow, Context: "table"}))
	ctx, _ := m["additional_context"].(string)
	if !strings.HasPrefix(ctx, "table\n") {
		t.Fatalf("%v", m)
	}
	// Cursor's sessionStart env reaches later hook executions only, never
	// the agent's shell, so the context has to spell out the command that
	// does attribute a server to this conversation.
	if !strings.Contains(ctx, `HARBORMASTER_SESSION=c1 harbormaster run --label "<task>" -- <command>`) {
		t.Fatalf("missing the attribution line: %q", ctx)
	}
	env := m["env"].(map[string]any)
	if env["HARBORMASTER_AGENT"] != "cursor" || env["HARBORMASTER_SESSION"] != "c1" {
		t.Fatalf("%v", env)
	}
	if got := cursor.FormatSession("sessionStart", "", hooks.Result{Decision: hooks.Allow}); got != nil {
		t.Fatalf("no session and no context must print nothing, got %s", got)
	}
	if got := cursor.Format("afterShellExecution", hooks.Result{Decision: hooks.Allow, Context: "c"}); got != nil {
		t.Fatalf("afterShellExecution has no output fields, got %s", got)
	}
	if got := cursor.Format("sessionEnd", hooks.Result{Decision: hooks.Allow}); got != nil {
		t.Fatalf("sessionEnd is fire and forget, got %s", got)
	}
}

// TestFormatCapsMessages: the reason is built from registry rows, so a
// machine with many registered servers could push kilobytes into a Cursor
// permission prompt.
func TestFormatCapsMessages(t *testing.T) {
	long := strings.Repeat("ü", 4000) // 8000 bytes
	m := decode(t, cursor.Format("beforeShellExecution", hooks.Result{Decision: hooks.Deny, Reason: long}))
	msg := m["agent_message"].(string)
	if len(msg) > hooks.MaxMessage || !strings.HasSuffix(msg, "…") {
		t.Fatalf("agent_message not capped: %d bytes", len(msg))
	}
	if len(m["user_message"].(string)) > hooks.MaxMessage {
		t.Fatal("user_message not capped")
	}
}
