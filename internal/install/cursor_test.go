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
	// afterShellExecution is deliberately not installed: Cursor documents no
	// output fields for it, so it would run a process per shell command and
	// have nowhere to put the answer.
	if _, ok := hooks["afterShellExecution"]; ok {
		t.Fatalf("afterShellExecution must not be installed: %v", hooks)
	}
	for _, ev := range []string{"sessionStart", "beforeShellExecution", "sessionEnd"} {
		list, ok := hooks[ev].([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("%s: %v", ev, hooks[ev])
		}
		e := list[0].(map[string]any)
		if e["command"] != "harbormaster hook cursor "+ev || e["type"] != "command" {
			t.Fatalf("%s: %v", ev, e)
		}
	}
	if len(r.Added) != 3 || r.Backup != "" {
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
	if err != nil || len(r.Added) != 0 || len(r.Skipped) != 3 {
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
	r, err := install.CursorUninstall(copts(dir, []byte("rule")))
	if err != nil || len(r.Removed) != 3 {
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
	if err != nil || len(r.Added) != 3 {
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

// TestAgentsMDFileWithoutTrailingNewline covers the off-by-one that used to
// slice past the end marker: a file that ends exactly at the marker, with no
// newline after it, must install, update and uninstall cleanly.
func TestAgentsMDFileWithoutTrailingNewline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "AGENTS.md")
	body := "# Project\n\nrules here\n\n<!-- harbormaster:start -->\nold block\n<!-- harbormaster:end -->"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := install.AgentsMD(p, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p) //nolint:gosec // test-owned temp file
	s := string(b)
	if !strings.Contains(s, "# Project") || !strings.Contains(s, "rules here") {
		t.Fatalf("user content lost: %q", s)
	}
	if strings.Contains(s, "old block") || strings.Count(s, "harbormaster:start") != 1 || strings.Count(s, "harbormaster:end") != 1 {
		t.Fatalf("block not replaced exactly once: %q", s)
	}
	if !strings.HasSuffix(s, "<!-- harbormaster:end -->\n") {
		t.Fatalf("block must end the file cleanly: %q", s)
	}
	// Now idempotent.
	r, err := install.AgentsMD(p, false)
	if err != nil || len(r.Skipped) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := install.AgentsMDUninstall(p, false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p) //nolint:gosec // test-owned temp file
	if s := string(b); strings.Contains(s, "harbormaster") || !strings.Contains(s, "rules here") {
		t.Fatalf("uninstall must remove exactly the block: %q", s)
	}
}

// TestAgentsMDRefusesBrokenMarkers: AGENTS.md is a file humans edit. A stray
// or duplicated marker means harbormaster cannot tell where its block ends,
// and guessing would eat the user's own text.
func TestAgentsMDRefusesBrokenMarkers(t *testing.T) {
	cases := map[string]string{
		"orphan start":     "# Mine\n\n<!-- harbormaster:start -->\nimportant user content\n",
		"orphan end":       "# Mine\n\nimportant user content\n<!-- harbormaster:end -->\n",
		"end before start": "<!-- harbormaster:end -->\nuser text\n<!-- harbormaster:start -->\n",
		"duplicate blocks": "<!-- harbormaster:start -->\na\n<!-- harbormaster:end -->\n\n<!-- harbormaster:start -->\nb\n<!-- harbormaster:end -->\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "AGENTS.md")
			if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, call := range []func() error{
				func() error { _, err := install.AgentsMD(p, false); return err },
				func() error { _, err := install.AgentsMDUninstall(p, false); return err },
			} {
				if err := call(); err == nil {
					t.Fatal("expected a refusal")
				} else if !strings.Contains(err.Error(), "harbormaster") {
					t.Fatalf("unhelpful error: %v", err)
				}
				b, _ := os.ReadFile(p) //nolint:gosec // test-owned temp file
				if string(b) != doc {
					t.Fatalf("file was rewritten:\n%q", b)
				}
			}
		})
	}
}

// TestCursorUserLevelUninstallLeavesRulesAlone: ~/.cursor/rules holds the
// user's own rules, and only a --project install ever wrote one there.
func TestCursorUserLevelUninstallLeavesRulesAlone(t *testing.T) {
	dir := t.TempDir()
	if _, err := install.Cursor(copts(dir, nil)); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rules, 0o750); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(rules, "harbormaster.mdc")
	if err := os.WriteFile(mine, []byte("my own rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := install.CursorUninstall(copts(dir, nil))
	if err != nil {
		t.Fatal(err)
	}
	if r.SkillPath != "" {
		t.Fatalf("a user-level uninstall must not name a rule file: %q", r.SkillPath)
	}
	b, err := os.ReadFile(mine) //nolint:gosec // test-owned temp file
	if err != nil || string(b) != "my own rule" {
		t.Fatalf("user rule was touched: %q %v", b, err)
	}
}

// TestInstallersRefuseShellMetacharacters: the command string is written
// into a settings file and run as a command line, so a path carrying shell
// syntax could inject a second command into every hook invocation.
func TestInstallersRefuseShellMetacharacters(t *testing.T) {
	for _, bad := range []string{
		"/opt/hm; rm -rf /",
		"/opt/hm$(id)",
		"/opt/hm`id`",
		`/opt/hm\x`,
		`/opt/"hm"`,
		"/opt/hm'x'",
		"/opt/hm|cat",
		"/opt/hm&x",
		"/opt/hm<x",
		"/opt/hm>x",
		"/opt/hm(x)",
	} {
		dir := t.TempDir()
		o := install.Options{ConfigDir: dir, Command: bad, Skill: []byte("s")}
		if _, err := install.Claude(o); err == nil {
			t.Fatalf("claude accepted %q", bad)
		}
		co := install.CursorOptions{ConfigDir: dir, Command: bad}
		if _, err := install.Cursor(co); err == nil {
			t.Fatalf("cursor accepted %q", bad)
		}
		if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
			t.Fatal("a refused command must write nothing")
		}
		if _, err := os.Stat(filepath.Join(dir, "hooks.json")); !os.IsNotExist(err) {
			t.Fatal("a refused command must write nothing")
		}
	}
	// A path with a space is quoted, not refused.
	dir := t.TempDir()
	if _, err := install.Cursor(install.CursorOptions{ConfigDir: dir, Command: "/Apps/My Tools/harbormaster"}); err != nil {
		t.Fatalf("a space must still be allowed: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "hooks.json")) //nolint:gosec // test-owned temp dir
	if !strings.Contains(string(b), `\"/Apps/My Tools/harbormaster\" hook cursor sessionStart`) {
		t.Fatalf("%s", b)
	}
}

// TestBackupNeverOverwritesAnEarlierOne: the backup name carries a
// one-second timestamp, so an install and an uninstall in the same second
// used to leave the user with only the second copy.
func TestBackupNeverOverwritesAnEarlierOne(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	const orig = `{"theme":"dark"}`
	if err := os.WriteFile(settings, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	fixed := func() time.Time { return time.Unix(1700000000, 0) }
	o := install.Options{ConfigDir: dir, Command: "harbormaster", Skill: []byte("s"), Now: fixed}
	first, err := install.Claude(o)
	if err != nil || first.Backup == "" {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := install.ClaudeUninstall(o)
	if err != nil || second.Backup == "" {
		t.Fatalf("%+v %v", second, err)
	}
	if first.Backup == second.Backup {
		t.Fatalf("the same clock second must not reuse a backup name: %s", first.Backup)
	}
	if !strings.HasSuffix(second.Backup, "-1") {
		t.Fatalf("expected the -1 suffix, got %s", second.Backup)
	}
	b, err := os.ReadFile(first.Backup) //nolint:gosec // path produced by the code under test, under t.TempDir
	if err != nil || string(b) != orig {
		t.Fatalf("the first backup must still hold the user's original file: %q %v", b, err)
	}
	b, err = os.ReadFile(second.Backup) //nolint:gosec // path produced by the code under test, under t.TempDir
	if err != nil || !strings.Contains(string(b), "hook claude") {
		t.Fatalf("the second backup must hold the post-install file: %q %v", b, err)
	}
}
