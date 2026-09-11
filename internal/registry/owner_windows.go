//go:build windows

package registry

import "os"

// checkOwner is a no-op on Windows: there is no uid to compare against, and
// file ownership there is not modelled by the syscall.Stat_t this check
// reads on unix.
func checkOwner(_ string, _ os.FileInfo) error { return nil }
