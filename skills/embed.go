// Package skills embeds the canonical agent skill so `harbormaster install`
// can write it without a source checkout.
package skills

import _ "embed"

// Harbormaster is skills/harbormaster/SKILL.md.
//
//go:embed harbormaster/SKILL.md
var Harbormaster []byte
