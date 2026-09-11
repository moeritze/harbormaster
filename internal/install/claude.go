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
	"unicode"
)

// Marker prefixes every hook command harbormaster installs.
const Marker = "harbormaster hook claude "

// ourCommand matches exactly the command strings Claude() writes: one path
// to a binary — a bare word, an absolute or relative path, under any name,
// optionally double-quoted because it contains a space — followed by "hook
// claude <event>". Anything else, in particular a command that merely
// contains Marker as a substring (a user's own script echoing it), is not
// ours. Matching by shape rather than by binary name is what makes a second
// install a no-op no matter what the binary on this machine is called.
var ourCommand = regexp.MustCompile(`^(?:"[^"]*"|\S+)\s+hook claude (?:SessionStart|PreToolUse|PostToolUse|SessionEnd)$`)

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
	{"SessionStart", "startup|resume|clear|compact|fork", 5},
	{"PreToolUse", "Bash", 5},
	{"PostToolUse", "Bash", 5},
	// SessionEnd releases and stops this session's servers, which means
	// waiting on real processes: 1 s was not enough for a session holding
	// more than one.
	{"SessionEnd", "", 3},
}

// command renders the hook command line. A binary path containing
// whitespace is double-quoted, which is both what a shell needs and what
// ourCommand recognises on the next run.
func (o Options) command(event string) string {
	c := o.Command
	if c == "" {
		c = "harbormaster"
	}
	if strings.ContainsFunc(c, unicode.IsSpace) {
		c = `"` + c + `"`
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

// backupPath is where the current settings.json is copied before a rewrite.
func backupPath(settings string, now func() time.Time) string {
	return fmt.Sprintf("%s.harbormaster-backup-%d", settings, now().Unix())
}

func writeBackup(path string, raw []byte) error {
	return os.WriteFile(path, raw, 0o600) //nolint:gosec // path is built from Settings, itself derived from the caller's configured ConfigDir
}

// guardPaths refuses to touch a settings file or skill file that is a
// symlink, before anything is read or written.
func guardPaths(r Report) error {
	for _, p := range []string{r.Settings, r.SkillPath} {
		if err := refuseSymlink(p); err != nil {
			return err
		}
	}
	return nil
}

// Claude installs hooks and the skill for Claude Code. Idempotent.
func Claude(o Options) (Report, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	r := Report{Settings: filepath.Join(o.ConfigDir, "settings.json"), SkillPath: filepath.Join(o.ConfigDir, "skills", "harbormaster", "SKILL.md")}
	if err := guardPaths(r); err != nil {
		return r, err
	}
	s, err := readSettings(r.Settings)
	if err != nil {
		return r, err
	}
	hooks, err := s.hooks(r.Settings)
	if err != nil {
		return r, err
	}
	changed := false
	for _, spec := range claudeHooks {
		list, err := eventList(r.Settings, spec.Event, hooks)
		if err != nil {
			return r, err
		}
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
	if changed && s.raw != nil {
		r.Backup = backupPath(r.Settings, o.Now)
		r.Actions = append(r.Actions, "back up "+r.Settings+" to "+r.Backup)
	}
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
		if r.Backup != "" {
			if err := writeBackup(r.Backup, s.raw); err != nil {
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
	if o.Now == nil {
		o.Now = time.Now
	}
	r := Report{Settings: filepath.Join(o.ConfigDir, "settings.json"), SkillPath: filepath.Join(o.ConfigDir, "skills", "harbormaster", "SKILL.md")}
	if err := guardPaths(r); err != nil {
		return r, err
	}
	s, err := readSettings(r.Settings)
	if err != nil {
		return r, err
	}
	hooks, err := s.hooks(r.Settings)
	if err != nil {
		return r, err
	}
	changed := false
	for ev, v := range hooks {
		list, ok := v.([]any)
		if !ok {
			// Not a shape we ever wrote, so nothing of ours is in it.
			continue
		}
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
	if len(hooks) == 0 {
		delete(s.doc, "hooks")
	}
	r.Actions = append(r.Actions, fmt.Sprintf("hooks in %s: remove %v", r.Settings, r.Removed))
	if changed && s.raw != nil {
		r.Backup = backupPath(r.Settings, o.Now)
		r.Actions = append(r.Actions, "back up "+r.Settings+" to "+r.Backup)
	}
	if _, err := os.Stat(r.SkillPath); err == nil {
		r.Actions = append(r.Actions, "remove "+r.SkillPath)
	}
	if o.DryRun {
		return r, nil
	}
	if changed {
		if r.Backup != "" {
			if err := writeBackup(r.Backup, s.raw); err != nil {
				return r, err
			}
		}
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
