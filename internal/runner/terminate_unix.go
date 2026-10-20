//go:build unix

package runner

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// target picks the signal target. A group signal is only sent when pid is
// actually a process-group leader (Getpgid(pid) == pid); anything else --
// a claimed pid that never called setpgid, or a reused pid inside some
// unrelated group -- is signalled on its own.
func target(pid int, group bool) int {
	if !group {
		return pid
	}
	// Getpgid fails once the leader has been reaped; the orphaned group may
	// still hold workers, and kill(-pid) is exactly what reaches them, so an
	// error keeps the group target. Only a live pid that answers with a
	// different group id is downgraded to a single-pid signal.
	if pgid, err := unix.Getpgid(pid); err == nil && pgid != pid {
		return pid
	}
	return -pid
}

func sendTerm(pid int, group bool) error {
	err := syscall.Kill(target(pid, group), syscall.SIGTERM)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func sendKill(pid int, group bool) error {
	err := syscall.Kill(target(pid, group), syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if group && errors.Is(err, syscall.EPERM) {
		// BSD/macOS observed behavior: once every member of a process group
		// has exited and only an unreaped zombie leader remains, killpg(2)
		// against that group can report EPERM instead of ESRCH. sendTerm
		// already succeeded against this same target, so a later EPERM here
		// means "nothing left to kill", not "not allowed".
		return nil
	}
	return err
}

// gone reports that nothing is left to wait for: for a group target, no
// member of the group remains (EPERM counts as gone there, the darwin
// zombie-leader quirk sendKill documents); for a single pid, EPERM is alive.
func gone(pid int, group bool) bool {
	t := target(pid, group)
	err := syscall.Kill(t, 0)
	return errors.Is(err, syscall.ESRCH) || (t < 0 && errors.Is(err, syscall.EPERM))
}

// exitCode reports a signaled child as 128+signal (shell convention),
// otherwise the process's exit status. syscall.WaitStatus is a unix-only
// concept so this lives in the unix build; see terminate_windows.go for the
// windows equivalent.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	return 1
}
