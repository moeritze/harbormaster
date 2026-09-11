// Package tools resolves the external programs harbormaster shells out to
// (ps, lsof, git) to fixed absolute paths, once per process. Resolving
// through the caller's $PATH would let a shimmed `ps` decide whose pid a
// process is, which is the basis of the never-signal-another-user's-process
// guarantee.
package tools

import (
	"os"
	"os/exec"
	"sync"
)

// candidates lists trusted locations per tool, in order. $PATH is only
// consulted when none of them exists (an unusual layout, Nix, BSDs).
var candidates = map[string][]string{
	"ps":   {"/bin/ps", "/usr/bin/ps"},
	"lsof": {"/usr/sbin/lsof", "/usr/bin/lsof", "/opt/homebrew/bin/lsof"},
	"git":  {"/usr/bin/git", "/opt/homebrew/bin/git", "/usr/local/bin/git"},
}

var (
	mu       sync.Mutex
	resolved = map[string]string{}
)

// Path returns the absolute path for name, or "" when it cannot be found.
// The result is cached for the life of the process.
func Path(name string) string {
	mu.Lock()
	defer mu.Unlock()
	if p, ok := resolved[name]; ok {
		return p
	}
	p := resolve(name)
	resolved[name] = p
	return p
}

func resolve(name string) string {
	for _, c := range candidates[name] {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return c
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}
