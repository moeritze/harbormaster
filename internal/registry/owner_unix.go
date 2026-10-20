//go:build unix

package registry

import (
	"fmt"
	"os"
	"syscall"
)

// checkPerms refuses a state directory that group or other can write, or
// one owned by another user: harbormaster keeps one user's bookkeeping, and a
// directory somebody else owns is somebody else's to change underneath us.
func checkPerms(dir string, fi os.FileInfo) error {
	if mode := fi.Mode().Perm(); mode&0o022 != 0 {
		return fmt.Errorf("state dir %s is group/other-writable (%o); fix with chmod 700", dir, mode)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("state dir %s is owned by uid %d, not %d; refusing to operate", dir, st.Uid, os.Getuid())
	}
	return nil
}
