//go:build !race

package registry_test

import "time"

// pruneBudget is the wall-clock budget for the 50-stale-entry prune test:
// one shared 100 ms re-probe delay plus a few concurrent dials.
const pruneBudget = 500 * time.Millisecond
