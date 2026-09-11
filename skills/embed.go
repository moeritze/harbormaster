// Package skills embeds the canonical agent skill so `harbormaster install`
// can write it without a source checkout.
package skills

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// Harbormaster is skills/harbormaster/SKILL.md.
//
//go:embed harbormaster/SKILL.md
var Harbormaster []byte

// defaultDescription is used when SKILL.md carries no frontmatter.
const defaultDescription = "Local dev-server registry: how to start, check and stop dev servers without killing other sessions' ports."

// CursorRule renders the skill as a Cursor project rule
// (.cursor/rules/harbormaster.mdc): the SKILL.md frontmatter becomes the
// rule's description with alwaysApply false, the body is kept verbatim.
func CursorRule() []byte { return cursorRule(string(Harbormaster)) }

func cursorRule(body string) []byte {
	desc := defaultDescription
	if i := strings.Index(body, "\n---\n"); strings.HasPrefix(body, "---\n") && i > 0 {
		front := body[4:i]
		body = body[i+5:]
		for _, line := range strings.Split(front, "\n") {
			if v, ok := strings.CutPrefix(line, "description:"); ok {
				desc = strings.TrimSpace(v)
			}
		}
	}
	// The description is copied out of SKILL.md, where a colon-space or a
	// leading "[" is ordinary prose but would make the .mdc frontmatter
	// parse as a YAML mapping or flow sequence — or fail outright. A JSON
	// string is a valid YAML double-quoted scalar, so quoting it this way
	// keeps any description intact.
	q, err := json.Marshal(desc)
	if err != nil {
		q = []byte(`""`)
	}
	return []byte("---\ndescription: " + string(q) + "\nalwaysApply: false\n---\n" + strings.TrimLeft(body, "\n"))
}
