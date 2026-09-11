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

// CheckStartTime refuses to signal pid when the process now occupying it is
// not the one the entry was written for. stored is the start time recorded
// at registration; an empty stored value or a nil lookup disables the check.
func CheckStartTime(pid int, stored string, lookup func(int) (string, error)) error {
	if stored == "" || lookup == nil {
		return nil
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
