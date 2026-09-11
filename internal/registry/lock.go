//go:build unix

package registry

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// lock acquires an exclusive flock on path, polling until timeout.
func lock(path string, timeout time.Duration) (func(), error) {
	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // path is built from Store.dir, not user input
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(int(fd.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(fd.Fd()), unix.LOCK_UN)
				_ = fd.Close()
			}, nil
		}
		if err != unix.EWOULDBLOCK {
			_ = fd.Close()
			return nil, fmt.Errorf("flock: %w", err)
		}
		if time.Now().After(deadline) {
			_ = fd.Close()
			return nil, fmt.Errorf("registry locked by another process for more than %s", timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
