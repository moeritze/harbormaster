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
	// A --project install lands in a file that gets committed: this
	// machine's absolute path would be wrong in every other checkout.
	if !strings.Contains(string(b), `"harbormaster hook claude PreToolUse"`) {
		t.Fatalf("--project must install the bare command: %s", b)
	}
	if err := h.run("uninstall", "claude", "--project", dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".claude", "settings.json")) //nolint:gosec // test-controlled t.TempDir path
	if strings.Contains(string(b), "harbormaster") {
		t.Fatalf("not removed: %s", b)
	}
}

// TestInstallClaudeProjectHonoursExplicitCommand: --project only swaps in
// the portable command when the user did not ask for a specific one.
func TestInstallClaudeProjectHonoursExplicitCommand(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	dir := t.TempDir()
	if err := h.run("install", "claude", "--project", dir, "--command", "/opt/hm/harbormaster"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json")) //nolint:gosec // test-controlled t.TempDir path
	if !strings.Contains(string(b), `"/opt/hm/harbormaster hook claude PreToolUse"`) {
		t.Fatalf("%s", b)
	}
}

// TestInstallReportsTheBackupItWrote covers both the dry-run listing and
// the line a real write prints.
func TestInstallReportsTheBackupItWrote(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(cfg, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.run("install", "claude", "--project", dir, "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "would back up") || !strings.Contains(h.out.String(), "harbormaster-backup-") {
		t.Fatalf("dry run must name the backup: %s", h.out.String())
	}
	if strings.Contains(h.out.String(), "reformatted") {
		t.Fatalf("dry run must not claim a write: %s", h.out.String())
	}
	if err := h.run("install", "claude", "--project", dir); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	if !strings.Contains(out, "settings.json reformatted; backup at ") {
		t.Fatalf("%s", out)
	}
	backup := ""
	for _, line := range strings.Split(out, "\n") {
		if after, ok := strings.CutPrefix(line, "settings.json reformatted; backup at "); ok {
			backup = after
		}
	}
	b, err := os.ReadFile(backup) //nolint:gosec // path printed by the command under test, under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"theme":"dark"}` {
		t.Fatalf("backup is not the original file: %s", b)
	}
}

func TestInstallCursorAndAgentsMDProject(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	dir := t.TempDir()
	if err := h.run("install", "cursor", "--project", dir, "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "hooks.json") || !strings.Contains(h.out.String(), "harbormaster.mdc") {
		t.Fatalf("%s", h.out.String())
	}
	if err := h.run("install", "cursor", "--project", dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".cursor", "hooks.json")) //nolint:gosec // test-owned temp dir
	if !strings.Contains(string(b), `"harbormaster hook cursor beforeShellExecution"`) || !strings.Contains(string(b), `"version": 1`) {
		t.Fatalf("%s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "rules", "harbormaster.mdc")); err != nil {
		t.Fatal("rule missing")
	}
	if err := h.run("uninstall", "cursor", "--project", dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".cursor", "hooks.json")) //nolint:gosec // test-owned temp dir
	if strings.Contains(string(b), "harbormaster") {
		t.Fatalf("not removed: %s", b)
	}
	if err := h.run("install", "agents-md", "--project", dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test-owned temp dir
	if !strings.Contains(string(b), "harbormaster:start") {
		t.Fatalf("%s", b)
	}
	if err := h.run("uninstall", "agents-md", "--project", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("AGENTS.md that only held our block must be gone")
	}
	if c := exitCode(h.run("install", "codex")); c != 3 {
		t.Fatalf("unsupported agent must exit 3, got %d", c)
	}
}
