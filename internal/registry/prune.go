package registry

import (
	"sync"
	"time"
)

// ListenGrace is how long after StartedAt an entry may have a closed port
// without being pruned. Matches the run command's listen timeout.
const ListenGrace = 60 * time.Second

// maxProbePerCall bounds how many port probes one read performs. A registry
// full of stale rows (a crashed machine, a restored backup) is cleaned over
// a few calls instead of stalling the first one; entries past the cap are
// kept untouched until the next read.
const maxProbePerCall = 200

// probeWorkers bounds concurrent port dials.
const probeWorkers = 8

// reprobeDelay separates the two liveness probes a port gets. It is a var
// so tests can drop it to zero.
var reprobeDelay = 100 * time.Millisecond

// pruneEntries removes entries whose process has died or whose port has
// stopped listening (after ListenGrace) and returns a HistoryRecord for
// each one removed; f.Entries is left holding only surviving entries.
//
// Port probes are batched: every candidate is dialed once concurrently,
// the re-probe delay is waited ONCE, and only the ports that failed the
// first pass are dialed again. Fifty stale entries therefore cost about one
// delay plus a handful of dials, not fifty delays in a row.
func pruneEntries(f *File, p Prober, now time.Time) []HistoryRecord {
	if p == nil {
		return nil
	}
	kept := f.Entries[:0]
	var pruned []HistoryRecord
	var candidates []Entry
	for _, e := range f.Entries {
		switch {
		case !p.PidAlive(e.PID):
			pruned = append(pruned, HistoryRecord{Entry: e, Reason: "pid_dead", At: now})
		case now.Sub(e.StartedAt) >= ListenGrace && len(candidates) < maxProbePerCall:
			candidates = append(candidates, e)
		default:
			kept = append(kept, e)
		}
	}
	closed := closedPorts(p, candidates)
	for _, e := range candidates {
		if closed[e.Port] {
			pruned = append(pruned, HistoryRecord{Entry: e, Reason: "port_closed", At: now})
		} else {
			kept = append(kept, e)
		}
	}
	f.Entries = kept
	return pruned
}

// closedPorts reports which candidate ports failed two probes reprobeDelay
// apart. A single failed dial is not enough: a dev server that is
// restarting, reloading its config, or momentarily refusing connections
// under load would otherwise have its live entry pruned out from under it.
func closedPorts(p Prober, candidates []Entry) map[int]bool {
	if len(candidates) == 0 {
		return nil
	}
	ports := make([]int, 0, len(candidates))
	seen := map[int]bool{}
	for _, e := range candidates {
		if !seen[e.Port] {
			seen[e.Port] = true
			ports = append(ports, e.Port)
		}
	}
	first := probeAll(p, ports)
	if len(first) == 0 {
		return nil
	}
	time.Sleep(reprobeDelay)
	closed := map[int]bool{}
	for _, port := range probeAll(p, first) {
		closed[port] = true
	}
	return closed
}

// probeAll dials each port with bounded concurrency and returns the ones
// that did not answer.
func probeAll(p Prober, ports []int) []int {
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, probeWorkers)
	var closed []int
	for _, port := range ports {
		wg.Add(1)
		sem <- struct{}{}
		go func(port int) {
			defer wg.Done()
			defer func() { <-sem }()
			if !p.PortListening(port) {
				mu.Lock()
				closed = append(closed, port)
				mu.Unlock()
			}
		}(port)
	}
	wg.Wait()
	return closed
}
