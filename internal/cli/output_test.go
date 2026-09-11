package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/moeritze/harbormaster/internal/registry"
)

func TestAgeBuckets(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"under a minute", 10 * time.Second, "<1m"},
		{"minutes", 12 * time.Minute, "12m"},
		{"hours", 2*time.Hour + 5*time.Minute, "2h05m"},
		{"days", 3 * 24 * time.Hour, "3d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := age(now, now.Add(-c.ago)); got != c.want {
				t.Fatalf("age(%v) = %q, want %q", c.ago, got, c.want)
			}
		})
	}
}

func TestShortSession(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short passthrough", "abc123", "abc123"},
		{"exactly twelve", "123456789012", "123456789012"},
		{"truncated", "0132abcdefghijklmnop", "0132abcdefg" + "h" + "…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shortSession(c.in); got != c.want {
				t.Fatalf("shortSession(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestShortPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "-"},
		{"nested", "/wt/auth", "wt/auth"},
		{"deep", "/repos/harbormaster-wt/task9", "harbormaster-wt/task9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shortPath(c.in); got != c.want {
				t.Fatalf("shortPath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestWriteJSONEncodesNilSliceAsEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, []registry.Entry(nil)); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Fatalf("got %q, want %q", got, "[]")
	}
}

func TestOrDash(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "-"},
		{"whitespace", "   ", "-"},
		{"value", "main", "main"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := orDash(c.in); got != c.want {
				t.Fatalf("orDash(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestRenderSanitizesAndTruncates covers H4 at the unit level.
func TestRenderSanitizesAndTruncates(t *testing.T) {
	if got := render("\x1b[31mred\x1b[0m"); got != "[31mred[0m" {
		t.Fatalf("escapes survived: %q", got)
	}
	if got := render("plain"); got != "plain" {
		t.Fatalf("render(%q) = %q", "plain", got)
	}
	long := render(strings.Repeat("a", 300))
	if !strings.HasSuffix(long, "…") {
		t.Fatalf("truncation not marked: %q", long)
	}
	if n := len(strings.TrimSuffix(long, "…")); n != renderMax {
		t.Fatalf("kept %d bytes, want %d", n, renderMax)
	}
	// Truncation happens on a rune boundary: never a half-encoded rune.
	multi := render(strings.Repeat("ä", 200))
	if !utf8.ValidString(multi) {
		t.Fatalf("invalid utf-8 after truncation: %q", multi)
	}
}

// TestWriteTableRendersHostileFields is H4 end to end: an entry whose
// worktree carries a terminal escape and whose label is 300 bytes long must
// print neither the escape nor the full label.
func TestWriteTableRendersHostileFields(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	e := registry.Entry{
		Port: 3000, PID: 7, Agent: "cl\x1b[31maude", Session: "s\x1b[0m2",
		Worktree:  "/wt/\x1b[31mevil",
		Branch:    "feat/\x1b[2Jclear",
		Label:     strings.Repeat("L", 300),
		StartedAt: now.Add(-time.Minute),
	}
	var buf bytes.Buffer
	if err := writeTable(&buf, []registry.Entry{e}, now); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("escape sequence rendered: %q", out)
	}
	if strings.Contains(out, strings.Repeat("L", 300)) {
		t.Fatalf("label not truncated: %q", out)
	}
	if !strings.Contains(out, strings.Repeat("L", renderMax)+"…") {
		t.Fatalf("label not rendered as expected: %q", out)
	}
	if !strings.Contains(out, "/wt/[31mevil") {
		t.Fatalf("worktree text lost: %q", out)
	}
	if !strings.Contains(out, "cl[31maude") {
		t.Fatalf("agent text lost: %q", out)
	}

	// ownerLine goes through the same filter.
	line := ownerLine(e, now)
	if strings.ContainsRune(line, 0x1b) || strings.Contains(line, strings.Repeat("L", 300)) {
		t.Fatalf("ownerLine not rendered: %q", line)
	}
}
