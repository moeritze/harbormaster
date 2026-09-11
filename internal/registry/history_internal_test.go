package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type alwaysAliveInternal struct{}

func (alwaysAliveInternal) PidAlive(int) bool      { return true }
func (alwaysAliveInternal) PortListening(int) bool { return true }

func fixedNowInternal() time.Time {
	return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
}

// TestReadHistoryLinesSkipsOverlongLines pins H9: a single damaged,
// over-long line in history.jsonl must not break every later read and
// write. The line is skipped and counted; the valid record after it is
// still returned.
func TestReadHistoryLinesSkipsOverlongLines(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hm"), alwaysAliveInternal{}, fixedNowInternal)
	if err != nil {
		t.Fatal(err)
	}
	good, err := json.Marshal(HistoryRecord{Entry: Entry{ID: "a", Port: 3000, PID: 1, StartedAt: fixedNowInternal()}, Reason: "test", At: fixedNowInternal()})
	if err != nil {
		t.Fatal(err)
	}
	junk := strings.Repeat("x", 2*1024*1024)
	content := junk + "\n" + string(good) + "\n"
	if err := os.WriteFile(filepath.Join(s.Dir(), historyName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	lines, skipped, err := s.readHistoryLines()
	if err != nil {
		t.Fatalf("an over-long line must not fail the read: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], `"port":3000`) {
		t.Fatalf("lines = %q", lines)
	}

	recs, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Port != 3000 {
		t.Fatalf("History = %+v", recs)
	}

	// A later append still works and drops the damaged line from the file.
	if err := s.AppendHistory(HistoryRecord{Entry: Entry{ID: "b", Port: 3001}, Reason: "test", At: fixedNowInternal()}); err != nil {
		t.Fatalf("append over a damaged history: %v", err)
	}
	recs, err = s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[1].Port != 3001 {
		t.Fatalf("History after append = %+v", recs)
	}
}
