package ident_test

import (
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/registry"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want ident.Identity
	}{
		{"human", nil, ident.Identity{Agent: "human", HostUser: "m"}},
		{"claude", map[string]string{"CLAUDE_SESSION_ID": "s1"}, ident.Identity{Agent: "claude", Session: "s1", HostUser: "m"}},
		{"override", map[string]string{"CLAUDE_SESSION_ID": "s1", "HARBORMASTER_AGENT": "cursor", "HARBORMASTER_SESSION": "c9"}, ident.Identity{Agent: "cursor", Session: "c9", HostUser: "m"}},
	}
	for _, c := range cases {
		if got := ident.Detect(env(c.env), "m"); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestOwns(t *testing.T) {
	me := ident.Identity{Agent: "claude", Session: "s1"}
	if !ident.Owns(me, registry.Entry{Session: "s1"}, "/wt/a") {
		t.Fatal("same session should own")
	}
	if ident.Owns(me, registry.Entry{Session: "s2", Worktree: "/wt/a"}, "/wt/a") {
		t.Fatal("different session should not own even in same worktree")
	}
	human := ident.Identity{Agent: "human"}
	if !ident.Owns(human, registry.Entry{Session: "s2", Worktree: "/wt/a"}, "/wt/a") {
		t.Fatal("no session: same worktree should own")
	}
	if ident.Owns(human, registry.Entry{Session: "s2", Worktree: "/wt/b"}, "/wt/a") {
		t.Fatal("no session: other worktree should not own")
	}
}

func TestSanitize(t *testing.T) {
	if got := ident.Sanitize("ok label"); got != "ok label" {
		t.Fatal(got)
	}
	if got := ident.Sanitize("bad\x1b[31mred\x00\n"); got != "bad[31mred" {
		t.Fatalf("%q", got)
	}
	long := ident.Sanitize(strings.Repeat("ä", 300))
	if len(long) > 256 {
		t.Fatalf("len %d", len(long))
	}
	if !strings.HasSuffix(long, "ä") {
		t.Fatal("must not cut a rune in half")
	}
}

func TestRedactCmd(t *testing.T) {
	got := ident.RedactCmd([]string{"npm", "run", "dev", "--", "--api-key=abc123", "STRIPE_SECRET=sk_live_1", "--token", "tkn", "--port", "3000"})
	if strings.Contains(got, "abc123") || strings.Contains(got, "sk_live_1") || strings.Contains(got, "tkn") {
		t.Fatalf("leaked: %s", got)
	}
	if !strings.Contains(got, "--port 3000") {
		t.Fatalf("over-redacted: %s", got)
	}
}
