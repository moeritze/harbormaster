package tools_test

import (
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
