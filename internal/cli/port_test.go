package cli_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/internal/ports"
	"github.com/moeritze/harbormaster/internal/registry"
)

// TestPortIsStable: `port` is what users paste into .env.local, so it must
// not move when the deterministic port happens to be busy.
func TestPortIsStable(t *testing.T) {
	h := newHarness(t, "me", "/wt/a")
	det := ports.Deterministic("/wt/a", h.app.Ports)
	stderr := &bytes.Buffer{}
	h.app.Stderr = stderr
	if err := h.run("port"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(h.out.String()); got != fmt.Sprint(det) || stderr.Len() != 0 {
		t.Fatalf("free: stdout %q stderr %q, want %d", got, stderr, det)
	}
	h.seed(t, registry.Entry{ID: "x", Port: det, PID: 77, Agent: "claude", Session: "other", Worktree: "/wt/b"})
	if err := h.run("port"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(h.out.String()); got != fmt.Sprint(det) {
		t.Fatalf("busy: stdout %q, want %d", got, det)
	}
	if !strings.Contains(stderr.String(), fmt.Sprint(det+1)) {
		t.Fatalf("busy: stderr %q must name the fallback %d", stderr, det+1)
	}
}
