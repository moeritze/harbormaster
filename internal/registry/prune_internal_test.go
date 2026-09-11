package registry

import "testing"

type twoProbes struct{ answers []bool }

func (twoProbes) PidAlive(int) bool { return true }

func (t *twoProbes) PortListening(int) bool {
	a := t.answers[0]
	if len(t.answers) > 1 {
		t.answers = t.answers[1:]
	}
	return a
}

// TestPortClosedNeedsTwoFailures pins the helper the prune rule is built on,
// with the re-probe delay dropped to zero so the test does not sleep.
func TestPortClosedNeedsTwoFailures(t *testing.T) {
	orig := reprobeDelay
	reprobeDelay = 0
	t.Cleanup(func() { reprobeDelay = orig })

	if portClosed(&twoProbes{answers: []bool{false, true}}, 3000) {
		t.Fatal("one failed probe must not report the port closed")
	}
	if !portClosed(&twoProbes{answers: []bool{false, false}}, 3000) {
		t.Fatal("two failed probes must report the port closed")
	}
	if portClosed(&twoProbes{answers: []bool{true, false}}, 3000) {
		t.Fatal("a listening port must never be reported closed")
	}
}
