package cli

import (
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
	renderpkg "github.com/moeritze/harbormaster/internal/render"
)

// renderMax mirrors package render's truncation length; kept here only so
// existing tests that assert on it keep compiling unchanged.
const renderMax = 120

// render, shortSession, shortPath, ownerLine, age, and writeTable are thin
// wrappers over package render, which owns entry rendering (moved there so
// hooks can share it without importing cli).
func render(s string) string { return renderpkg.Text(s) }

func shortSession(s string) string { return renderpkg.ShortSession(s) }

func shortPath(p string) string { return renderpkg.ShortPath(p) }

func ownerLine(e registry.Entry, now time.Time) string { return renderpkg.OwnerLine(e, now) }

func age(now, t time.Time) string { return renderpkg.Age(now, t) }

func writeTable(w io.Writer, entries []registry.Entry, now time.Time) error {
	return renderpkg.Table(w, entries, now)
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
