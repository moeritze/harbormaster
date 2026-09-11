package registry_test

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

// countingProber is alive for every pid, closed for every port, and counts
// probes so tests can assert batching and the per-call cap.
type countingProber struct {
	ports atomic.Int64
}

func (c *countingProber) PidAlive(int) bool { return true }
func (c *countingProber) PortListening(int) bool {
	c.ports.Add(1)
	return false
}

func seedStale(t *testing.T, dir string, now time.Time, n int) {
	t.Helper()
	s, err := registry.Open(dir, alwaysAlive{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(f *registry.File) error {
		for i := 0; i < n; i++ {
			f.Entries = append(f.Entries, registry.Entry{ID: fmt.Sprintf("e%d", i), Port: 20000 + i, PID: 1, StartedAt: now.Add(-2 * time.Minute)})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPruneFiftyStaleEntriesIsFast(t *testing.T) {
	now := fixedNow()
	dir := filepath.Join(t.TempDir(), "hm")
	seedStale(t, dir, now, 50)
	pr := &countingProber{}
	s, _ := registry.Open(dir, pr, func() time.Time { return now })
	start := time.Now()
	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > pruneBudget {
		t.Fatalf("Load with 50 stale entries took %s, want < %s", d, pruneBudget)
	}
	if len(f.Entries) != 0 {
		t.Fatalf("expected all pruned, %d left", len(f.Entries))
	}
	if n := pr.ports.Load(); n != 100 {
		t.Fatalf("expected two probes per entry (100), got %d", n)
	}
}

func TestPruneCapsWorkPerCall(t *testing.T) {
	now := fixedNow()
	dir := filepath.Join(t.TempDir(), "hm")
	seedStale(t, dir, now, 250)
	pr := &countingProber{}
	s, _ := registry.Open(dir, pr, func() time.Time { return now })
	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 50 {
		t.Fatalf("expected 50 unprobed entries to survive this call, got %d", len(f.Entries))
	}
	if n := pr.ports.Load(); n > 400 {
		t.Fatalf("probe count %d exceeds the per-call cap", n)
	}
	g, _ := s.Load()
	if len(g.Entries) != 0 {
		t.Fatalf("second call should finish the job, %d left", len(g.Entries))
	}
}

func TestRemoveTakesTargetBeforePrune(t *testing.T) {
	now := fixedNow()
	dir := filepath.Join(t.TempDir(), "hm")
	seed, _ := registry.Open(dir, alwaysAlive{}, func() time.Time { return now })
	_ = seed.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries,
			registry.Entry{ID: "dead", Port: 3000, PID: 2, StartedAt: now},
			registry.Entry{ID: "other-dead", Port: 3001, PID: 3, StartedAt: now})
		return nil
	})
	dead := fakeProber{alive: map[int]bool{2: false, 3: false}, listening: map[int]bool{}}
	s, _ := registry.Open(dir, dead, func() time.Time { return now })
	e, found, err := s.Remove("dead")
	if err != nil || !found || e.Port != 3000 {
		t.Fatalf("Remove must find the dead entry before pruning: found=%v err=%v e=%+v", found, err, e)
	}
	hist, _ := s.History(0)
	for _, h := range hist {
		if h.ID == "dead" {
			t.Fatalf("Remove must not write a prune record for its own target: %+v", h)
		}
		if h.ID == "other-dead" && h.Reason != "pid_dead" {
			t.Fatalf("other dead entries are still pruned: %+v", h)
		}
	}
	if len(hist) != 1 {
		t.Fatalf("expected exactly one prune record (other-dead), got %d", len(hist))
	}
}
