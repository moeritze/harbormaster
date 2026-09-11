// Package gitctx discovers repo, worktree, and branch for a directory.
package gitctx

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// Context describes where a command runs. Empty fields mean "not in git".
type Context struct {
	Repo     string
	Worktree string
	Branch   string
}

// Discover never fails; it returns whatever git can tell.
func Discover(dir string) Context {
	top := run(dir, "rev-parse", "--show-toplevel")
	if top == "" {
		return Context{}
	}
	common := run(dir, "rev-parse", "--git-common-dir")
	if common != "" && !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	repo := filepath.Dir(filepath.Clean(common))
	branch := run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "HEAD" {
		branch = ""
	}
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	if w, err := filepath.EvalSymlinks(top); err == nil {
		top = w
	}
	return Context{Repo: repo, Worktree: top, Branch: branch}
}

func run(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
