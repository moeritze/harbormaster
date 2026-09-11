//go:build windows

package registry

import (
	"fmt"
	"os"
	"time"
)

// lock opens the lock file on Windows. There is no flock equivalent used
// here; locking is best-effort per spec, so this just ensures the file
// exists and returns a no-op unlock.
func lock(path string, _ time.Duration) (func(), error) {
	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // path is built from Store.dir, not user input
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	return func() {
		_ = fd.Close()
	}, nil
}
