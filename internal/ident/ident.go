// Package ident identifies the calling agent session and applies data-safety rules.
package ident

import (
	"regexp"
	"strconv"
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

// Detect reads well-known environment variables. ppid is the caller's parent
// process id, used only for the rule 5 shell fallback (inject os.Getppid()).
func Detect(getenv func(string) string, hostUser string, ppid int) Identity {
	id := Identity{Agent: "human", HostUser: hostUser}
	if a := getenv("HARBORMASTER_AGENT"); a != "" {
		id.Agent = Sanitize(a)
		id.Session = Sanitize(getenv("HARBORMASTER_SESSION"))
		return id
	}
	if s := getenv("HARBORMASTER_SESSION"); s != "" {
		id.Session = Sanitize(s)
		return id
	}
	if s := getenv("CLAUDE_CODE_SESSION_ID"); s != "" {
		id.Agent = "claude"
		id.Session = Sanitize(s)
		return id
	}
	if s := getenv("CLAUDE_SESSION_ID"); s != "" {
		id.Agent = "claude"
		id.Session = Sanitize(s)
		return id
	}
	// No session id was handed to us: fall back to something stable for this
	// shell so a human at a terminal (or an unrecognized agent) doesn't need
	// --force to manage their own servers. This never yields an empty session.
	fallback := "shell:" + strconv.Itoa(ppid)
	switch {
	case getenv("TMUX_PANE") != "":
		fallback = "tmux:" + getenv("TMUX_PANE")
	case getenv("ITERM_SESSION_ID") != "":
		fallback = "iterm:" + getenv("ITERM_SESSION_ID")
	case getenv("TERM_SESSION_ID") != "":
		fallback = "term:" + getenv("TERM_SESSION_ID")
	}
	id.Session = Sanitize(fallback)
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
	// sensitiveKey matches flag and variable names whose values are secrets.
	// Substring matching is deliberate (spec §10: *KEY*, *SECRET*, ...): it
	// fails safe by over-redacting names like --keyboard.
	sensitiveKey = regexp.MustCompile(`(?i)(key|secret|token|password|passwd|pwd|auth|credential|bearer|dsn)`)
	assignment   = regexp.MustCompile(`^(--?[A-Za-z0-9_-]+|[A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	// shortSecretFlag covers single-letter flags that conventionally take a
	// password (mysql/psql/ssh -p, curl -u).
	shortSecretFlag = regexp.MustCompile(`^-[pu]$`)
	// urlCreds finds scheme://user[:pass]@ anywhere in an argument.
	urlCreds = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]+@`)
	// authHeader matches "Name: value" / "Name:value" for headers that carry
	// credentials, as one argument (curl -H "Authorization: Bearer x").
	authHeader = regexp.MustCompile(`(?i)^(authorization|proxy-authorization|cookie|set-cookie|x-api-key|x-auth-token|api-key):\s*.*$`)
	// bearerToken masks "Bearer <token>" wherever it appears.
	bearerToken = regexp.MustCompile(`(?i)\bbearer\s+\S+`)
)

// maxArg caps one stored argument; maxCmd caps the stored command line.
const (
	maxArg = 256
	maxCmd = 2048
)

// RedactCmd joins args for storage, masking sensitive values. Redaction runs
// per argument before any truncation, so a secret late on a long command
// line is masked rather than hidden behind an earlier cut.
func RedactCmd(args []string) string {
	out := make([]string, 0, len(args))
	maskNext := false
	total := 0
	for _, a := range args {
		a = redactArg(a, &maskNext)
		a = Sanitize(truncate(a, maxArg))
		if total+len(a)+1 > maxCmd {
			out = append(out, "…")
			break
		}
		total += len(a) + 1
		out = append(out, a)
	}
	return strings.Join(out, " ")
}

// redactArg masks the secret-bearing parts of one argument. maskNext carries
// the "previous flag takes a value" state between arguments.
func redactArg(a string, maskNext *bool) string {
	if *maskNext {
		*maskNext = false
		if strings.HasPrefix(a, "-") {
			// The sensitive flag took no value (boolean switch); don't
			// swallow the next flag as its argument.
			return redactHeaderFlag(a, maskNext)
		}
		if m := authHeader.FindStringSubmatch(a); m != nil {
			return m[1] + ": ***"
		}
		return "***"
	}
	if m := authHeader.FindStringSubmatch(a); m != nil {
		return m[1] + ": ***"
	}
	a = urlCreds.ReplaceAllString(a, "${1}***@")
	a = bearerToken.ReplaceAllString(a, "Bearer ***")
	if m := assignment.FindStringSubmatch(a); m != nil {
		if sensitiveKey.MatchString(m[1]) {
			return m[1] + "=***"
		}
		return a
	}
	return redactHeaderFlag(a, maskNext)
}

// redactHeaderFlag decides whether a flag argument makes the NEXT argument a
// secret: sensitive flag names (--token, --password), short password flags
// (-p, -u), and header flags (-H/--header) whose header may carry credentials.
func redactHeaderFlag(a string, maskNext *bool) string {
	if !strings.HasPrefix(a, "-") {
		return a
	}
	// -H/--header needs no special state: the header argument itself is
	// matched by authHeader in redactArg, and plain headers pass through.
	if shortSecretFlag.MatchString(a) || sensitiveKey.MatchString(a) {
		*maskNext = true
	}
	return a
}

// truncate cuts s to at most n bytes on a rune boundary, marking the cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
