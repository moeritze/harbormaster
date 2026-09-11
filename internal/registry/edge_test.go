package registry_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

func TestPruneGraceBoundary(t *testing.T) {
	now := fixedNow()
	dir := filepath.Join(t.TempDir(), "hm")
	seed, _ := registry.Open(dir, alwaysAlive{}, func() time.Time { return now })
	_ = seed.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries,
			registry.Entry{ID: "at-grace", Port: 3000, PID: 1, StartedAt: now.Add(-registry.ListenGrace)},
			registry.Entry{ID: "inside-grace", Port: 3001, PID: 1, StartedAt: now.Add(-registry.ListenGrace + time.Second)})
		return nil
	})
	closed := fakeProber{alive: map[int]bool{1: true}, listening: map[int]bool{}}
	s, _ := registry.Open(dir, closed, func() time.Time { return now })
	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 1 || f.Entries[0].ID != "inside-grace" {
		t.Fatalf("exactly-at-grace must be pruned, one second inside must survive: %+v", f.Entries)
	}
}

func TestNilProberNeverPrunes(t *testing.T) {
	now := fixedNow()
	dir := filepath.Join(t.TempDir(), "hm")
	seed, _ := registry.Open(dir, alwaysAlive{}, func() time.Time { return now })
	_ = seed.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries, registry.Entry{ID: "x", Port: 3000, PID: 999999, StartedAt: now.Add(-time.Hour)})
		return nil
	})
	s, _ := registry.Open(dir, nil, func() time.Time { return now })
	f, err := s.Load()
	if err != nil || len(f.Entries) != 1 {
		t.Fatalf("nil prober must keep every entry: %+v %v", f, err)
	}
}

func TestHistorySkipsCorruptLine(t *testing.T) {
	s := open(t)
	rec := registry.HistoryRecord{Entry: registry.Entry{ID: "ok", Port: 3000}, Reason: "released", At: fixedNow()}
	if err := s.AppendHistory(rec); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(s.Dir(), "history.jsonl")
	b, _ := os.ReadFile(p)                                                                     //nolint:gosec // path is inside the test temp dir
	if err := os.WriteFile(p, append([]byte("this is not json\n"), b...), 0o600); err != nil { //nolint:gosec // path is inside the test temp dir
		t.Fatal(err)
	}
	got, err := s.History(0)
	if err != nil || len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("corrupt line must be skipped, valid record kept: %+v %v", got, err)
	}
}
