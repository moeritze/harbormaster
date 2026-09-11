//go:build unix

package registry

import (
	"fmt"
	"os"
	"syscall"
)

// checkOwner refuses a state directory owned by another user: harbormaster
// keeps one user's bookkeeping, and a directory somebody else owns is
// somebody else's to change underneath us.
func checkOwner(dir string, fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("state dir %s is owned by uid %d, not %d; refusing to operate", dir, st.Uid, os.Getuid())
	}
	return nil
}
