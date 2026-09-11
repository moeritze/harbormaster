package registry

import "time"

// pruneEntries removes entries whose process is no longer running and
// returns a HistoryRecord for each one removed; f.Entries is left holding
// only entries whose PID is still alive.
//
// This is a minimal PID-liveness check only, added to make the commit
// ordering in registry.go (write registry, then append history) testable
// end-to-end. Task 5 replaces this with the full prune policy (port
// liveness, grace periods, richer reasons, etc).
func pruneEntries(f *File, p Prober, now time.Time) []HistoryRecord {
	kept := make([]Entry, 0, len(f.Entries))
	var pruned []HistoryRecord
	for _, e := range f.Entries {
		if p.PidAlive(e.PID) {
			kept = append(kept, e)
			continue
		}
		pruned = append(pruned, HistoryRecord{Entry: e, Reason: "process exited", At: now})
	}
	f.Entries = kept
	return pruned
}
