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

// TestForeignHookSurvivesLookalikeCommand covers a user hook whose command
// merely contains our Marker as a substring (e.g. it echoes it). It must not
// be treated as ours: Claude() must still install its own PreToolUse entry
// alongside it, and ClaudeUninstall() must leave it in place.
func TestForeignHookSurvivesLookalikeCommand(t *testing.T) {
	dir := t.TempDir()
	orig := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo \"harbormaster hook claude PreToolUse\""}]}]}}`
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(orig), 0o600)
	r, err := install.Claude(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range r.Added {
		if ev == "PreToolUse" {
			found = true
		}
	}
	if !found {
		t.Fatalf("lookalike command must not block install: %+v", r)
	}
	m := readSettings(t, dir)
	pre := m["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("expected lookalike entry + ours, got %d", len(pre))
	}
	ur, err := install.ClaudeUninstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	m = readSettings(t, dir)
	pre = m["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("lookalike command must survive uninstall: %v (report %+v)", pre, ur)
	}
	entry := pre[0].(map[string]any)
	hs := entry["hooks"].([]any)[0].(map[string]any)
	if hs["command"] != `echo "harbormaster hook claude PreToolUse"` {
		t.Fatalf("wrong entry removed: %v", entry)
	}
}

// TestUninstallMatchesAbsolutePathAndAlias covers the two forms our own
// installer's Command option may take: an absolute path to the binary, and
// the "hm" alias.
func TestUninstallMatchesAbsolutePathAndAlias(t *testing.T) {
	orig := `{"hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"/usr/local/bin/harbormaster hook claude SessionEnd"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"hm hook claude PreToolUse"}]}]}}`
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(orig), 0o600)
	r, err := install.ClaudeUninstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Removed) != 2 {
		t.Fatalf("expected both entries removed, got %+v", r)
	}
	m := readSettings(t, dir)
	if hooks, ok := m["hooks"]; ok {
		t.Fatalf("both events must be fully removed, got %v", hooks)
	}
}

// TestInstalledEntriesAreExactlyThese is the golden shape of what an
// install writes. Matchers and timeouts are the contract with Claude Code:
// a typo in one silently disables a hook, and nothing else in the suite
// would notice.
func TestInstalledEntriesAreExactlyThese(t *testing.T) {
	dir := t.TempDir()
	if _, err := install.Claude(opts(dir)); err != nil {
		t.Fatal(err)
	}
	hooks, ok := readSettings(t, dir)["hooks"].(map[string]any)
	if !ok {
		t.Fatal("no hooks object written")
	}
	want := []struct {
		event, matcher, command string
		timeout                 float64
	}{
		{"SessionStart", "startup|resume|clear|compact|fork", "harbormaster hook claude SessionStart", 5},
		{"PreToolUse", "Bash", "harbormaster hook claude PreToolUse", 5},
		{"PostToolUse", "Bash", "harbormaster hook claude PostToolUse", 5},
		{"SessionEnd", "", "harbormaster hook claude SessionEnd", 3},
	}
	if len(hooks) != len(want) {
		t.Fatalf("wrote %d events, want %d: %v", len(hooks), len(want), hooks)
	}
	for _, w := range want {
		list, ok := hooks[w.event].([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("%s: %v", w.event, hooks[w.event])
		}
		entry, ok := list[0].(map[string]any)
		if !ok {
			t.Fatalf("%s: %v", w.event, list[0])
		}
		got, has := entry["matcher"]
		if (w.matcher == "") != !has {
			t.Fatalf("%s: matcher present=%v, want %q", w.event, has, w.matcher)
		}
		if has && got != w.matcher {
			t.Fatalf("%s: matcher %v, want %q", w.event, got, w.matcher)
		}
		hs, ok := entry["hooks"].([]any)
		if !ok || len(hs) != 1 {
			t.Fatalf("%s: %v", w.event, entry["hooks"])
		}
		h, ok := hs[0].(map[string]any)
		if !ok {
			t.Fatalf("%s: %v", w.event, hs[0])
		}
		if h["type"] != "command" || h["command"] != w.command || h["timeout"] != w.timeout {
			t.Fatalf("%s: %v, want type=command command=%q timeout=%v", w.event, h, w.command, w.timeout)
		}
	}
}

// TestSecondInstallIsANoOpForAnyBinaryName: ownership is recognised by the
// shape of the command, not by the binary's name, so a harbormaster
// installed as /tmp/hm-final still recognises its own entries.
func TestSecondInstallIsANoOpForAnyBinaryName(t *testing.T) {
	dir := t.TempDir()
	o := opts(dir)
	o.Command = "/tmp/hm-final"
	if _, err := install.Claude(o); err != nil {
		t.Fatal(err)
	}
	r, err := install.Claude(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Added) != 0 || len(r.Skipped) != 4 {
		t.Fatalf("second install must be a no-op: %+v", r)
	}
	ur, err := install.ClaudeUninstall(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(ur.Removed) != 4 {
		t.Fatalf("uninstall must remove all four: %+v", ur)
	}
	if hooks, ok := readSettings(t, dir)["hooks"]; ok {
		t.Fatalf("nothing of ours may survive: %v", hooks)
	}
}

// TestCommandPathWithSpaceIsQuotedAndMatched: a binary under a path with a
// space has to reach the shell quoted, and the quoted form has to be
// recognised as ours on the next run.
func TestCommandPathWithSpaceIsQuotedAndMatched(t *testing.T) {
	dir := t.TempDir()
	o := opts(dir)
	o.Command = "/Applications/My Tools/harbormaster"
	if _, err := install.Claude(o); err != nil {
		t.Fatal(err)
	}
	hooks := readSettings(t, dir)["hooks"].(map[string]any)
	entry := hooks["PreToolUse"].([]any)[0].(map[string]any)
	got := entry["hooks"].([]any)[0].(map[string]any)["command"]
	if got != `"/Applications/My Tools/harbormaster" hook claude PreToolUse` {
		t.Fatalf("command not quoted: %v", got)
	}
	r, err := install.Claude(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Added) != 0 || len(r.Skipped) != 4 {
		t.Fatalf("a quoted command must be recognised as ours: %+v", r)
	}
	ur, err := install.ClaudeUninstall(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(ur.Removed) != 4 {
		t.Fatalf("%+v", ur)
	}
}

func TestInstallRefusesSymlinkedSettings(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "settings.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := install.Claude(opts(dir))
	if err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("install must refuse a symlinked settings.json, got %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "{}" { //nolint:gosec // test-controlled t.TempDir path
		t.Fatalf("wrote through the symlink: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "harbormaster", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("a refused install must write nothing at all")
	}
	if _, err := install.ClaudeUninstall(opts(dir)); err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("uninstall must refuse it too, got %v", err)
	}
}

func TestInstallRefusesSymlinkedSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "harbormaster")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "real.md")
	if err := os.WriteFile(target, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := install.Claude(opts(dir))
	if err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("install must refuse a symlinked SKILL.md, got %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "mine" { //nolint:gosec // test-controlled t.TempDir path
		t.Fatalf("wrote through the symlink: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("a refused install must not have written settings.json either")
	}
}

func TestDryRunNamesTheBackupItWouldWrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	o := opts(dir)
	o.DryRun = true
	r, err := install.Claude(o)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "settings.json") + ".harbormaster-backup-1700000000"
	if r.Backup != want {
		t.Fatalf("Backup %q, want %q", r.Backup, want)
	}
	if joined := strings.Join(r.Actions, "\n"); !strings.Contains(joined, want) {
		t.Fatalf("dry-run actions must name the backup:\n%s", joined)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("dry run wrote the backup")
	}
}

func TestDryRunOnAMissingFileNamesNoBackup(t *testing.T) {
	o := opts(t.TempDir())
	o.DryRun = true
	r, err := install.Claude(o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Backup != "" {
		t.Fatalf("nothing to back up, got %q", r.Backup)
	}
}

func TestUninstallBacksUpBeforeRewriting(t *testing.T) {
	dir := t.TempDir()
	orig := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}]}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := install.Claude(opts(dir)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "settings.json")) //nolint:gosec // test-controlled t.TempDir path
	if err != nil {
		t.Fatal(err)
	}
	r, err := install.ClaudeUninstall(opts(dir))
	if err != nil {
		t.Fatal(err)
	}
	if r.Backup == "" {
		t.Fatal("uninstall must back up before rewriting")
	}
	b, err := os.ReadFile(r.Backup) //nolint:gosec // path comes from the report, under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(before) {
		t.Fatalf("backup must be byte-identical to what it replaced:\n%s\n---\n%s", b, before)
	}
}

func TestInstallRefusesWrongTypedHooksKey(t *testing.T) {
	dir := t.TempDir()
	orig := `{"hooks":"nope"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := install.Claude(opts(dir))
	if err == nil || !strings.Contains(err.Error(), `"hooks"`) {
		t.Fatalf("must refuse and name the key, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(b) != orig { //nolint:gosec // test-controlled t.TempDir path
		t.Fatalf("settings.json was touched: %s", b)
	}
}

func TestInstallRefusesWrongTypedEventKey(t *testing.T) {
	dir := t.TempDir()
	orig := `{"hooks":{"PreToolUse":{"matcher":"Bash"}}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := install.Claude(opts(dir))
	if err == nil || !strings.Contains(err.Error(), `"hooks.PreToolUse"`) {
		t.Fatalf("must refuse and name the key, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(b) != orig { //nolint:gosec // test-controlled t.TempDir path
		t.Fatalf("settings.json was touched: %s", b)
	}
}
