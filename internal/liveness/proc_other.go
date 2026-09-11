//go:build unix && !linux

package liveness

// procStartTime has no /proc/<pid>/stat to read on this platform; ps answers
// instead (see PidStartTime).
func procStartTime(int) (string, bool) { return "", false }
