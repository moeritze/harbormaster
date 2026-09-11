package tools_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/moeritze/harbormaster/internal/tools"
)

func TestPathResolvesAbsoluteAndCaches(t *testing.T) {
	ps := tools.Path("ps")
	if ps == "" || !filepath.IsAbs(ps) {
		t.Fatalf("ps should resolve to an absolute path, got %q", ps)
	}
	if again := tools.Path("ps"); again != ps {
		t.Fatalf("cached result changed: %q vs %q", ps, again)
	}
	if got := tools.Path("definitely-not-a-real-tool-xyz"); got != "" {
		t.Fatalf("unknown tool must resolve to empty, got %q", got)
	}
}

// TestPathRefusesRelativePathEntry pins the $PATH fallback's absolute-only
// rule. With GODEBUG=execerrdot=0 exec.LookPath happily returns a path
// relative to the current directory, which would let whoever chooses that
// directory supply the `ps` harbormaster asks who owns a pid.
func TestPathRefusesRelativePathEntry(t *testing.T) {
	t.Setenv("GODEBUG", "execerrdot=0")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	const name = "hm-relative-path-probe"
	if err := os.WriteFile(filepath.Join(dir, "bin", name), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { //nolint:gosec // an executable bit is the point of this test
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("PATH", "bin")

	// Sanity check: the standard library really does hand out the relative
	// path here, so the assertion below is about tools.Path, not about
	// LookPath happening to fail.
	if p, err := exec.LookPath(name); err != nil || filepath.IsAbs(p) {
		t.Skipf("LookPath did not return a relative hit (%q, %v); nothing to guard against here", p, err)
	}
	if got := tools.Path(name); got != "" {
		t.Fatalf("a relative $PATH hit must be refused, got %q", got)
	}
}
