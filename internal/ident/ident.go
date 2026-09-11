// Package ident identifies the calling agent session and applies data-safety rules.
package ident

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moeritze/harbormaster/internal/registry"
)

// Identity is who is calling harbormaster.
type Identity struct {
	Agent    string
	Session  string
	HostUser string
}

// Detect reads well-known environment variables.
func Detect(getenv func(string) string, hostUser string) Identity {
	id := Identity{Agent: "human", HostUser: hostUser}
	if a := getenv("HARBORMASTER_AGENT"); a != "" {
		id.Agent = Sanitize(a)
		id.Session = Sanitize(getenv("HARBORMASTER_SESSION"))
		return id
	}
	if s := getenv("CLAUDE_SESSION_ID"); s != "" {
		id.Agent = "claude"
		id.Session = Sanitize(s)
	}
	return id
}

// Owns implements spec §5 ownership: same session, or same worktree when the caller has none.
func Owns(me Identity, e registry.Entry, myWorktree string) bool {
	if me.Session != "" {
		return e.Session == me.Session
	}
	return myWorktree != "" && e.Worktree == myWorktree
}

const maxLen = 256

// Sanitize drops control and non-printable runes and caps at 256 bytes on a rune boundary.
func Sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > maxLen {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

var (
	sensitiveKey = regexp.MustCompile(`(?i)(key|secret|token|password|passwd|pwd)`)
	assignment   = regexp.MustCompile(`^(--?[A-Za-z0-9_-]+|[A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
)

// RedactCmd joins args for storage, masking sensitive values.
func RedactCmd(args []string) string {
	out := make([]string, 0, len(args))
	maskNext := false
	for _, a := range args {
		if maskNext {
			maskNext = false
			if strings.HasPrefix(a, "-") {
				// The sensitive flag took no value (boolean switch); don't
				// swallow the next flag as its argument.
				out = append(out, a)
				continue
			}
			out = append(out, "***")
			continue
		}
		if m := assignment.FindStringSubmatch(a); m != nil {
			if sensitiveKey.MatchString(m[1]) {
				out = append(out, m[1]+"=***")
				continue
			}
			out = append(out, a)
			continue
		}
		if strings.HasPrefix(a, "-") && sensitiveKey.MatchString(a) {
			maskNext = true
		}
		out = append(out, a)
	}
	return Sanitize(strings.Join(out, " "))
}
