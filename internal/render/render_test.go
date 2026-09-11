package render_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
	"github.com/moeritze/harbormaster/internal/render"
)

func TestTextSanitizesAndTruncates(t *testing.T) {
	if got := render.Text("a\x1b[31mb"); got != "a[31mb" {
		t.Fatalf("%q", got)
	}
	long := render.Text(strings.Repeat("ä", 200))
	if len(long) > 123 || !strings.HasSuffix(long, "…") {
		t.Fatalf("len %d %q", len(long), long[len(long)-3:])
	}
}

func TestShortSessionRuneSafe(t *testing.T) {
	if got := render.ShortSession("ääääääääääää-rest"); !strings.HasSuffix(got, "…") || strings.Contains(got, "�") {
		t.Fatalf("%q", got)
	}
	if got := render.ShortSession("short"); got != "short" {
		t.Fatalf("%q", got)
	}
}

func TestTableAndOwnerLine(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	e := registry.Entry{Port: 3000, PID: 1, Agent: "claude", Session: "sess-1234567890", Worktree: "/wt/a", Branch: "b", Label: "x", StartedAt: now.Add(-3 * time.Minute)}
	var buf bytes.Buffer
	if err := render.Table(&buf, []registry.Entry{e}, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PORT", "3000", "claude", "sess-1234567…", "/wt/a", "3m"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("missing %q in %s", want, buf.String())
		}
	}
	if ol := render.OwnerLine(e, now); !strings.Contains(ol, `claude session sess-1234567… in wt/a ("x", 3m)`) {
		t.Fatalf("%q", ol)
	}
}

// TestTableBlankFieldsRenderAsDash: a claimed entry has no worktree, and an
// empty cell in a tab-aligned table silently merges with its neighbour.
func TestTableBlankFieldsRenderAsDash(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	e := registry.Entry{Port: 3000, PID: 1, Agent: "claude", Session: "s", StartedAt: now}
	var buf bytes.Buffer
	if err := render.Table(&buf, []registry.Entry{e}, now); err != nil {
		t.Fatal(err)
	}
	row := strings.Split(strings.TrimSpace(buf.String()), "\n")[1]
	if strings.Count(row, "-") != 3 {
		t.Fatalf("worktree, branch and label must all render as \"-\": %q", row)
	}
}

func TestAgeBuckets(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
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
			if got := render.Age(now, now.Add(-c.ago)); got != c.want {
				t.Fatalf("Age(%v) = %q, want %q", c.ago, got, c.want)
			}
		})
	}
}

func TestShortPathEmpty(t *testing.T) {
	if got := render.ShortPath(""); got != "-" {
		t.Fatalf("ShortPath(\"\") = %q, want %q", got, "-")
	}
}

func TestOrDash(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "-"},
		{"   ", "-"},
		{"main", "main"},
	}
	for _, c := range cases {
		if got := render.OrDash(c.in); got != c.want {
			t.Fatalf("OrDash(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
