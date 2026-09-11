package registry_test

import (
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
