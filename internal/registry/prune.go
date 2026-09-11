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

// defaultProbeBudget bounds the wall time one read may spend dialing ports.
// The count cap alone is not enough: 200 dials that each sit out a full
// connect timeout (a firewalled port, a host under load) would block `ls`
// for the better part of a minute. Whatever is not reached within the
// budget is simply not probed this call and is carried to the next one.
const defaultProbeBudget = time.Second

// maxBackfillPerCall bounds how many start-time lookups one read performs.
// Each one forks a helper, so a huge registry is repaired over a few calls
// rather than making a single `ls` fork hundreds of processes.
const maxBackfillPerCall = 200

// probeWorkers bounds concurrent port dials.
const probeWorkers = 8

// reprobeDelay separates the two liveness probes a port gets. It is a var
// so tests can drop it to zero.
var reprobeDelay = 100 * time.Millisecond

// pruneOptions carries the injectable parts of one prune.
type pruneOptions struct {
	// startTimeOf backfills a surviving entry's missing start time. nil
	// disables backfilling.
	startTimeOf func(pid int) string
	// budget bounds the probe phase; zero means defaultProbeBudget.
	budget time.Duration
}

// pruneEntries removes entries whose process has died or whose port has
// stopped listening (after ListenGrace) and returns a HistoryRecord for
// each one removed; f.Entries is left holding only surviving entries. The
// second return value lists the indices in f.Entries whose start time was
// backfilled, so the caller knows the document must be written back and
// which rows were repaired.
//
// Port probes are batched: every candidate is dialed once concurrently,
// the re-probe delay is waited ONCE, and only the ports that failed the
// first pass are dialed again. Fifty stale entries therefore cost about one
// delay plus a handful of dials, not fifty delays in a row.
func pruneEntries(f *File, p Prober, now time.Time, opt pruneOptions) ([]HistoryRecord, []int) {
	if p == nil {
		return nil, nil
	}
	budget := opt.budget
	if budget <= 0 {
		budget = defaultProbeBudget
	}
	deadline := time.Now().Add(budget)

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
	closed := closedPorts(p, candidates, deadline)
	for _, e := range candidates {
		if closed[e.Port] {
			pruned = append(pruned, HistoryRecord{Entry: e, Reason: "port_closed", At: now})
		} else {
			kept = append(kept, e)
		}
	}
	f.Entries = kept
	return pruned, backfillStartTimes(f.Entries, opt.startTimeOf)
}

// backfillStartTimes records the start time of surviving entries that have
// none. Their pid was just confirmed alive, so the value read now describes
// the very process the entry is about. An entry without a start time cannot
// be checked for pid reuse at all, and a process-group signal is refused for
// it outright (runner.CheckStartTime), so this is what makes `harbormaster
// ls` -- which prunes, hence reaches here -- the way to repair such a row.
func backfillStartTimes(entries []Entry, lookup func(pid int) string) []int {
	if lookup == nil {
		return nil
	}
	var filled []int
	tried := 0
	for i := range entries {
		if entries[i].StartTime != "" || entries[i].PID <= 0 {
			continue
		}
		if tried++; tried > maxBackfillPerCall {
			break
		}
		if st := lookup(entries[i].PID); st != "" {
			entries[i].StartTime = st
			filled = append(filled, i)
		}
	}
	return filled
}

// closedPorts reports which candidate ports failed two probes reprobeDelay
// apart, within the deadline. A single failed dial is not enough: a dev
// server that is restarting, reloading its config, or momentarily refusing
// connections under load would otherwise have its live entry pruned out from
// under it.
//
// Half the remaining budget is reserved for the confirmation pass. A port
// dialed once tells us nothing on its own, so spending the whole budget on
// first probes would prune nothing at all; stopping the first pass half way
// instead means the ports that were reached get both of their dials and the
// rest simply wait for the next call.
func closedPorts(p Prober, candidates []Entry, deadline time.Time) map[int]bool {
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
	first := probeAll(p, ports, time.Now().Add(time.Until(deadline)/2))
	if len(first) == 0 {
		return nil
	}
	time.Sleep(reprobeDelay)
	closed := map[int]bool{}
	for _, port := range probeAll(p, first, deadline) {
		closed[port] = true
	}
	return closed
}

// probeAll dials the ports in waves of probeWorkers and returns the ones
// that did not answer. Ports left unprobed when the deadline passes are
// simply not reported, so their entries survive this call untouched.
func probeAll(p Prober, ports []int, deadline time.Time) []int {
	var mu sync.Mutex
	var closed []int
	for i := 0; i < len(ports); i += probeWorkers {
		if !time.Now().Before(deadline) {
			break
		}
		var wg sync.WaitGroup
		for _, port := range ports[i:min(i+probeWorkers, len(ports))] {
			wg.Add(1)
			go func(port int) {
				defer wg.Done()
				if !p.PortListening(port) {
					mu.Lock()
					closed = append(closed, port)
					mu.Unlock()
				}
			}(port)
		}
		wg.Wait()
	}
	return closed
}
