package skills_test

import (
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/skills"
)

func TestEmbeddedSkillHasFrontmatter(t *testing.T) {
	s := string(skills.Harbormaster)
	if !strings.HasPrefix(s, "---\nname: harbormaster\n") || !strings.Contains(s, "\ndescription:") || !strings.Contains(s, "harbormaster run") {
		t.Fatalf("unexpected skill content:\n%s", s[:min(len(s), 200)])
	}
	if strings.Count(s, "\n") > 120 {
		t.Fatal("skill must stay short (< 120 lines)")
	}
}

func TestCursorRuleHasMdcFrontmatter(t *testing.T) {
	r := string(skills.CursorRule())
	if !strings.HasPrefix(r, "---\ndescription: ") || !strings.Contains(r, "\nalwaysApply: false\n---\n") {
		t.Fatalf("bad frontmatter:\n%s", r[:min(len(r), 200)])
	}
	if strings.Contains(r, "name: harbormaster") || !strings.Contains(r, "harbormaster run") {
		t.Fatalf("body/frontmatter mix-up:\n%s", r[:min(len(r), 300)])
	}
}
