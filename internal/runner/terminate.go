package runner

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Guard refuses pids harbormaster must never signal, even with --force (spec §10).
func Guard(pid int, uid func(int) (int, error)) error {
	if pid <= 1 {
		return fmt.Errorf("refusing to signal pid %d", pid)
	}
	if pid == os.Getpid() {
		return errors.New("refusing to signal my own pid")
	}
	u, err := uid(pid)
	if err != nil {
		return fmt.Errorf("cannot determine owner of pid %d: %w", pid, err)
	}
	if u != os.Getuid() {
		return fmt.Errorf("pid %d belongs to uid %d, not you", pid, u)
	}
	return nil
}

// ErrNoStartTime is returned for a group signal against an entry that never
// recorded a start time. The message names the repair, because the entry is
// repairable: `harbormaster ls` prunes, and pruning backfills the start time
// of every live entry that is missing one.
var ErrNoStartTime = errors.New("entry has no recorded start time; refusing a process-group signal (run harbormaster ls to backfill, then retry)")

// CheckStartTime refuses to signal pid when the process now occupying it is
// not the one the entry was written for. stored is the start time recorded
// at registration; a nil lookup, or a platform that cannot report start
// times at all, disables the check.
//
// group says the signal would go to the whole process group, which changes
// what a MISSING start time is allowed to mean. Signalling a single reused
// pid hits one wrong process; signalling -pid hits every process in whatever
// group holds that id now -- a login shell's group, say. So an entry that
// cannot be verified may still be signalled on its own (runner.Guard still
// applies to it), but never as a group.
func CheckStartTime(pid int, stored string, group bool, lookup func(int) (string, error)) error {
	if lookup == nil {
		return nil
	}
	if stored == "" {
		if !group {
			return nil
		}
		// Only refuse where a start time could have been recorded: on a
		// platform that has none (windows) every entry would be refused.
		if cur, err := lookup(pid); err != nil || cur == "" {
			return nil
		}
		return ErrNoStartTime
	}
	cur, err := lookup(pid)
	if err != nil {
		return fmt.Errorf("cannot verify pid %d start time: %w", pid, err)
	}
	if cur != stored {
		return fmt.Errorf("pid %d was reused by another process (started %s, registered %s); refusing to signal it", pid, cur, stored)
	}
	return nil
}

// Terminate sends SIGTERM (to the process group when group is true), waits up
// to timeout for exit, then SIGKILL.
func Terminate(pid int, group bool, timeout time.Duration) error {
	if err := sendTerm(pid, group); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return sendKill(pid, group)
}
