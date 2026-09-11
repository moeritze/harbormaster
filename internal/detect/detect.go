// Package detect classifies shell commands an agent is about to run:
// does it kill something, does it start a dev server, which ports and
// pids does it mention. It is a heuristic by design (spec §8); a miss
// degrades to awareness-only, never to a wrong deny.
package detect

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Class is the coarse category of a command.
type Class int

// Class values.
const (
	None Class = iota
	Kill
	ServerStart
)

func (c Class) String() string {
	switch c {
	case Kill:
		return "kill"
	case ServerStart:
		return "server_start"
	default:
		return "none"
	}
}

// Result is what Classify learned about a command.
type Result struct {
	Class   Class
	Ports   []int
	Pids    []int
	Wrapped bool
	Matched string
}

type pattern struct {
	name string
	re   *regexp.Regexp
}

var (
	wrappedRe = regexp.MustCompile(`(?:^|[\s;&|(])(?:harbormaster|hm)\s+run\b`)

	// notKillRe excludes harbormaster's own "kill"-named subcommands
	// (e.g. "hm kill 3000") from the process-kill classification: they
	// target a registry entry by port, not an arbitrary process.
	notKillRe = regexp.MustCompile(`(?:^|[\s;&|(])(?:harbormaster|hm)\s+(?:kill|check|claim|release|ls)\b`)

	killPatterns = []pattern{
		// A leading "-9"/"-TERM" right after kill is always a signal
		// spec, never a pid, so it never satisfies the target group on
		// its own (that's what stops "xargs kill -9" with no literal
		// pid from being misread as pid 9). A literal "--" switches to
		// the process-group form, where a negative number is the target.
		{"kill", regexp.MustCompile(`(?:^|[\s;&|($])kill\s+(?:(?:-[A-Za-z0-9]+\s+)*\d+(?:\s+\d+)*|--\s+-?\d+(?:\s+-?\d+)*)`)},
		{"pkill", regexp.MustCompile(`(?:^|[\s;&|(])pkill\b`)},
		{"killall", regexp.MustCompile(`(?:^|[\s;&|(])killall\b`)},
		{"fuser-k", regexp.MustCompile(`(?:^|[\s;&|(])fuser\s+-k\b`)},
		{"lsof-t", regexp.MustCompile(`(?:^|[\s;&|($])lsof\s+(?:-\w+\s+)*-t\w*\s*(?:-i)?\s*(?:tcp)?:?\s*\d+`)},
		{"kill-port", regexp.MustCompile(`(?:^|[\s;&|(])(?:npx\s+)?kill-port\b`)},
	}

	serverPatterns = []pattern{
		{"pkg-script", regexp.MustCompile(`(?:^|[\s;&|(])(?:npm|pnpm|yarn|bun)\s+(?:run\s+)?(?:dev|start|serve|preview)\b`)},
		{"next", regexp.MustCompile(`(?:^|[\s;&|(])(?:npx\s+)?next\s+dev\b`)},
		{"vite", regexp.MustCompile(`(?:^|[\s;&|(])(?:npx\s+)?vite(?:\s+(?:preview|dev|serve))?(?:\s|$)`)},
		{"astro", regexp.MustCompile(`(?:^|[\s;&|(])(?:npx\s+)?astro\s+dev\b`)},
		{"nuxt", regexp.MustCompile(`(?:^|[\s;&|(])(?:npx\s+)?nuxt\s+dev\b`)},
		{"remix", regexp.MustCompile(`(?:^|[\s;&|(])(?:npx\s+)?remix\s+dev\b`)},
		{"ng", regexp.MustCompile(`(?:^|[\s;&|(])ng\s+serve\b`)},
		{"http.server", regexp.MustCompile(`(?:^|[\s;&|(])python3?\s+-m\s+http\.server\b`)},
		{"uvicorn", regexp.MustCompile(`(?:^|[\s;&|(])uvicorn\b`)},
		{"flask", regexp.MustCompile(`(?:^|[\s;&|(])flask\s+run\b`)},
		{"rails", regexp.MustCompile(`(?:^|[\s;&|(])(?:bundle\s+exec\s+)?rails\s+s(?:erver)?\b`)},
		{"artisan", regexp.MustCompile(`(?:^|[\s;&|(])php\s+artisan\s+serve\b`)},
		{"hugo", regexp.MustCompile(`(?:^|[\s;&|(])hugo\s+serve(?:r)?\b`)},
		{"jekyll", regexp.MustCompile(`(?:^|[\s;&|(])jekyll\s+serve\b`)},
		{"go-run-port", regexp.MustCompile(`(?:^|[\s;&|(])go\s+run\b.*--port\b`)},
		{"cargo-run-port", regexp.MustCompile(`(?:^|[\s;&|(])cargo\s+run\b.*--port\b`)},
	}

	// Explicit build/test invocations that would otherwise match "vite".
	notServerRe = regexp.MustCompile(`(?:^|[\s;&|(])vite\s+build\b`)

	portPatterns = []*regexp.Regexp{
		regexp.MustCompile(`--port[=\s]+(\d{2,5})\b`),
		regexp.MustCompile(`(?:^|\s)-p\s*(\d{2,5})\b`),
		regexp.MustCompile(`\bPORT=(\d{2,5})\b`),
		regexp.MustCompile(`\bhttp\.server\s+(\d{2,5})\b`),
		regexp.MustCompile(`(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]):(\d{2,5})\b`),
		regexp.MustCompile(`\b(?:lsof\s+(?:-\w+\s+)*-t\w*\s*(?:-i)?\s*(?:tcp)?:?\s*|fuser\s+-k\s+)(\d{2,5})\b`),
		regexp.MustCompile(`\bkill-port\s+((?:\d{2,5}\s*)+)`),
		regexp.MustCompile(`\b(?:hm|harbormaster)\s+(?:kill|check|claim|release)\s+(\d{2,5})\b`),
	}

	// killPidRe mirrors the "kill" classify pattern's two forms: plain
	// pids (group 1, no leading dash, since a leading "-9" is a signal)
	// or, after a literal "--", process-group targets (group 2, dash allowed).
	killPidRe = regexp.MustCompile(`(?:^|[\s;&|(])kill\s+(?:(?:-[A-Za-z0-9]+\s+)*(\d+(?:\s+\d+)*)|--\s+(-?\d+(?:\s+-?\d+)*))`)
)

// Classify inspects one shell command line.
func Classify(cmd string) Result {
	r := Result{}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return r
	}
	r.Wrapped = wrappedRe.MatchString(cmd)
	r.Ports = ports(cmd)

	if !notKillRe.MatchString(cmd) {
		for _, p := range killPatterns {
			if p.re.MatchString(cmd) {
				r.Class, r.Matched = Kill, p.name
				r.Pids = pids(cmd)
				return r
			}
		}
	}
	if !notServerRe.MatchString(cmd) {
		for _, p := range serverPatterns {
			if p.re.MatchString(cmd) {
				r.Class, r.Matched = ServerStart, p.name
				return r
			}
		}
	}
	return r
}

func ports(cmd string) []int {
	seen := map[int]bool{}
	var out []int
	add := func(s string) {
		for _, f := range strings.Fields(s) {
			n, err := strconv.Atoi(f)
			if err != nil || n < 1 || n > 65535 || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, re := range portPatterns {
		for _, m := range re.FindAllStringSubmatch(cmd, -1) {
			add(m[1])
		}
	}
	sort.Ints(out)
	return out
}

func pids(cmd string) []int {
	seen := map[int]bool{}
	var out []int
	add := func(s string) {
		for _, f := range strings.Fields(s) {
			f = strings.TrimPrefix(f, "-")
			n, err := strconv.Atoi(f)
			if err != nil || n <= 0 || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, m := range killPidRe.FindAllStringSubmatch(cmd, -1) {
		add(m[1])
		add(m[2])
	}
	sort.Ints(out)
	return out
}
