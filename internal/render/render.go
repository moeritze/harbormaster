// Package render formats registry entries for humans and for agent context;
// every field passes through ident.Sanitize at render time.
package render

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

// textMax is how many bytes of one registry field human output shows.
const textMax = 120

// Text prepares an untrusted field for a terminal. Registry rows are written
// by other sessions, and a claimed process's command name comes straight
// from the OS, so a worktree path or label can carry terminal escapes or run
// to kilobytes. Sanitize drops the control characters; truncation on a rune
// boundary keeps one long value from pushing the rest of a row off the
// screen. --json output is deliberately not rendered: machine consumers get
// the stored value.
func Text(s string) string {
	s = ident.Sanitize(s)
	if len(s) <= textMax {
		return s
	}
	cut := textMax
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// Age buckets a duration into a short human string: "<1m", "12m", "2h05m", "3d".
func Age(now, t time.Time) string {
	d := now.Sub(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ShortSession truncates a session id to 12 runes, marking truncation with
// "…". Rune-safe: a multi-byte id is never cut mid-rune.
func ShortSession(s string) string {
	if utf8.RuneCountInString(s) <= 12 {
		return s
	}
	runes := []rune(s)
	return string(runes[:12]) + "…"
}

// ShortPath renders a path as "<parent-dir>/<base>", or "-" when empty.
func ShortPath(p string) string {
	if p == "" {
		return "-"
	}
	return filepath.Base(filepath.Dir(p)) + "/" + filepath.Base(p)
}

// OwnerLine renders the actionable owner description used in errors.
func OwnerLine(e registry.Entry, now time.Time) string {
	who := Text(e.Agent)
	if e.Session != "" {
		who += " session " + Text(ShortSession(e.Session))
	}
	where := ShortPath(Text(e.Worktree))
	label := ""
	if e.Label != "" {
		label = fmt.Sprintf("%q, ", Text(e.Label))
	}
	return fmt.Sprintf("%s in %s (%s%s)", who, where, label, Age(now, e.StartedAt))
}

// Table writes entries as a tab-aligned table.
func Table(w io.Writer, entries []registry.Entry, now time.Time) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "no registered servers")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PORT\tPID\tAGENT\tSESSION\tWORKTREE\tBRANCH\tLABEL\tAGE")
	for _, e := range entries {
		_, _ = fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.Port, e.PID, Text(e.Agent), Text(ShortSession(e.Session)), OrDash(Text(e.Worktree)), OrDash(Text(e.Branch)), OrDash(Text(e.Label)), Age(now, e.StartedAt))
	}
	return tw.Flush()
}

// OrDash renders a blank or whitespace-only field as "-".
func OrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
