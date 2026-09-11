package ports_test

import (
	"testing"

	"github.com/moeritze/harbormaster/internal/ports"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigDefaultsAndOverrides(t *testing.T) {
	c, err := ports.ConfigFromEnv(env(nil))
	if err != nil || c.Base != 3000 || c.Range != 1000 {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_BASE": "8000", "HARBORMASTER_RANGE": "10"}))
	if err != nil || c.Base != 8000 || c.Range != 10 {
		t.Fatalf("override: %+v %v", c, err)
	}
	if _, err := ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_BASE": "abc"})); err == nil {
		t.Fatal("expected error on non-numeric base")
	}
	if _, err := ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_RANGE": "0"})); err == nil {
		t.Fatal("expected error on zero range")
	}
	if _, err := ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_RANGE": "9223372036854775807"})); err == nil {
		t.Fatal("expected error on range exceeding 65535")
	}
	if _, err := ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_BASE": "65000", "HARBORMASTER_RANGE": "1000"})); err == nil {
		t.Fatal("expected error on window exceeding 65535")
	}
	if _, err := ports.ConfigFromEnv(env(map[string]string{"HARBORMASTER_BASE": "70000"})); err == nil {
		t.Fatal("expected error on base exceeding 65535")
	}
}

func TestDeterministicStableAndInRange(t *testing.T) {
	c := ports.Config{Base: 3000, Range: 1000}
	a := ports.Deterministic("/Users/m/repo-wt/auth", c)
	b := ports.Deterministic("/Users/m/repo-wt/auth", c)
	if a != b {
		t.Fatalf("not stable: %d %d", a, b)
	}
	if a < 3000 || a >= 4000 {
		t.Fatalf("out of range: %d", a)
	}
	if ports.Deterministic("/Users/m/repo-wt/other", c) == a {
		t.Log("collision between two keys is allowed but unlikely; check hash if this repeats")
	}
}

func TestResolveProbesUpwardWithinRange(t *testing.T) {
	c := ports.Config{Base: 3000, Range: 5}
	want := ports.Deterministic("k", c)
	// Mark every port from want through the end of the window as taken to force wraparound
	taken := make(map[int]bool)
	for p := want; p < c.Base+c.Range; p++ {
		taken[p] = true
	}
	got, err := ports.Resolve("k", c, func(p int) bool { return taken[p] })
	if err != nil {
		t.Fatal(err)
	}
	if got < c.Base || got >= want {
		t.Fatalf("got %d (deterministic %d), expected wraparound to [%d, %d)", got, want, c.Base, want)
	}
	all := func(int) bool { return true }
	if _, err := ports.Resolve("k", c, all); err == nil {
		t.Fatal("expected exhaustion error")
	}
}

func TestZeroRangeGuards(t *testing.T) {
	c := ports.Config{Base: 3000}
	if p := ports.Deterministic("x", c); p != 3000 {
		t.Fatalf("Deterministic with zero range: expected 3000, got %d", p)
	}
	if _, err := ports.Resolve("x", c, func(int) bool { return false }); err == nil {
		t.Fatal("expected error on zero range in Resolve")
	}
}
