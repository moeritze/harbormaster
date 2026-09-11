package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/ident"
)

// installApp is the minimum an install run needs: install touches no
// registry, only stdout/stderr and the filesystem. Only stderr is returned
// because that is where the warning under test lands.
func installApp() (*app.App, *bytes.Buffer) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return &app.App{
		Stdout: out, Stderr: errOut,
		Now:   func() time.Time { return time.Unix(1700000000, 0) },
		Ident: ident.Identity{Agent: "claude", Session: "s1"},
		Cwd:   ".",
	}, errOut
}

func runInstall(t *testing.T, a *app.App, args ...string) {
	t.Helper()
	root := NewRoot(a)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

// TestProjectInstallWarnsWhenTheBareNameIsNotOnAGUIPath: Claude Code and
// Cursor are usually launched from the Dock or Spotlight, where no login
// shell has run and ~/.local/bin is not on PATH. A --project install writes
// the bare name, so hooks pointing at a binary only a login shell can find
// would silently never fire.
func TestProjectInstallWarnsWhenTheBareNameIsNotOnAGUIPath(t *testing.T) {
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	var probed []string
	lookPath = func(p string) (string, error) {
		probed = append(probed, p)
		return "", errors.New("not found")
	}
	for _, agent := range []string{"claude", "cursor"} {
		a, errOut := installApp()
		runInstall(t, a, "install", agent, "--project", t.TempDir())
		if !strings.Contains(errOut.String(), commandPathWarning) {
			t.Fatalf("%s: no warning: %q", agent, errOut.String())
		}
	}
	want := filepath.Join("/opt/homebrew/bin", "harbormaster")
	if len(probed) == 0 || probed[len(probed)-1] != want {
		t.Fatalf("must probe the GUI PATH dirs, probed %v", probed)
	}
}

// TestProjectInstallIsQuietWhenTheBinaryIsFound is the other half.
func TestProjectInstallIsQuietWhenTheBinaryIsFound(t *testing.T) {
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	lookPath = func(p string) (string, error) { return p, nil }
	for _, agent := range []string{"claude", "cursor"} {
		a, errOut := installApp()
		runInstall(t, a, "install", agent, "--project", t.TempDir())
		if strings.Contains(errOut.String(), "GUI app's default PATH") {
			t.Fatalf("%s: unexpected warning: %q", agent, errOut.String())
		}
	}
}

// TestNoPathWarningOutsideProjectInstalls: a user-level install writes this
// binary's absolute path, an uninstall writes no command at all, and a
// dry-run wrote nothing to warn about.
func TestNoPathWarningOutsideProjectInstalls(t *testing.T) {
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	dir := t.TempDir()
	cases := [][]string{
		{"install", "claude", "--project", dir, "--dry-run"},
		{"install", "claude", "--project", dir, "--command", "/opt/hm/harbormaster"},
		{"uninstall", "claude", "--project", dir},
		{"install", "agents-md", "--project", dir},
	}
	for _, args := range cases {
		a, errOut := installApp()
		runInstall(t, a, args...)
		if strings.Contains(errOut.String(), "GUI app's default PATH") {
			t.Fatalf("%v: unexpected warning: %q", args, errOut.String())
		}
	}
	// A user-level install is out of scope here: it would write into the
	// real home directory.
	if _, err := os.Stat(filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}
}
