// Package ports allocates a stable port per worktree.
package ports

import (
	"fmt"
	"hash/fnv"
	"strconv"
)

// Config is the allocation window [Base, Base+Range).
type Config struct {
	Base  int
	Range int
}

// ConfigFromEnv reads HARBORMASTER_BASE and HARBORMASTER_RANGE.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{Base: 3000, Range: 1000}
	if v := getenv("HARBORMASTER_BASE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return c, fmt.Errorf("HARBORMASTER_BASE must be 1-65535, got %q", v)
		}
		c.Base = n
	}
	if v := getenv("HARBORMASTER_RANGE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("HARBORMASTER_RANGE must be >= 1, got %q", v)
		}
		c.Range = n
	}
	if c.Base+c.Range-1 > 65535 {
		return c, fmt.Errorf("port window %d-%d exceeds 65535", c.Base, c.Base+c.Range-1)
	}
	return c, nil
}

// Deterministic maps key to a stable port inside the window.
func Deterministic(key string, c Config) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return c.Base + int(h.Sum64()%uint64(c.Range)) //nolint:gosec // range is validated 1..65535 by ConfigFromEnv
}

// Resolve returns the deterministic port or the next free one above it,
// wrapping around inside the window. taken reports registry or OS conflicts.
func Resolve(key string, c Config, taken func(int) bool) (int, error) {
	start := Deterministic(key, c)
	for i := 0; i < c.Range; i++ {
		p := c.Base + (start-c.Base+i)%c.Range
		if !taken(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port in %d-%d", c.Base, c.Base+c.Range-1)
}
