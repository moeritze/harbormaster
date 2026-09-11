package cli_test

import (
	"os"
	"path/filepath"
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

// TestHookClaudeParseErrorLogsToHookErrorsLog covers the fix-round-1 review
// finding: a malformed payload must stay silent on stdout/stderr but still
// leave a diagnostic behind in hook-errors.log under the state dir.
func TestHookClaudeParseErrorLogsToHookErrorsLog(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	h.stdin = strings.NewReader(`{{{`)
	if err := h.run("hook", "claude", "PreToolUse"); err != nil {
		t.Fatalf("must exit 0: %v", err)
	}
	if strings.TrimSpace(h.out.String()) != "" {
		t.Fatalf("must still print nothing, got %q", h.out.String())
	}
	b, err := os.ReadFile(filepath.Join(h.app.Store.Dir(), "hook-errors.log"))
	if err != nil {
		t.Fatalf("hook-errors.log: %v", err)
	}
	if !strings.Contains(string(b), "parse") {
		t.Fatalf("log missing parse diagnostic: %s", b)
	}
}

func TestHookClaudeStdinCap(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	h.stdin = strings.NewReader(`{"session_id":"me","tool_name":"Bash","tool_input":{"command":"` + strings.Repeat("a", 2<<20) + `"}}`)
	if err := h.run("hook", "claude", "PreToolUse"); err != nil {
		t.Fatalf("must exit 0: %v", err)
	}
}

// TestHookClaudeNudgeNeverAutoApproves: a PreToolUse "allow" would skip the
// user's own permission prompt, so harbormaster's context-only answers must
// carry no permissionDecision at all.
func TestHookClaudeNudgeNeverAutoApproves(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	h.stdin = strings.NewReader(`{"session_id":"me","cwd":"/wt/a","tool_name":"Bash","tool_input":{"command":"npm run dev"}}`)
	if err := h.run("hook", "claude", "PreToolUse"); err != nil {
		t.Fatalf("hook must exit 0: %v", err)
	}
	out := h.out.String()
	if strings.Contains(out, "permissionDecision") {
		t.Fatalf("must not auto-approve: %s", out)
	}
	if !strings.Contains(out, "additionalContext") || !strings.Contains(out, "harbormaster run") {
		t.Fatalf("%s", out)
	}
}

// TestHookClaudeBlindKillAsks: a kill that names no port and no pid, with
// another session's server registered, must reach Claude Code as "ask".
func TestHookClaudeBlindKillAsks(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3100, PID: 77, Session: "other", Worktree: "/wt/b", Label: "api"})
	h.stdin = strings.NewReader(`{"session_id":"me","cwd":"/wt/a","tool_name":"Bash","tool_input":{"command":"pkill -f node"}}`)
	if err := h.run("hook", "claude", "PreToolUse"); err != nil {
		t.Fatalf("hook must exit 0: %v", err)
	}
	out := h.out.String()
	if !strings.Contains(out, `"permissionDecision":"ask"`) || !strings.Contains(out, "3100") {
		t.Fatalf("%s", out)
	}
}

// TestHookClaudeSessionEndClearKeepsServers: /clear is not the end of a
// session, so the servers it started stay registered and running.
func TestHookClaudeSessionEndClearKeepsServers(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	h.seed(t, registry.Entry{ID: "t", Port: 3100, PID: 77, Session: "me", Worktree: "/wt/a"})
	h.stdin = strings.NewReader(`{"session_id":"me","cwd":"/wt/a","reason":"clear"}`)
	if err := h.run("hook", "claude", "SessionEnd"); err != nil {
		t.Fatalf("hook must exit 0: %v", err)
	}
	f, err := h.app.Store.Peek()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 1 {
		t.Fatalf("clear must release nothing: %+v", f.Entries)
	}
}
