package registry_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

type alwaysAlive struct{}

func (alwaysAlive) PidAlive(int) bool      { return true }
func (alwaysAlive) PortListening(int) bool { return true }

// deadPID reports every pid dead except the ones listed as alive.
type deadPID struct{ alive map[int]bool }

func (d deadPID) PidAlive(pid int) bool { return d.alive[pid] }
func (deadPID) PortListening(int) bool  { return true }

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
	left, err := filepath.Glob(filepath.Join(s.Dir(), "registry-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestRemove(t *testing.T) {
	s := open(t)
	if err := s.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries,
			registry.Entry{ID: "a", Port: 3000, PID: 1, StartedAt: fixedNow()},
			registry.Entry{ID: "b", Port: 3001, PID: 2, StartedAt: fixedNow()},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	removed, ok, err := s.Remove("a")
	if err != nil || !ok || removed.ID != "a" || removed.Port != 3000 {
		t.Fatalf("remove present: removed=%+v ok=%v err=%v", removed, ok, err)
	}
	f, _ := s.Load()
	if len(f.Entries) != 1 || f.Entries[0].ID != "b" {
		t.Fatalf("entries after remove: %+v", f.Entries)
	}

	_, ok, err = s.Remove("nope")
	if err != nil || ok {
		t.Fatalf("remove absent: ok=%v err=%v", ok, err)
	}
	f, _ = s.Load()
	if len(f.Entries) != 1 || f.Entries[0].ID != "b" {
		t.Fatalf("entries changed after removing an absent id: %+v", f.Entries)
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

func TestFailedUpdateDoesNotPersistPrunedHistory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hm")
	// pid 123 is reported dead from the start; the seeded entry only becomes
	// prunable once it's actually on disk (pruning runs before fn on each call).
	s, err := registry.Open(dir, deadPID{alive: map[int]bool{}}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries, registry.Entry{ID: "a", Port: 4000, PID: 123, StartedAt: fixedNow()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(s.Dir(), "registry.json")
	before, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	if err := s.Update(func(*registry.File) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}

	after, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("registry.json changed even though fn returned an error")
	}
	if recs, err := s.History(0); err != nil {
		t.Fatal(err)
	} else if len(recs) != 0 {
		t.Fatalf("expected no history records after failed Update, got %d", len(recs))
	}

	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 0 {
		t.Fatalf("expected the dead-pid entry to be pruned, got %+v", f.Entries)
	}
	recs, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected exactly one history record after Load, got %d", len(recs))
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

func TestPrunePrunesAndReturnsRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hm")
	s, err := registry.Open(dir, deadPID{alive: map[int]bool{1: true}}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries,
			registry.Entry{ID: "alive", Port: 3000, PID: 1, StartedAt: fixedNow()},
			registry.Entry{ID: "dead", Port: 3001, PID: 2, StartedAt: fixedNow()},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	pruned, err := s.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 1 || pruned[0].ID != "dead" || pruned[0].Reason != "pid_dead" {
		t.Fatalf("pruned = %+v", pruned)
	}

	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 1 || f.Entries[0].ID != "alive" {
		t.Fatalf("entries = %+v", f.Entries)
	}

	hist, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Reason != "pid_dead" {
		t.Fatalf("history = %+v", hist)
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

// --- H1: symlink-safe atomic writes -----------------------------------

// TestWriteIgnoresPlantedTempSymlink pins H1: the registry write no longer
// uses a predictable "registry.json.tmp" path, so a symlink planted there
// cannot be used to make harbormaster write through it.
func TestWriteIgnoresPlantedTempSymlink(t *testing.T) {
	s := open(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(s.Dir(), "registry.json.tmp")); err != nil {
		t.Fatal(err)
	}

	if err := s.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries, registry.Entry{ID: "a", Port: 3000, PID: 1, StartedAt: fixedNow()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Lstat(filepath.Join(s.Dir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() {
		t.Fatalf("registry.json is not a regular file: %v", fi.Mode())
	}
	b, err := os.ReadFile(victim) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "untouched" {
		t.Fatalf("symlink target was written through: %q", b)
	}
}

// TestAppendHistoryIgnoresPlantedTempSymlink is the history.jsonl half of H1.
func TestAppendHistoryIgnoresPlantedTempSymlink(t *testing.T) {
	s := open(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(s.Dir(), "history.jsonl.tmp")); err != nil {
		t.Fatal(err)
	}

	if err := s.AppendHistory(registry.HistoryRecord{Entry: registry.Entry{ID: "x", Port: 3000}, Reason: "test", At: fixedNow()}); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Lstat(filepath.Join(s.Dir(), "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() {
		t.Fatalf("history.jsonl is not a regular file: %v", fi.Mode())
	}
	b, err := os.ReadFile(victim) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "untouched" {
		t.Fatalf("symlink target was written through: %q", b)
	}
}

func TestLoadRefusesSymlinkedRegistry(t *testing.T) {
	s := open(t)
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte(`{"version":1,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(s.Dir(), "registry.json")); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load()
	if err == nil || !strings.Contains(err.Error(), "is a symlink; refusing to operate") {
		t.Fatalf("got %v, want a symlink refusal", err)
	}
}

func TestHistoryRefusesSymlinkedFile(t *testing.T) {
	s := open(t)
	target := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	if err := os.WriteFile(target, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(s.Dir(), "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.History(0); err == nil || !strings.Contains(err.Error(), "is a symlink; refusing to operate") {
		t.Fatalf("History: got %v, want a symlink refusal", err)
	}
	err := s.AppendHistory(registry.HistoryRecord{Entry: registry.Entry{ID: "x"}, Reason: "test", At: fixedNow()})
	if err == nil || !strings.Contains(err.Error(), "is a symlink; refusing to operate") {
		t.Fatalf("AppendHistory: got %v, want a symlink refusal", err)
	}
}

// --- H2: state directory validation -----------------------------------

func TestOpenRefusesSymlinkedStateDir(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := registry.Open(link, alwaysAlive{}, fixedNow)
	if err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("got %v, want a symlink refusal", err)
	}
}

func TestOpenRefusesGroupOtherWritableStateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hm")
	if err := os.Mkdir(dir, 0o777); err != nil { //nolint:gosec // the world-writable dir is exactly what this test asserts is refused
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil { //nolint:gosec // defeat the umask: the test needs the write bits actually set
		t.Fatal(err)
	}
	_, err := registry.Open(dir, alwaysAlive{}, fixedNow)
	if err == nil || !strings.Contains(err.Error(), "group/other-writable") {
		t.Fatalf("got %v, want a group/other-writable refusal", err)
	}
	if !strings.Contains(err.Error(), "chmod 700") {
		t.Fatalf("error should say how to fix it: %v", err)
	}
}

// TestOpenAcceptsExistingPrivateDirUnchanged is H2's happy path: a
// pre-existing directory that is not writable by group or other is accepted
// and, per the existing rule, never re-chmodded.
func TestOpenAcceptsExistingPrivateDirUnchanged(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hm")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o750); err != nil { //nolint:gosec // group-readable on purpose: this dir must be accepted and left alone
		t.Fatal(err)
	}
	s, err := registry.Open(dir, alwaysAlive{}, fixedNow)
	if err != nil {
		t.Fatalf("group-readable but not group-writable dir must be accepted: %v", err)
	}
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o750 {
		t.Fatalf("existing dir was re-chmodded to %o", st.Mode().Perm())
	}
}

func TestPeekDoesNotPrune(t *testing.T) {
	now := fixedNow()
	dead := fakeProber{alive: map[int]bool{1: false}, listening: map[int]bool{}}
	s, _ := registry.Open(filepath.Join(t.TempDir(), "hm"), dead, func() time.Time { return now })
	seedStore, _ := registry.Open(s.Dir(), alwaysAlive{}, func() time.Time { return now })
	_ = seedStore.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries, registry.Entry{ID: "d", Port: 3000, PID: 1, StartedAt: now.Add(-time.Hour)})
		return nil
	})
	f, err := s.Peek()
	if err != nil || len(f.Entries) != 1 {
		t.Fatalf("peek should not prune: %v %+v", err, f)
	}
	g, _ := s.Load()
	if len(g.Entries) != 0 {
		t.Fatal("load should prune")
	}
}
