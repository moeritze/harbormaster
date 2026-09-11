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
func Sanitize(s string) string { return sanitize(s, maxLen, "") }

// SanitizeN is Sanitize with the cap chosen by the caller and the cut marked
// with "…". It exists for values that are already the product of their own
// length discipline -- RedactCmd's output, which is redacted and capped at
// maxCmd -- where re-running the 256-byte identifier cap would throw away
// most of a command line that was deliberately kept.
func SanitizeN(s string, limit int) string { return sanitize(s, limit, "…") }

func sanitize(s string, limit int, marker string) string {
	var b strings.Builder
	cut := false
	for _, r := range s {
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > limit-len(marker) {
			cut = true
			break
		}
		b.WriteRune(r)
	}
	if cut {
		b.WriteString(marker)
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
	// shortSecretAttached is the same flags with the value glued on, which
	// is how mysql is normally invoked: -pSuperSecret, -ualice:hunter2.
	// Everything after the flag letter is the value.
	shortSecretAttached = regexp.MustCompile(`^(-[pu]).+$`)
	// tokenSep splits one argument into the tokens each rule is applied to:
	// a connection string ("host=db password=x"), a query string
	// ("?a=1&token=x"), a shell-ish list ("a=1;b=2") all hide assignments
	// inside a single argv entry.
	tokenSep = regexp.MustCompile(`[\s&?;,]+`)
	// jsonPair matches a "key": "value" pair in a JSON-ish request body, in
	// either quote style, so `curl -d '{"api_key":"sk-live"}'` is masked.
	jsonPair = regexp.MustCompile(`(["'])([A-Za-z0-9_.\-]+)(["'])(\s*:\s*)(["'])([^"']*)(["'])`)
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
	if m := shortSecretAttached.FindStringSubmatch(a); m != nil {
		return m[1] + "***"
	}
	a = urlCreds.ReplaceAllString(a, "${1}***@")
	a = bearerToken.ReplaceAllString(a, "Bearer ***")
	a = redactJSONPairs(a)
	if m := assignment.FindStringSubmatch(a); m != nil {
		// The whole argument is one assignment. When its key is sensitive
		// the ENTIRE value is the secret -- `--password=two words` is one
		// password -- so it is masked as a unit and never split further.
		if sensitiveKey.MatchString(m[1]) {
			return m[1] + "=***"
		}
		// A harmless key may still carry secrets inside its value
		// ("host=db user=alice password=swordfish"), so the value goes
		// through the per-token rules. redactHeaderFlag is deliberately not
		// applied: an argument that is itself an assignment never makes the
		// NEXT argument a secret (that is what kept `--auth=abc def` from
		// swallowing `def` before).
		return redactTokens(a)
	}
	return redactHeaderFlag(redactTokens(a), maskNext)
}

// redactTokens applies the assignment rules to every whitespace-, "&"-, "?"-,
// ";"- or ","-delimited token of an argument, preserving the delimiters.
// Secrets hide inside arguments as often as they are arguments: a psql
// connection string, a URL query, a `-d` body are each one argv entry.
func redactTokens(a string) string {
	seps := tokenSep.FindAllStringIndex(a, -1)
	if len(seps) == 0 {
		return redactToken(a)
	}
	var b strings.Builder
	prev := 0
	for _, s := range seps {
		b.WriteString(redactToken(a[prev:s[0]]))
		b.WriteString(a[s[0]:s[1]])
		prev = s[1]
	}
	b.WriteString(redactToken(a[prev:]))
	return b.String()
}

// redactToken masks one delimited token whose key names a secret.
func redactToken(t string) string {
	if m := assignment.FindStringSubmatch(t); m != nil && sensitiveKey.MatchString(m[1]) {
		return m[1] + "=***"
	}
	return t
}

// redactJSONPairs masks the value of every JSON-ish pair whose key names a
// secret, in either quote style. Only the value is replaced, so the shape of
// the body a reader needs to recognize the command survives.
func redactJSONPairs(a string) string {
	return jsonPair.ReplaceAllStringFunc(a, func(m string) string {
		g := jsonPair.FindStringSubmatch(m)
		if !sensitiveKey.MatchString(g[2]) {
			return m
		}
		return g[1] + g[2] + g[3] + g[4] + g[5] + "***" + g[7]
	})
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
