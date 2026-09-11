//go:build windows

package liveness

import (
	"errors"
	"os"
)

func (OS) PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}

func PidUID(int) (int, error) { return 0, errors.New("PidUID not supported on windows") }

func PidOnPort(int) (int, string, bool) { return 0, "", false }

// PidStartTime is unavailable on windows; an empty value disables the check.
func PidStartTime(int) (string, error) { return "", nil }
