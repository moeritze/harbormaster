//go:build windows

package liveness

import (
	"errors"

	"golang.org/x/sys/windows"
)

// stillActive is STILL_ACTIVE, the exit code of a process that has not exited.
const stillActive = 259

// PidAlive asks for the exit code: os.FindProcess succeeds for an exited
// process whose object another handle still holds, so it cannot tell.
func (OS) PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // pid > 0 checked above; windows pids are 32-bit
	if err != nil {
		// Like EPERM on unix: the process exists, it just isn't ours.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}

func PidUID(int) (int, error) { return 0, errors.New("PidUID not supported on windows") }

func PidOnPort(int) (int, string, bool) { return 0, "", false }

// PidStartTime is unavailable on windows; an empty value disables the check.
func PidStartTime(int) (string, error) { return "", nil }
