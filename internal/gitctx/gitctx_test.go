package gitctx_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/moeritze/harbormaster/internal/gitctx"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiscoverOutsideGit(t *testing.T) {
	c := gitctx.Discover(t.TempDir())
	if c != (gitctx.Context{}) {
		t.Fatalf("expected empty, got %+v", c)
	}
}

func TestDiscoverRepoAndWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	root := t.TempDir()
	main := filepath.Join(root, "main")
	git(t, root, "init", "-q", "-b", "main", main)
	git(t, main, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(root, "wt-feature")
	git(t, main, "worktree", "add", "-q", "-b", "feature", wt)

	c := gitctx.Discover(wt)
	if filepath.Base(c.Repo) != "main" {
		t.Fatalf("repo %q", c.Repo)
	}
	if filepath.Base(c.Worktree) != "wt-feature" {
		t.Fatalf("worktree %q", c.Worktree)
	}
	if c.Branch != "feature" {
		t.Fatalf("branch %q", c.Branch)
	}
	if !filepath.IsAbs(c.Repo) {
		t.Fatalf("repo not absolute: %q", c.Repo)
	}
	if !filepath.IsAbs(c.Worktree) {
		t.Fatalf("worktree not absolute: %q", c.Worktree)
	}

	cm := gitctx.Discover(main)
	if filepath.Base(cm.Repo) != "main" {
		t.Fatalf("main repo %q", cm.Repo)
	}
	if filepath.Base(cm.Worktree) != "main" {
		t.Fatalf("main worktree %q", cm.Worktree)
	}
	if cm.Branch != "main" {
		t.Fatalf("main branch %q", cm.Branch)
	}
	if !filepath.IsAbs(cm.Repo) {
		t.Fatalf("main repo not absolute: %q", cm.Repo)
	}
	if !filepath.IsAbs(cm.Worktree) {
		t.Fatalf("main worktree not absolute: %q", cm.Worktree)
	}
}
