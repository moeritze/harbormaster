//go:build windows

package registry

// defaultStartTimeOf has nothing to report on windows, where a process start
// time is not available at all (see liveness.PidStartTime), so nothing is
// ever backfilled there.
func defaultStartTimeOf(int) string { return "" }
