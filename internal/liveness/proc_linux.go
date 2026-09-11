//go:build linux

package liveness

import (
	"os"
	"strconv"
	"strings"
)

// procStartTime reads field 22 of /proc/<pid>/stat -- starttime, in clock
// ticks since boot. It is preferred over ps because the kernel prints an
// integer: no locale, time zone or ps column format can change it, and its
// resolution is a clock tick rather than a whole second.
func procStartTime(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat") //nolint:gosec // path is built from an integer pid, not from user input
	if err != nil {
		return "", false
	}
	// Field 2 (comm) is wrapped in parentheses and may itself contain both
	// spaces and ')', so everything after it is found from the LAST ')'.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", false
	}
	fields := strings.Fields(s[i+1:])
	// fields[0] is field 3 (state), so field 22 is fields[19].
	const startTimeIdx = 19
	if len(fields) <= startTimeIdx {
		return "", false
	}
	if _, err := strconv.ParseUint(fields[startTimeIdx], 10, 64); err != nil {
		return "", false
	}
	return "proc:" + fields[startTimeIdx], true
}
