//go:build !race

package detect_test

// raceBudget scales the timing assertion in TestClassifyCapsScannedInput.
// Without the race detector the budget is the real one.
const raceBudget = 1
