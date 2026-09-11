package cli

import (
	"testing"
	"time"
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
