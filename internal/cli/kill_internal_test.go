package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/registry"
)

type liveProber struct{}

func (liveProber) PidAlive(int) bool      { return true }
func (liveProber) PortListening(int) bool { return true }

// TestRestoreAfterFailedKillSkipsEntriesThatWereAlreadyGone covers the
// found == false branch: something else had already unregistered the entry,
// so putting it back would resurrect a row nobody owns.
func TestRestoreAfterFailedKillSkipsEntriesThatWereAlreadyGone(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	st, err := registry.Open(filepath.Join(t.TempDir(), "hm"), liveProber{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	st.StartTimeOf = nil
	out := &bytes.Buffer{}
	a := &app.App{Stdout: out, Stderr: out, Store: st, Prober: liveProber{}, Now: func() time.Time { return now }}
	e := registry.Entry{ID: "gone", Port: 3600, PID: 4242, Session: "s1", StartedAt: now}

	restoreAfterFailedKill(a, e, false)

	f, err := st.Peek()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 0 {
		t.Fatalf("an entry that was already gone must not come back: %+v", f.Entries)
	}
	if !strings.Contains(out.String(), "already gone") {
		t.Fatalf("expected a warning, got %q", out.String())
	}
}
