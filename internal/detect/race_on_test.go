//go:build race

package detect_test

// raceBudget scales the timing assertion in TestClassifyCapsScannedInput.
// The race detector instruments every memory access and slows the regexp
// engine by more than an order of magnitude, so a `go test -race` run
// measures the detector, not the cap. The test still runs there -- it is
// the correctness half (the 100 KB command must classify as a server
// start) that matters under -race -- only its timing budget is relaxed.
const raceBudget = 40
