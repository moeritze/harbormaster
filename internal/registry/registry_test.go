package registry_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

type alwaysAlive struct{}

func (alwaysAlive) PidAlive(int) bool      { return true }
func (alwaysAlive) PortListening(int) bool { return true }

func fixedNow() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

func open(t *testing.T) *registry.Store {
	t.Helper()
	s, err := registry.Open(filepath.Join(t.TempDir(), "hm"), alwaysAlive{}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenCreatesDirWithMode0700(t *testing.T) {
	s := open(t)
	st, err := os.Stat(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
}

func TestLoadEmptyReturnsVersion1(t *testing.T) {
	f, err := open(t).Load()
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || len(f.Entries) != 0 {
		t.Fatalf("got %+v", f)
	}
}

func TestUpdatePersistsAndFileMode0600(t *testing.T) {
	s := open(t)
	err := s.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries, registry.Entry{ID: "a", Port: 3000, PID: 1, StartedAt: fixedNow()})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := s.Load()
	if len(f.Entries) != 1 || f.Entries[0].Port != 3000 {
		t.Fatalf("got %+v", f.Entries)
	}
	st, _ := os.Stat(filepath.Join(s.Dir(), "registry.json"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "registry.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestLoadDoesNotRewriteWhenNothingPruned(t *testing.T) {
	s := open(t)
	if err := s.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries, registry.Entry{ID: "a", Port: 3000, PID: 1, StartedAt: fixedNow()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(s.Dir(), "registry.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, err := os.ReadFile(path) //nolint:gosec // path is s.Dir()/registry.json, test-controlled
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, err := os.ReadFile(path) //nolint:gosec // path is s.Dir()/registry.json, test-controlled
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("registry.json was rewritten: mtime %v -> %v", before.ModTime(), after.ModTime())
	}
	if string(beforeBytes) != string(afterBytes) {
		t.Fatal("registry.json bytes changed on Load with nothing pruned")
	}
}

func TestConcurrentUpdatesUnderLockProduceValidFile(t *testing.T) {
	s := open(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Update(func(f *registry.File) error {
				f.Entries = append(f.Entries, registry.Entry{ID: string(rune('a' + i)), Port: 3000 + i, PID: 1, StartedAt: fixedNow()}) //nolint:gosec // i is 0..19, no overflow
				return nil
			})
		}(i)
	}
	wg.Wait()
	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 20 {
		t.Fatalf("expected 20 entries, got %d", len(f.Entries))
	}
}

func TestHistoryAppendAndCap(t *testing.T) {
	s := open(t)
	for i := 0; i < 1005; i++ {
		if err := s.AppendHistory(registry.HistoryRecord{Entry: registry.Entry{ID: "x", Port: i}, Reason: "test", At: fixedNow()}); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1000 {
		t.Fatalf("expected 1000, got %d", len(recs))
	}
	if recs[0].Port != 5 {
		t.Fatalf("oldest should be trimmed, first port %d", recs[0].Port)
	}
	last, _ := s.History(2)
	if len(last) != 2 || last[1].Port != 1004 {
		t.Fatalf("limit: %+v", last)
	}
}

func TestLoadRejectsUnknownVersion(t *testing.T) {
	s := open(t)
	if err := os.WriteFile(filepath.Join(s.Dir(), "registry.json"), []byte(`{"version":99,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err == nil {
		t.Fatal("expected version error")
	}
}
