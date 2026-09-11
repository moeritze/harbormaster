package cli_test

import (
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/internal/registry"
)

func TestHookClaudeDeniesForeignKillAndExitsZero(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3100, PID: 77, Session: "other", Worktree: "/wt/b", Label: "api"})
	h.stdin = strings.NewReader(`{"session_id":"me","cwd":"/wt/a","tool_name":"Bash","tool_input":{"command":"lsof -ti:3100 | xargs kill"}}`)
	if err := h.run("hook", "claude", "PreToolUse"); err != nil {
		t.Fatalf("hook must exit 0: %v", err)
	}
	if !strings.Contains(h.out.String(), `"permissionDecision":"deny"`) || !strings.Contains(h.out.String(), "3100") {
		t.Fatalf("%s", h.out.String())
	}
}

func TestHookClaudeGarbageStdinAllows(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	h.stdin = strings.NewReader(`{{{`)
	if err := h.run("hook", "claude", "PreToolUse"); err != nil {
		t.Fatalf("must exit 0: %v", err)
	}
	if strings.TrimSpace(h.out.String()) != "" {
		t.Fatalf("garbage must print nothing on stdout, got %q", h.out.String())
	}
}

func TestHookClaudeStdinCap(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	h.stdin = strings.NewReader(`{"session_id":"me","tool_name":"Bash","tool_input":{"command":"` + strings.Repeat("a", 2<<20) + `"}}`)
	if err := h.run("hook", "claude", "PreToolUse"); err != nil {
		t.Fatalf("must exit 0: %v", err)
	}
}
