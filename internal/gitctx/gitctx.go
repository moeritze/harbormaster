// Package gitctx discovers repo, worktree, and branch for a directory.
package gitctx

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/moeritze/harbormaster/internal/tools"
)

// Context describes where a command runs. Empty fields mean "not in git".
type Context struct {
	Repo     string
	Worktree string
	Branch   string
}

// gitTimeout bounds one git invocation. A hung git (a repository on a
// stalled network mount) must not hang every harbormaster command.
const gitTimeout = 3 * time.Second

// Discover never fails; it returns whatever git can tell, in one call.
func Discover(dir string) Context {
	git := tools.Path("git")
	if git == "" {
		return Context{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, "rev-parse", "--show-toplevel", "--git-common-dir", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return Context{}
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 3 || lines[0] == "" {
		return Context{}
	}
	top, common, branch := lines[0], lines[1], lines[2]
	if common != "" && !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	repo := filepath.Dir(filepath.Clean(common))
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
