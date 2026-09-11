//go:build unix

package liveness

import (
	"bytes"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/moeritze/harbormaster/internal/tools"
)

// pinnedEnv is the environment every helper process is run with. Locale and
// time zone decide how `ps` formats the start time it prints ("Do Sep 11
// 10:00:00 2026" under LC_ALL=de_DE.UTF-8, a different wall clock under
// TZ=Europe/Berlin), so an entry registered from one shell would not match
// the lookup made from another -- and a start time that does not match reads
// as a reused pid, which refuses a kill the caller is entitled to. PATH is
// pinned as well so nothing the caller exported can influence what these
// programs find; the programs themselves are already resolved to absolute
// paths by tools.Path.
var pinnedEnv = []string{"LC_ALL=C", "LANG=C", "TZ=UTC", "PATH=/usr/bin:/bin"}

// output runs bin with args under pinnedEnv and returns its standard output.
// Every tool whose output harbormaster parses goes through here.
func output(bin string, args ...string) ([]byte, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = pinnedEnv
	return cmd.Output()
}

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
	ps := tools.Path("ps")
	if ps == "" {
		return 0, errors.New("ps not found; refusing to guess a process owner")
	}
	out, err := output(ps, "-o", "uid=", "-p", strconv.Itoa(pid))
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, errors.New("no such process")
	}
	return strconv.Atoi(s)
}

// PidStartTime returns an opaque, stable description of when pid started.
// Two processes that ever share a pid still differ here, which is what lets
// a signal be refused after pid reuse.
//
// On Linux the value is "proc:<ticks>" from /proc/<pid>/stat: an integer the
// kernel never reformats, immune to locale and time zone. Everywhere else it
// is ps's lstart column, read under a pinned environment for the same
// reason. The two forms never mix on one machine.
func PidStartTime(pid int) (string, error) {
	if s, ok := procStartTime(pid); ok {
		return s, nil
	}
	ps := tools.Path("ps")
	if ps == "" {
		return "", errors.New("ps not found")
	}
	out, err := output(ps, "-o", "lstart=", "-p", strconv.Itoa(pid))
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
	lsof := tools.Path("lsof")
	if lsof == "" {
		return 0, "", false
	}
	out, err := output(lsof, "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc")
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
