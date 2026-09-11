package skills

import (
	"strings"
	"testing"
)

// TestCursorRuleQuotesTheDescription: the description is copied out of
// SKILL.md, where "…: …" is ordinary prose and a leading "[" is a bracket.
// Both break an unquoted YAML scalar — the first parses as a mapping, the
// second as a flow sequence — so the .mdc has to quote it.
func TestCursorRuleQuotesTheDescription(t *testing.T) {
	const desc = `[beta] Use when: starting a server, "checking" a port, or stopping one`
	out := string(cursorRule("---\nname: harbormaster\ndescription: " + desc + "\n---\n\n# body\n"))
	want := "description: \"[beta] Use when: starting a server, \\\"checking\\\" a port, or stopping one\"\n"
	if !strings.Contains(out, want) {
		t.Fatalf("description not JSON-quoted:\n%s", out)
	}
	if !strings.HasPrefix(out, "---\ndescription: \"") || !strings.Contains(out, "\nalwaysApply: false\n---\n# body\n") {
		t.Fatalf("bad frontmatter:\n%s", out)
	}
	// A description with no frontmatter at all still produces a valid rule.
	out = string(cursorRule("# just a body\n"))
	if !strings.HasPrefix(out, "---\ndescription: \""+defaultDescription[:10]) {
		t.Fatalf("%s", out)
	}
}
