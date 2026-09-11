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

// TestClosedPortsNeedsTwoFailures pins the rule the prune is built on, with
// the re-probe delay dropped to zero so the test does not sleep.
func TestClosedPortsNeedsTwoFailures(t *testing.T) {
	orig := reprobeDelay
	reprobeDelay = 0
	t.Cleanup(func() { reprobeDelay = orig })

	one := []Entry{{Port: 3000}}
	if closedPorts(&twoProbes{answers: []bool{false, true}}, one)[3000] {
		t.Fatal("one failed probe must not report the port closed")
	}
	if !closedPorts(&twoProbes{answers: []bool{false, false}}, one)[3000] {
		t.Fatal("two failed probes must report the port closed")
	}
	if closedPorts(&twoProbes{answers: []bool{true, false}}, one)[3000] {
		t.Fatal("a listening port must never be reported closed")
	}
	if closedPorts(&twoProbes{answers: []bool{false}}, nil) != nil {
		t.Fatal("no candidates must probe nothing")
	}
}
