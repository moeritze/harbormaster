//go:build race

package registry_test

import "time"

// pruneBudget is the wall-clock budget for the 50-stale-entry prune test.
// The race detector slows goroutine-heavy code several-fold, so the budget
// is widened there; the batching itself is still asserted by probe counts.
const pruneBudget = 2 * time.Second
