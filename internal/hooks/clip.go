package hooks

import "unicode/utf8"

// MaxMessage caps the text an adapter hands the agent in one field. A reason
// is built from registry rows, so a machine with many registered servers
// could otherwise push kilobytes into a permission prompt — where the host
// may truncate it at an arbitrary byte, or refuse the payload outright.
const MaxMessage = 2 << 10

// Clip cuts s to at most n bytes, on a rune boundary, marking the cut with
// an ellipsis. n below the ellipsis's own width yields the empty string.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const ell = "…"
	cut := n - len(ell)
	if cut <= 0 {
		return ""
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ell
}
