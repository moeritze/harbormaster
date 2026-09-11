package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudeProjectDryRunAndReal(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	dir := t.TempDir()
	if err := h.run("install", "claude", "--project", dir, "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "settings.json") || !strings.Contains(h.out.String(), "SKILL.md") {
		t.Fatalf("%s", h.out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote")
	}
	if err := h.run("install", "claude", "--project", dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json")) //nolint:gosec // test-controlled t.TempDir path
	if !strings.Contains(string(b), "hook claude PreToolUse") {
		t.Fatalf("%s", b)
	}
	if err := h.run("uninstall", "claude", "--project", dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".claude", "settings.json")) //nolint:gosec // test-controlled t.TempDir path
	if strings.Contains(string(b), "harbormaster") {
		t.Fatalf("not removed: %s", b)
	}
}
