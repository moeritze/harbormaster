// Package install writes and removes harbormaster's agent integration:
// hook entries in settings.json and the skill file. It touches nothing else.
package install

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Marker prefixes every hook command harbormaster installs.
const Marker = "harbormaster hook claude "

// ourCommand matches exactly the command strings Claude() writes: an optional
// absolute-path prefix, the harbormaster binary (by its full name or the "hm"
// alias), then "hook claude <event>". Anything else — even a command that
// merely contains Marker as a substring, such as a user's own script that
// echoes it — is not ours.
var ourCommand = regexp.MustCompile(`^(?:\S*/)?(?:harbormaster|hm) hook claude (?:SessionStart|PreToolUse|PostToolUse|SessionEnd)$`)

// Options configure an install or uninstall.
type Options struct {
	ConfigDir string
	Command   string
	Skill     []byte
	DryRun    bool
	Now       func() time.Time
}

// Report says what happened (or, with DryRun, what would happen).
type Report struct {
	Settings  string
	Backup    string
	SkillPath string
	Added     []string
	Skipped   []string
	Removed   []string
	Actions   []string
}

type hookSpec struct {
	Event   string
	Matcher string
	Timeout int
}

var claudeHooks = []hookSpec{
	{"SessionStart", "startup|resume|clear|compact", 5},
	{"PreToolUse", "Bash", 5},
	{"PostToolUse", "Bash", 5},
	{"SessionEnd", "", 1},
}

func (o Options) command(event string) string {
	c := o.Command
	if c == "" {
		c = "harbormaster"
	}
	return c + " hook claude " + event
}

func isOurs(entry map[string]any) bool {
	hs, _ := entry["hooks"].([]any)
	for _, h := range hs {
		m, _ := h.(map[string]any)
		cmd, _ := m["command"].(string)
		if ourCommand.MatchString(strings.TrimSpace(cmd)) {
			return true
		}
	}
	return false
}

// Claude installs hooks and the skill for Claude Code. Idempotent.
func Claude(o Options) (Report, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	r := Report{Settings: filepath.Join(o.ConfigDir, "settings.json"), SkillPath: filepath.Join(o.ConfigDir, "skills", "harbormaster", "SKILL.md")}
	s, err := readSettings(r.Settings)
	if err != nil {
		return r, err
	}
	hooks := s.hooks()
	changed := false
	for _, spec := range claudeHooks {
		list, _ := hooks[spec.Event].([]any)
		present := false
		for _, e := range list {
			if m, ok := e.(map[string]any); ok && isOurs(m) {
				present = true
				break
			}
		}
		if present {
			r.Skipped = append(r.Skipped, spec.Event)
			continue
		}
		entry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": o.command(spec.Event), "timeout": spec.Timeout}}}
		if spec.Matcher != "" {
			entry["matcher"] = spec.Matcher
		}
		hooks[spec.Event] = append(list, entry)
		r.Added = append(r.Added, spec.Event)
		changed = true
	}
	r.Actions = append(r.Actions, fmt.Sprintf("hooks in %s: add %v, keep %v", r.Settings, r.Added, r.Skipped))
	skillChanged := true
	if cur, err := os.ReadFile(r.SkillPath); err == nil && bytes.Equal(cur, o.Skill) { //nolint:gosec // SkillPath is derived from the caller's configured ConfigDir
		skillChanged = false
	}
	if skillChanged {
		r.Actions = append(r.Actions, "write "+r.SkillPath)
	}
	if o.DryRun {
		return r, nil
	}
	if changed {
		if s.raw != nil {
			r.Backup = fmt.Sprintf("%s.harbormaster-backup-%d", r.Settings, o.Now().Unix())
			if err := os.WriteFile(r.Backup, s.raw, 0o600); err != nil { //nolint:gosec // Backup is built from Settings, itself derived from the caller's configured ConfigDir
				return r, err
			}
		}
		if err := os.MkdirAll(o.ConfigDir, 0o750); err != nil {
			return r, err
		}
		if err := s.write(r.Settings); err != nil {
			return r, err
		}
	}
	if skillChanged {
		if err := os.MkdirAll(filepath.Dir(r.SkillPath), 0o750); err != nil {
			return r, err
		}
		if err := os.WriteFile(r.SkillPath, o.Skill, 0o600); err != nil {
			return r, err
		}
	}
	return r, nil
}

// ClaudeUninstall removes exactly what Claude added.
func ClaudeUninstall(o Options) (Report, error) {
	r := Report{Settings: filepath.Join(o.ConfigDir, "settings.json"), SkillPath: filepath.Join(o.ConfigDir, "skills", "harbormaster", "SKILL.md")}
	s, err := readSettings(r.Settings)
	if err != nil {
		return r, err
	}
	hooks, _ := s.doc["hooks"].(map[string]any)
	changed := false
	for ev, v := range hooks {
		list, _ := v.([]any)
		var kept []any
		for _, e := range list {
			if m, ok := e.(map[string]any); ok && isOurs(m) {
				r.Removed = append(r.Removed, ev)
				changed = true
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = kept
		}
	}
	sort.Strings(r.Removed)
	if hooks != nil && len(hooks) == 0 {
		delete(s.doc, "hooks")
	}
	r.Actions = append(r.Actions, fmt.Sprintf("hooks in %s: remove %v", r.Settings, r.Removed))
	if _, err := os.Stat(r.SkillPath); err == nil {
		r.Actions = append(r.Actions, "remove "+r.SkillPath)
	}
	if o.DryRun {
		return r, nil
	}
	if changed {
		if err := s.write(r.Settings); err != nil {
			return r, err
		}
	}
	if err := os.Remove(r.SkillPath); err != nil && !os.IsNotExist(err) {
		return r, err
	}
	_ = os.Remove(filepath.Dir(r.SkillPath)) // only succeeds when empty
	return r, nil
}
