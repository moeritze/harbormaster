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
		ppid int
		want ident.Identity
	}{
		{"human shell fallback (ppid)", nil, 4321, ident.Identity{Agent: "human", Session: "shell:4321", HostUser: "m"}},
		{"human tmux fallback", map[string]string{"TMUX_PANE": "%3"}, 4321, ident.Identity{Agent: "human", Session: "tmux:%3", HostUser: "m"}},
		{"human iterm fallback", map[string]string{"ITERM_SESSION_ID": "w0t0p0"}, 4321, ident.Identity{Agent: "human", Session: "iterm:w0t0p0", HostUser: "m"}},
		{"human term fallback", map[string]string{"TERM_SESSION_ID": "abc"}, 4321, ident.Identity{Agent: "human", Session: "term:abc", HostUser: "m"}},
		{"tmux beats term when both set", map[string]string{"TMUX_PANE": "%3", "TERM_SESSION_ID": "abc"}, 4321, ident.Identity{Agent: "human", Session: "tmux:%3", HostUser: "m"}},
		{"claude legacy var", map[string]string{"CLAUDE_SESSION_ID": "s1"}, 4321, ident.Identity{Agent: "claude", Session: "s1", HostUser: "m"}},
		{"claude code session id", map[string]string{"CLAUDE_CODE_SESSION_ID": "cc1"}, 4321, ident.Identity{Agent: "claude", Session: "cc1", HostUser: "m"}},
		{"claude code session id wins over legacy", map[string]string{"CLAUDE_CODE_SESSION_ID": "cc1", "CLAUDE_SESSION_ID": "s1"}, 4321, ident.Identity{Agent: "claude", Session: "cc1", HostUser: "m"}},
		{"harbormaster session without agent stays human", map[string]string{"HARBORMASTER_SESSION": "hs1", "CLAUDE_CODE_SESSION_ID": "cc1"}, 4321, ident.Identity{Agent: "human", Session: "hs1", HostUser: "m"}},
		{"override wins over claude code id and legacy", map[string]string{"CLAUDE_CODE_SESSION_ID": "cc1", "CLAUDE_SESSION_ID": "s1", "HARBORMASTER_AGENT": "cursor", "HARBORMASTER_SESSION": "c9"}, 4321, ident.Identity{Agent: "cursor", Session: "c9", HostUser: "m"}},
	}
	for _, c := range cases {
		if got := ident.Detect(env(c.env), "m", c.ppid); got != c.want {
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

func TestRedactCmdBooleanFlag(t *testing.T) {
	got := ident.RedactCmd([]string{"--token", "--verbose", "--port", "3000"})
	if strings.Contains(got, "***") {
		t.Fatalf("over-redacted: %s", got)
	}
	if !strings.Contains(got, "--verbose") {
		t.Fatalf("dropped flag: %s", got)
	}
	if !strings.Contains(got, "--port 3000") {
		t.Fatalf("over-redacted: %s", got)
	}
}

func TestRedactCmdSecretShapes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		leak []string // must not appear
		keep []string // must appear
	}{
		{"url creds in env assignment", []string{"DATABASE_URL=postgres://alice:swordfish@db.internal:5432/app", "npm", "run", "dev"},
			[]string{"alice", "swordfish"}, []string{"DATABASE_URL=postgres://***@db.internal:5432/app", "npm run dev"}},
		{"url creds bare arg", []string{"git", "clone", "https://user:pa55w0rd@example.com/repo.git"},
			[]string{"pa55w0rd", "user:"}, []string{"https://***@example.com/repo.git"}},
		{"url user only", []string{"curl", "https://bob@example.com/"},
			[]string{"bob"}, []string{"https://***@example.com/"}},
		{"authorization header single arg", []string{"curl", "-H", "Authorization: Bearer sk-abc123XYZ", "https://x/"},
			[]string{"sk-abc123XYZ"}, []string{"Authorization: ***", "https://x/"}},
		{"cookie header", []string{"curl", "--header", "Cookie: session=deadbeef", "https://x/"},
			[]string{"deadbeef"}, []string{"Cookie: ***"}},
		{"proxy auth header", []string{"curl", "-H", "Proxy-Authorization: Basic abc==", "https://x/"},
			[]string{"abc=="}, []string{"Proxy-Authorization: ***"}},
		{"bearer inside arg", []string{"http", "GET", "https://x/", "Authorization:Bearer tok123"},
			[]string{"tok123"}, []string{"Authorization: ***"}},
		{"wider keys", []string{"--auth=abc", "--credential", "cred1", "DSN=mysql://root:pw@h/db", "APIKEY=k1", "-p", "topsecret"},
			[]string{"abc", "cred1", "root:pw", "k1", "topsecret"}, []string{"--auth=***", "--credential ***", "DSN=***", "APIKEY=***", "-p ***"}},
		{"harmless keeps", []string{"npm", "run", "dev", "--port", "3000", "--host", "0.0.0.0"},
			[]string{"***"}, []string{"--port 3000", "--host 0.0.0.0"}},
	}
	for _, c := range cases {
		got := ident.RedactCmd(c.args)
		for _, l := range c.leak {
			if strings.Contains(got, l) {
				t.Errorf("%s: leaked %q in %q", c.name, l, got)
			}
		}
		for _, k := range c.keep {
			if !strings.Contains(got, k) {
				t.Errorf("%s: missing %q in %q", c.name, k, got)
			}
		}
	}
}

func TestRedactCmdTruncatesPerArgument(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := ident.RedactCmd([]string{"cmd", long, "--password=hunter2"})
	if strings.Contains(got, "hunter2") {
		t.Fatalf("late secret leaked after a long argument: %q", got)
	}
	if !strings.Contains(got, "--password=***") {
		t.Fatalf("late secret not redacted: %q", got)
	}
	if len(got) > 2048 {
		t.Fatalf("total length %d exceeds cap", len(got))
	}
	// The long argument itself is cut, not the tail of the command line.
	if !strings.Contains(got, "…") {
		t.Fatalf("expected truncation marker in %q", got)
	}
}
