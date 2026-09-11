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

// Cursor hooks live in <config>/hooks.json ("version": 1); each entry is
// flat: {"command", "type", "timeout", …}. Verified against
// https://cursor.com/docs/hooks on 2026-09-11.

// ourCursorCommand matches exactly the command strings Cursor() writes, and
// afterShellExecution besides: harbormaster installed that hook once, so the
// matcher has to keep recognising it for an uninstall to clean it up.
var ourCursorCommand = regexp.MustCompile(`^(?:"[^"]*"|\S+)\s+hook cursor (?:sessionStart|beforeShellExecution|afterShellExecution|sessionEnd)$`)

// cursorHooks are the events harbormaster installs. afterShellExecution is
// deliberately absent: Cursor documents no output fields for it, so the
// post-start nudge it would carry has nowhere to go, and the hook would run
// a process after every single shell command for nothing. The adapter still
// parses it, so an install that predates this still behaves.
var cursorHooks = []hookSpec{
	{"sessionStart", "", 5},
	{"beforeShellExecution", "", 5},
	{"sessionEnd", "", 3},
}

// CursorOptions configure a Cursor install or uninstall.
type CursorOptions struct {
	ConfigDir string // ~/.cursor or <project>/.cursor
	Command   string
	Rule      []byte // .cursor/rules/harbormaster.mdc content; written only when non-nil (project installs)
	DryRun    bool
	Now       func() time.Time
}

func (o CursorOptions) command(event string) string {
	c := o.Command
	if c == "" {
		c = "harbormaster"
	}
	if strings.ContainsFunc(c, unicode.IsSpace) {
		c = `"` + c + `"`
	}
	return c + " hook cursor " + event
}

func isOursCursor(entry map[string]any) bool {
	cmd, _ := entry["command"].(string)
	return ourCursorCommand.MatchString(strings.TrimSpace(cmd))
}

func cursorReport(o CursorOptions) Report {
	r := Report{Settings: filepath.Join(o.ConfigDir, "hooks.json")}
	if o.Rule != nil {
		r.SkillPath = filepath.Join(o.ConfigDir, "rules", "harbormaster.mdc")
	}
	return r
}

// Cursor installs hooks into <ConfigDir>/hooks.json and, for project
// installs, the harbormaster rule file. Idempotent.
func Cursor(o CursorOptions) (Report, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	r := cursorReport(o)
	if err := checkCommand(o.Command); err != nil {
		return r, err
	}
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
	for _, spec := range cursorHooks {
		list, err := eventList(r.Settings, spec.Event, hooks)
		if err != nil {
			return r, err
		}
		present := false
		for _, e := range list {
			if m, ok := e.(map[string]any); ok && isOursCursor(m) {
				present = true
				break
			}
		}
		if present {
			r.Skipped = append(r.Skipped, spec.Event)
			continue
		}
		hooks[spec.Event] = append(list, map[string]any{"command": o.command(spec.Event), "type": "command", "timeout": spec.Timeout})
		r.Added = append(r.Added, spec.Event)
		changed = true
	}
	if _, ok := s.doc["version"]; !ok {
		s.doc["version"] = 1
		changed = true
	}
	r.Actions = append(r.Actions, fmt.Sprintf("hooks in %s: add %v, keep %v", r.Settings, r.Added, r.Skipped))
	if changed && s.raw != nil {
		r.Backup = backupPath(r.Settings, o.Now)
		r.Actions = append(r.Actions, "back up "+r.Settings+" to "+r.Backup)
	}
	ruleChanged := false
	if o.Rule != nil {
		ruleChanged = true
		if cur, err := os.ReadFile(r.SkillPath); err == nil && bytes.Equal(cur, o.Rule) { //nolint:gosec // SkillPath is derived from the caller's configured ConfigDir
			ruleChanged = false
		}
		if ruleChanged {
			r.Actions = append(r.Actions, "write "+r.SkillPath)
		}
	}
	if o.DryRun {
		return r, nil
	}
	if changed {
		if r.Backup != "" {
			p, err := writeBackup(r.Backup, s.raw)
			if err != nil {
				return r, err
			}
			r.Backup = p
		}
		if err := os.MkdirAll(o.ConfigDir, 0o750); err != nil {
			return r, err
		}
		if err := s.write(r.Settings); err != nil {
			return r, err
		}
	}
	if ruleChanged {
		if err := os.MkdirAll(filepath.Dir(r.SkillPath), 0o750); err != nil {
			return r, err
		}
		if err := os.WriteFile(r.SkillPath, o.Rule, 0o600); err != nil {
			return r, err
		}
	}
	return r, nil
}

// CursorUninstall removes exactly what Cursor added.
func CursorUninstall(o CursorOptions) (Report, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	// cursorReport sets SkillPath only when Rule != nil, i.e. for a project
	// install. A user-level install never wrote ~/.cursor/rules — those are
	// the user's own rules — so a user-level uninstall must not touch it.
	r := cursorReport(o)
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
			continue
		}
		var kept []any
		for _, e := range list {
			if m, ok := e.(map[string]any); ok && isOursCursor(m) {
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
	if r.SkillPath != "" {
		if _, err := os.Stat(r.SkillPath); err == nil {
			r.Actions = append(r.Actions, "remove "+r.SkillPath)
		}
	}
	if o.DryRun {
		return r, nil
	}
	if changed {
		if r.Backup != "" {
			p, err := writeBackup(r.Backup, s.raw)
			if err != nil {
				return r, err
			}
			r.Backup = p
		}
		if err := s.write(r.Settings); err != nil {
			return r, err
		}
	}
	if r.SkillPath != "" {
		if err := os.Remove(r.SkillPath); err != nil && !os.IsNotExist(err) {
			return r, err
		}
		_ = os.Remove(filepath.Dir(r.SkillPath)) // only succeeds when empty
	}
	return r, nil
}
