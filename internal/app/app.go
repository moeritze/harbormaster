// Package app wires the dependencies every command needs.
package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/moeritze/harbormaster/internal/gitctx"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/liveness"
	"github.com/moeritze/harbormaster/internal/ports"
	"github.com/moeritze/harbormaster/internal/registry"
)

// ErrUsage marks a configuration error the caller can fix in the environment
// or on the command line. main maps it to the usage exit code instead of the
// registry one.
var ErrUsage = errors.New("usage")

// App carries injected dependencies.
type App struct {
	Stdout    io.Writer
	Stderr    io.Writer
	Stdin     io.Reader
	Now       func() time.Time
	Store     *registry.Store
	Prober    registry.Prober
	Ident     ident.Identity
	Git       gitctx.Context
	Ports     ports.Config
	Cwd       string
	PidOnPort func(port int) (int, string, bool)
	// PidStartTime reports when a pid started (see registry.Entry.StartTime).
	PidStartTime func(pid int) (string, error)
	// Terminate signals a registered process (SIGTERM, then SIGKILL after
	// the timeout; the whole process group when group is true). nil means
	// runner.Terminate. It exists so tests can drive what happens after a
	// signal that fails, and is not typed against runner because runner
	// imports this package.
	Terminate func(pid int, group bool, timeout time.Duration) error
}

// Options tune how New builds the App.
type Options struct {
	// SkipGit leaves Git zero instead of discovering it. gitctx.Discover
	// forks `git` three times, which no hook invocation can afford on the
	// path Claude Code runs before every shell command; the hook core
	// discovers git lazily, only for the rare decision that needs it.
	SkipGit bool
}

// New builds an App from the environment and current directory.
func New(getenv func(string) string, stdout, stderr io.Writer) (*App, error) {
	return NewWithOptions(getenv, stdout, stderr, Options{})
}

// NewWithOptions is New with the knobs in Options.
func NewWithOptions(getenv func(string) string, stdout, stderr io.Writer, o Options) (*App, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	pc, err := ports.ConfigFromEnv(getenv)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUsage, err)
	}
	hostUser := ""
	if u, err := user.Current(); err == nil {
		hostUser = u.Username
	}
	prober := liveness.OS{}
	now := func() time.Time { return time.Now().UTC() }
	store, err := registry.Open(StateDir(getenv), prober, now)
	if err != nil {
		return nil, err
	}
	git := gitctx.Context{}
	if !o.SkipGit {
		git = gitctx.Discover(cwd)
	}
	return &App{
		Stdout: stdout, Stderr: stderr, Stdin: os.Stdin, Now: now,
		Store: store, Prober: prober,
		Ident:        ident.Detect(getenv, hostUser, os.Getppid()),
		Git:          git,
		Ports:        pc,
		Cwd:          cwd,
		PidOnPort:    liveness.PidOnPort,
		PidStartTime: liveness.PidStartTime,
	}, nil
}

// StateDir resolves HARBORMASTER_HOME, then XDG_STATE_HOME, then ~/.local/state.
func StateDir(getenv func(string) string) string {
	if d := getenv("HARBORMASTER_HOME"); d != "" {
		return d
	}
	if x := getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "harbormaster")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "state", "harbormaster")
}

// Clock returns the current time via the injected clock.
func (a *App) Clock() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now().UTC()
}

// WorktreeKey is the hash input for deterministic ports.
func (a *App) WorktreeKey() string {
	if a.Git.Worktree != "" {
		return a.Git.Worktree
	}
	return a.Cwd
}

// MyPort resolves this worktree's port, skipping ports held by others.
func (a *App) MyPort() (int, error) {
	f, err := a.Store.Load()
	if err != nil {
		return 0, fmt.Errorf("registry: %w", err)
	}
	taken := func(p int) bool {
		for _, e := range f.Entries {
			if e.Port == p {
				return !ident.Owns(a.Ident, e, a.Git.Worktree)
			}
		}
		return a.Prober.PortListening(p)
	}
	return ports.Resolve(a.WorktreeKey(), a.Ports, taken)
}
