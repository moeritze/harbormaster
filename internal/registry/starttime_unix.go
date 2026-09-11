//go:build unix

package registry

import "github.com/moeritze/harbormaster/internal/liveness"

// defaultStartTimeOf is the platform start-time lookup used to backfill
// entries that have none. A failure (the process is gone, `ps` is missing)
// is reported as "cannot tell" so the entry is left exactly as it is.
func defaultStartTimeOf(pid int) string {
	st, err := liveness.PidStartTime(pid)
	if err != nil {
		return ""
	}
	return st
}
