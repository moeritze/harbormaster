package install_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/install"
)

func opts(dir string) install.Options {
	return install.Options{ConfigDir: dir, Command: "harbormaster", Skill: []byte("---\nname: harbormaster\ndescription: x\n---\nbody\n"), Now: func() time.Time { return time.Unix(1700000000, 0) }}
}

func readSettings(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json")) //nolint:gosec // test-controlled t.TempDir path
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestInstallIntoMissingSettings(t *testing.T) {
	dir := t.TempDir()
	r, err := install.Claude(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	m := readSettings(t, dir)
	hooks := m["hooks"].(map[string]any)
	for _, ev := range []string{"SessionStart", "PreToolUse", "PostToolUse", "SessionEnd"} {
		if _, ok := hooks[ev]; !ok {
			t.Fatalf("missing %s", ev)
		}
	}
	if len(r.Added) != 4 || r.Backup != "" {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "skills", "harbormaster", "SKILL.md")); !strings.HasPrefix(string(b), "---\nname: harbormaster") { //nolint:gosec // test-controlled t.TempDir path
		t.Fatal("skill not written")
	}
}

func TestInstallPreservesExistingSettingsAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	orig := `{"theme":"dark","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}]},"permissions":{"allow":["Bash(ls *)"]}}`
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(orig), 0o600)
	r, err := install.Claude(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if r.Backup == "" {
		t.Fatal("expected a backup")
	}
	if b, _ := os.ReadFile(r.Backup); string(b) != orig {
		t.Fatal("backup must be byte-identical to the original")
	}
	m := readSettings(t, dir)
	if m["theme"] != "dark" || m["permissions"] == nil {
		t.Fatal("unrelated keys lost")
	}
	pre := m["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("expected rtk entry + ours, got %d", len(pre))
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := install.Claude(opts(dir)); err != nil {
		t.Fatal(err)
	}
	r, err := install.Claude(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Added) != 0 || len(r.Skipped) != 4 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json.harbormaster-backup-1700000000")); err == nil {
		t.Fatal("no backup on a no-op run")
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	o := opts(dir)
	o.DryRun = true
	r, err := install.Claude(o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote settings")
	}
	if len(r.Actions) < 2 {
		t.Fatalf("dry run must list actions: %+v", r)
	}
}

func TestUninstallRemovesOnlyOurs(t *testing.T) {
	dir := t.TempDir()
	orig := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}]}}`
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(orig), 0o600)
	if _, err := install.Claude(opts(dir)); err != nil {
		t.Fatal(err)
	}
	r, err := install.ClaudeUninstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Removed) != 4 {
		t.Fatalf("%+v", r)
	}
	m := readSettings(t, dir)
	hooks := m["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"]; ok {
		t.Fatal("empty event arrays must be dropped")
	}
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("rtk entry must survive: %v", pre)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "harbormaster")); !os.IsNotExist(err) {
		t.Fatal("skill dir must be removed when it holds only our file")
	}
}

func TestUninstallKeepsSkillDirWithForeignFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := install.Claude(opts(dir)); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "skills", "harbormaster", "notes.md"), []byte("mine"), 0o600)
	if _, err := install.ClaudeUninstall(opts(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "harbormaster", "notes.md")); err != nil {
		t.Fatal("foreign file must survive")
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "harbormaster", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("our SKILL.md must be removed")
	}
}

func TestInstallRefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{oops"), 0o600)
	if _, err := install.Claude(opts(dir)); err == nil {
		t.Fatal("must refuse to touch an unparseable settings.json")
	}
}
