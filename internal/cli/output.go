package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

// renderMax is how many bytes of one registry field human output shows.
const renderMax = 120

// render prepares an untrusted field for a terminal. Registry rows are
// written by other sessions, and a claimed process's command name comes
// straight from the OS, so a worktree path or label can carry terminal
// escapes or run to kilobytes. Sanitize drops the control characters;
// truncation on a rune boundary keeps one long value from pushing the rest
// of a row off the screen. --json output is deliberately not rendered:
// machine consumers get the stored value.
func render(s string) string {
	s = ident.Sanitize(s)
	if len(s) <= renderMax {
		return s
	}
	cut := renderMax
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// writeJSON encodes v as indented JSON. A nil slice encodes as an empty
// array ([]) rather than JSON null, since machine consumers of --json
// output expect an array even when the result set is empty.
func writeJSON(w io.Writer, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		v = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func age(now, t time.Time) string {
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

func shortSession(s string) string {
	if len(s) > 12 {
		return s[:12] + "…"
	}
	return s
}

func shortPath(p string) string {
	if p == "" {
		return "-"
	}
	return filepath.Base(filepath.Dir(p)) + "/" + filepath.Base(p)
}

// ownerLine renders the actionable owner description used in errors.
func ownerLine(e registry.Entry, now time.Time) string {
	who := render(e.Agent)
	if e.Session != "" {
		who += " session " + render(shortSession(e.Session))
	}
	where := shortPath(render(e.Worktree))
	label := ""
	if e.Label != "" {
		label = fmt.Sprintf("%q, ", render(e.Label))
	}
	return fmt.Sprintf("%s in %s (%s%s)", who, where, label, age(now, e.StartedAt))
}

func writeTable(w io.Writer, entries []registry.Entry, now time.Time) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "no registered servers")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PORT\tPID\tAGENT\tSESSION\tWORKTREE\tBRANCH\tLABEL\tAGE")
	for _, e := range entries {
		_, _ = fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.Port, e.PID, render(e.Agent), render(shortSession(e.Session)), render(e.Worktree), orDash(render(e.Branch)), orDash(render(e.Label)), age(now, e.StartedAt))
	}
	return tw.Flush()
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func findByPort(f *registry.File, port int) (registry.Entry, bool) {
	for _, e := range f.Entries {
		if e.Port == port {
			return e, true
		}
	}
	return registry.Entry{}, false
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, exitf(ExitUsage, "invalid port %q", s)
	}
	return p, nil
}
