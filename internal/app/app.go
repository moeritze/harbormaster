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
	Now       func() time.Time
	Store     *registry.Store
	Prober    registry.Prober
	Ident     ident.Identity
	Git       gitctx.Context
	Ports     ports.Config
	Cwd       string
	PidOnPort func(port int) (int, string, bool)
}

// New builds an App from the environment and current directory.
func New(getenv func(string) string, stdout, stderr io.Writer) (*App, error) {
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
	return &App{
		Stdout: stdout, Stderr: stderr, Now: now,
		Store: store, Prober: prober,
		Ident:     ident.Detect(getenv, hostUser),
		Git:       gitctx.Discover(cwd),
		Ports:     pc,
		Cwd:       cwd,
		PidOnPort: liveness.PidOnPort,
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
