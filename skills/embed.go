// Package skills embeds the canonical agent skill so `harbormaster install`
// can write it without a source checkout.
package skills

import (
	_ "embed"
	"strings"
)

// Harbormaster is skills/harbormaster/SKILL.md.
//
//go:embed harbormaster/SKILL.md
var Harbormaster []byte

// CursorRule renders the skill as a Cursor project rule
// (.cursor/rules/harbormaster.mdc): the SKILL.md frontmatter becomes the
// rule's description with alwaysApply false, the body is kept verbatim.
func CursorRule() []byte {
	body := string(Harbormaster)
	desc := "Local dev-server registry: how to start, check and stop dev servers without killing other sessions' ports."
	if i := strings.Index(body, "\n---\n"); strings.HasPrefix(body, "---\n") && i > 0 {
		front := body[4:i]
		body = body[i+5:]
		for _, line := range strings.Split(front, "\n") {
			if v, ok := strings.CutPrefix(line, "description:"); ok {
				desc = strings.TrimSpace(v)
			}
		}
	}
	return []byte("---\ndescription: " + desc + "\nalwaysApply: false\n---\n" + strings.TrimLeft(body, "\n"))
}
