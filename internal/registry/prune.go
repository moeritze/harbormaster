package registry

import "time"

// ListenGrace is how long after StartedAt an entry may have a closed port
// without being pruned. Matches the run command's listen timeout.
const ListenGrace = 60 * time.Second

// pruneEntries removes entries whose process has died or whose port has
// stopped listening (after ListenGrace) and returns a HistoryRecord for
// each one removed; f.Entries is left holding only surviving entries.
func pruneEntries(f *File, p Prober, now time.Time) []HistoryRecord {
	if p == nil {
		return nil
	}
	kept := f.Entries[:0]
	var pruned []HistoryRecord
	for _, e := range f.Entries {
		switch {
		case !p.PidAlive(e.PID):
			pruned = append(pruned, HistoryRecord{Entry: e, Reason: "pid_dead", At: now})
		case now.Sub(e.StartedAt) >= ListenGrace && portClosed(p, e.Port):
			pruned = append(pruned, HistoryRecord{Entry: e, Reason: "port_closed", At: now})
		default:
			kept = append(kept, e)
		}
	}
	f.Entries = kept
	return pruned
}

// reprobeDelay separates the two liveness probes portClosed makes. It is a
// var so tests can drop it to zero.
var reprobeDelay = 100 * time.Millisecond

// portClosed reports a port as closed only when two probes reprobeDelay apart
// both fail. A single failed dial is not enough: a dev server that is
// restarting, reloading its config, or momentarily refusing connections under
// load would otherwise have its live entry pruned out from under it.
func portClosed(p Prober, port int) bool {
	if p.PortListening(port) {
		return false
	}
	time.Sleep(reprobeDelay)
	return !p.PortListening(port)
}
