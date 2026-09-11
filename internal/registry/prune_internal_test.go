package registry

import (
	"testing"
	"time"
)

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

	// A deadline far in the future: the budget is exercised separately in
	// TestPruneStopsAtTheProbeBudget.
	never := time.Now().Add(time.Hour)
	one := []Entry{{Port: 3000}}
	if closedPorts(&twoProbes{answers: []bool{false, true}}, one, never)[3000] {
		t.Fatal("one failed probe must not report the port closed")
	}
	if !closedPorts(&twoProbes{answers: []bool{false, false}}, one, never)[3000] {
		t.Fatal("two failed probes must report the port closed")
	}
	if closedPorts(&twoProbes{answers: []bool{true, false}}, one, never)[3000] {
		t.Fatal("a listening port must never be reported closed")
	}
	if closedPorts(&twoProbes{answers: []bool{false}}, nil, never) != nil {
		t.Fatal("no candidates must probe nothing")
	}
}
