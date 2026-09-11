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

func copts(dir string, rule []byte) install.CursorOptions {
	return install.CursorOptions{ConfigDir: dir, Command: "harbormaster", Rule: rule, Now: func() time.Time { return time.Unix(1700000000, 0) }}
}

func readHooksJSON(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "hooks.json")) //nolint:gosec // test-owned temp dir
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCursorInstallShape(t *testing.T) {
	dir := t.TempDir()
	r, err := install.Cursor(copts(dir, []byte("---\ndescription: x\nalwaysApply: false\n---\nbody\n")))
	if err != nil {
		t.Fatal(err)
	}
	m := readHooksJSON(t, dir)
	if v, _ := m["version"].(float64); v != 1 {
		t.Fatalf("version %v", m["version"])
	}
	hooks := m["hooks"].(map[string]any)
	for _, ev := range []string{"sessionStart", "beforeShellExecution", "afterShellExecution", "sessionEnd"} {
		list, ok := hooks[ev].([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("%s: %v", ev, hooks[ev])
		}
		e := list[0].(map[string]any)
		if e["command"] != "harbormaster hook cursor "+ev || e["type"] != "command" {
			t.Fatalf("%s: %v", ev, e)
		}
	}
	if len(r.Added) != 4 || r.Backup != "" {
		t.Fatalf("%+v", r)
	}
	b2, _ := os.ReadFile(filepath.Join(dir, "rules", "harbormaster.mdc")) //nolint:gosec // test-owned temp dir
	if !strings.HasPrefix(string(b2), "---\ndescription") {
		t.Fatal("rule not written")
	}
}

func TestCursorInstallPreservesForeignAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	orig := `{"version":1,"hooks":{"beforeShellExecution":[{"command":"./mine.sh","type":"command"}]},"other":true}`
	_ = os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(orig), 0o600)
	r, err := install.Cursor(copts(dir, nil))
	if err != nil {
		t.Fatal(err)
	}
	if r.Backup == "" {
		t.Fatal("expected backup")
	}
	if b, _ := os.ReadFile(r.Backup); string(b) != orig {
		t.Fatal("backup differs")
	}
	m := readHooksJSON(t, dir)
	if m["other"] != true {
		t.Fatal("unrelated key lost")
	}
	if l := m["hooks"].(map[string]any)["beforeShellExecution"].([]any); len(l) != 2 {
		t.Fatalf("expected mine + ours, got %d", len(l))
	}
	r, err = install.Cursor(copts(dir, nil))
	if err != nil || len(r.Added) != 0 || len(r.Skipped) != 4 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules")); !os.IsNotExist(err) {
		t.Fatal("no rule for a user-level install")
	}
}

func TestCursorUninstallRemovesOnlyOurs(t *testing.T) {
	dir := t.TempDir()
	orig := `{"version":1,"hooks":{"beforeShellExecution":[{"command":"./mine.sh","type":"command"}]}}`
	_ = os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(orig), 0o600)
	if _, err := install.Cursor(copts(dir, []byte("rule"))); err != nil {
		t.Fatal(err)
	}
	r, err := install.CursorUninstall(copts(dir, nil))
	if err != nil || len(r.Removed) != 4 {
		t.Fatalf("%+v %v", r, err)
	}
	m := readHooksJSON(t, dir)
	hooks := m["hooks"].(map[string]any)
	if _, ok := hooks["sessionStart"]; ok {
		t.Fatal("empty event must be dropped")
	}
	if l := hooks["beforeShellExecution"].([]any); len(l) != 1 {
		t.Fatalf("foreign entry must survive: %v", l)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules", "harbormaster.mdc")); !os.IsNotExist(err) {
		t.Fatal("rule must be removed")
	}
}

func TestCursorDryRunAndLookalike(t *testing.T) {
	dir := t.TempDir()
	o := copts(dir, nil)
	o.DryRun = true
	r, err := install.Cursor(o)
	if err != nil || len(r.Actions) == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks.json")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote")
	}
	orig := `{"version":1,"hooks":{"beforeShellExecution":[{"command":"echo \"harbormaster hook cursor beforeShellExecution\"","type":"command"}]}}`
	_ = os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(orig), 0o600)
	r, err = install.Cursor(copts(dir, nil))
	if err != nil || len(r.Added) != 4 {
		t.Fatalf("lookalike must not block install: %+v %v", r, err)
	}
	r, err = install.CursorUninstall(copts(dir, nil))
	if err != nil {
		t.Fatal(err)
	}
	if l := readHooksJSON(t, dir)["hooks"].(map[string]any)["beforeShellExecution"].([]any); len(l) != 1 {
		t.Fatalf("lookalike must survive uninstall: %v", l)
	}
}

func TestAgentsMDUpsertAndRemove(t *testing.T) {
	p := filepath.Join(t.TempDir(), "AGENTS.md")
	r, err := install.AgentsMD(p, false)
	if err != nil || len(r.Added) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	b, _ := os.ReadFile(p) //nolint:gosec // test-owned temp file
	if !strings.Contains(string(b), "<!-- harbormaster:start -->") || !strings.Contains(string(b), "harbormaster run") {
		t.Fatalf("%s", b)
	}
	r, _ = install.AgentsMD(p, false)
	if len(r.Skipped) != 1 {
		t.Fatalf("second run must be a no-op: %+v", r)
	}
	// Existing content before and after the block survives an update.
	_ = os.WriteFile(p, []byte("# Project\n\nrules here\n\n<!-- harbormaster:start -->\nold\n<!-- harbormaster:end -->\n\n## After\n"), 0o600)
	r, _ = install.AgentsMD(p, false)
	b, _ = os.ReadFile(p) //nolint:gosec // test-owned temp file
	s := string(b)
	if len(r.Added) != 1 || !strings.Contains(s, "# Project") || !strings.Contains(s, "## After") || strings.Contains(s, "\nold\n") || strings.Count(s, "harbormaster:start") != 1 {
		t.Fatalf("%+v\n%s", r, s)
	}
	r, err = install.AgentsMDUninstall(p, false)
	if err != nil || len(r.Removed) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	b, _ = os.ReadFile(p) //nolint:gosec // test-owned temp file
	if strings.Contains(string(b), "harbormaster") || !strings.Contains(string(b), "## After") {
		t.Fatalf("%s", b)
	}
	_ = os.WriteFile(p, []byte("<!-- harbormaster:start -->\nx\n<!-- harbormaster:end -->\n"), 0o600)
	if _, err := install.AgentsMDUninstall(p, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file that only held the block must be deleted")
	}
	if _, err := install.AgentsMD(p, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("dry run must not create the file")
	}
}
