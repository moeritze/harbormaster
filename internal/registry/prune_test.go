package registry_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/registry"
)

type fakeProber struct {
	alive     map[int]bool
	listening map[int]bool
}

func (f fakeProber) PidAlive(pid int) bool       { return f.alive[pid] }
func (f fakeProber) PortListening(port int) bool { return f.listening[port] }

func TestPruneRemovesDeadPidAndClosedPortAfterGrace(t *testing.T) {
	now := fixedNow()
	p := fakeProber{
		alive:     map[int]bool{10: true, 11: false, 12: true, 13: true},
		listening: map[int]bool{3000: true, 3001: true, 3002: false, 3003: false},
	}
	s, err := registry.Open(filepath.Join(t.TempDir(), "hm"), p, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	seed := []registry.Entry{
		{ID: "ok", Port: 3000, PID: 10, StartedAt: now.Add(-time.Hour)},
		{ID: "deadpid", Port: 3001, PID: 11, StartedAt: now.Add(-time.Hour)},
		{ID: "closed-old", Port: 3002, PID: 12, StartedAt: now.Add(-2 * time.Minute)},
		{ID: "closed-fresh", Port: 3003, PID: 13, StartedAt: now.Add(-10 * time.Second)},
	}
	// Seed without pruning by using a store whose prober says everything is alive.
	seedStore, _ := registry.Open(s.Dir(), alwaysAlive{}, func() time.Time { return now })
	if err := seedStore.Update(func(f *registry.File) error { f.Entries = seed; return nil }); err != nil {
		t.Fatal(err)
	}

	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range f.Entries {
		ids[e.ID] = true
	}
	if !ids["ok"] || ids["deadpid"] || ids["closed-old"] || !ids["closed-fresh"] {
		t.Fatalf("surviving ids: %v", ids)
	}

	hist, _ := s.History(0)
	reasons := map[string]string{}
	for _, h := range hist {
		reasons[h.ID] = h.Reason
	}
	if reasons["deadpid"] != "pid_dead" || reasons["closed-old"] != "port_closed" {
		t.Fatalf("history reasons: %v", reasons)
	}
}

// flappingProber fails the first PortListening call for flapPort and answers
// true on every call after that: a dev server that refuses one connection
// while it is busy, then serves normally.
type flappingProber struct {
	flapPort int
	calls    map[int]int
}

func (f *flappingProber) PidAlive(int) bool { return true }

func (f *flappingProber) PortListening(port int) bool {
	f.calls[port]++
	return port != f.flapPort || f.calls[port] != 1
}

// TestPruneKeepsEntryThatFailsOnlyTheFirstProbe covers the two-probe rule:
// one failed dial must never prune a live entry.
func TestPruneKeepsEntryThatFailsOnlyTheFirstProbe(t *testing.T) {
	now := fixedNow()
	p := &flappingProber{flapPort: 3100, calls: map[int]int{}}
	s, err := registry.Open(filepath.Join(t.TempDir(), "hm"), p, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	seedStore, err := registry.Open(s.Dir(), alwaysAlive{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := seedStore.Update(func(f *registry.File) error {
		f.Entries = []registry.Entry{{ID: "flap", Port: 3100, PID: 10, StartedAt: now.Add(-time.Hour)}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 1 || f.Entries[0].ID != "flap" {
		t.Fatalf("entry pruned after a single failed probe: %+v", f.Entries)
	}
	if p.calls[3100] < 2 {
		t.Fatalf("expected a second probe, got %d call(s)", p.calls[3100])
	}
}

// TestPruneBackfillsMissingStartTime covers the repair path for entries that
// carry no start time: a surviving entry gets one recorded and the registry
// is written back even though nothing was pruned. Without this an entry
// written by an older build could never be group-signalled again.
func TestPruneBackfillsMissingStartTime(t *testing.T) {
	now := fixedNow()
	dir := filepath.Join(t.TempDir(), "hm")
	seed, err := registry.Open(dir, alwaysAlive{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	seed.StartTimeOf = nil
	if err := seed.Update(func(f *registry.File) error {
		f.Entries = append(f.Entries,
			registry.Entry{ID: "no-start", Port: 3000, PID: 42, StartedAt: now},
			registry.Entry{ID: "has-start", Port: 3001, PID: 43, StartedAt: now, StartTime: "already"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	s, err := registry.Open(dir, alwaysAlive{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var asked []int
	s.StartTimeOf = func(pid int) string {
		asked = append(asked, pid)
		return fmt.Sprintf("start-%d", pid)
	}
	loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != 42 {
		t.Fatalf("only the entry without a start time may be looked up, asked %v", asked)
	}
	// The repair counts from the next read on: a start time read now says
	// nothing about whether the pid was already reused before it was read,
	// so the caller that triggered the repair still sees an unverified row.
	for _, e := range loaded.Entries {
		if e.ID == "no-start" && e.StartTime != "" {
			t.Fatalf("the repairing read must still report the entry as unverified: %+v", e)
		}
	}

	// The backfill must be persisted, not just applied in memory.
	reader, err := registry.Open(dir, alwaysAlive{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	reader.StartTimeOf = nil
	f, err := reader.Peek()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range f.Entries {
		got[e.ID] = e.StartTime
	}
	if got["no-start"] != "start-42" || got["has-start"] != "already" {
		t.Fatalf("start times after backfill: %v", got)
	}
}

// slowProber sleeps on every dial. Twenty stale entries need 1.9 s of
// dialing without a budget (two passes of three waves plus the re-probe
// delay), which is exactly what the budget exists to cut short.
type slowProber struct{ dial time.Duration }

func (slowProber) PidAlive(int) bool { return true }
func (s slowProber) PortListening(int) bool {
	time.Sleep(s.dial)
	return false
}

// TestPruneStopsAtTheProbeBudget: the probe phase is bounded by wall time as
// well as by count. Entries it could not reach stay untouched for the next
// call rather than making this one hang.
func TestPruneStopsAtTheProbeBudget(t *testing.T) {
	now := fixedNow()
	dir := filepath.Join(t.TempDir(), "hm")
	seedStale(t, dir, now, 20)
	s, err := registry.Open(dir, slowProber{dial: 300 * time.Millisecond}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	s.StartTimeOf = nil // keep the measurement about dialing only
	s.ProbeBudget = time.Second

	start := time.Now()
	f, err := s.Load()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	// Unbounded this takes ~1.9 s; bounded it takes the budget plus the one
	// wave that was already in flight when it ran out.
	if elapsed > 1600*time.Millisecond {
		t.Fatalf("Load took %s; the probe budget did not bound it", elapsed)
	}
	if len(f.Entries) == 0 {
		t.Fatal("entries that could not be probed within the budget must survive")
	}
	if len(f.Entries) == 20 {
		t.Fatal("the entries that were probed within the budget must still be pruned")
	}
}
