//go:build windows

package registry

import "os"

// checkPerms is a no-op on Windows: there is no uid to compare against, file
// ownership is not modelled by the syscall.Stat_t the unix check reads, and Go
// reports every directory as 0777 because Windows has no POSIX mode bits.
func checkPerms(_ string, _ os.FileInfo) error { return nil }
