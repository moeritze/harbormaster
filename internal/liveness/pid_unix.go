//go:build unix

package liveness

import (
	"bytes"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// PidAlive sends signal 0. EPERM means the process exists but belongs to someone else.
func (OS) PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// PidUID returns the real uid of pid via ps.
func PidUID(pid int) (int, error) {
	out, err := exec.Command("ps", "-o", "uid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, errors.New("no such process")
	}
	return strconv.Atoi(s)
}

// PidStartTime returns an opaque, stable description of when pid started
// (ps's lstart column). Two processes that ever share a pid still differ
// here, which is what lets a signal be refused after pid reuse.
func PidStartTime(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", errors.New("no such process")
	}
	return s, nil
}

// PidOnPort finds the listening pid on a TCP port via lsof. ok=false if unknown.
func PidOnPort(port int) (int, string, bool) {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return 0, "", false
	}
	var pid int
	var cmd string
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(string(line[1:]))
		case 'c':
			cmd = string(line[1:])
		}
		if pid != 0 && cmd != "" {
			return pid, cmd, true
		}
	}
	if pid != 0 {
		return pid, cmd, true
	}
	return 0, "", false
}
