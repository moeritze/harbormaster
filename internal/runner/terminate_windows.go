//go:build windows

package runner

import (
	"errors"
	"os"
	"os/exec"
)

func setProcessGroup(*exec.Cmd) {}

func sendTerm(pid int, _ bool) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	return p.Kill()
}

func sendKill(pid int, group bool) error { return sendTerm(pid, group) }

func alive(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}

// exitCode has no signal concept on windows; forward the process exit code.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}
