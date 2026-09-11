package hooks

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/detect"
	"github.com/moeritze/harbormaster/internal/gitctx"
	"github.com/moeritze/harbormaster/internal/ident"
	"github.com/moeritze/harbormaster/internal/liveness"
	"github.com/moeritze/harbormaster/internal/registry"
	"github.com/moeritze/harbormaster/internal/render"
	"github.com/moeritze/harbormaster/internal/runner"
)

// defaultKillTimeout is the budget one SessionEnd termination gets. The
// SessionEnd hook itself is given 3 s by the installed spec, and a session
// can own more than one server, so a single stubborn process must not eat
// the whole budget.
const defaultKillTimeout = 700 * time.Millisecond

// Core makes hook decisions. One Core serves one hook invocation.
type Core struct {
	App         *app.App
	Strict      bool
	KillTimeout time.Duration
	Log         io.Writer
}

// New builds a Core from the app; Strict comes from HARBORMASTER_STRICT=1.
func New(a *app.App) *Core {
	return &Core{App: a, Strict: os.Getenv("HARBORMASTER_STRICT") == "1", KillTimeout: defaultKillTimeout}
}

// Handle never panics and never fails closed: any internal error is
// logged and answered with Allow (spec §10, hook input safety).
func (c *Core) Handle(ev Event) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			c.logf("%s: panic: %v", ev.Kind, r)
			res = Result{Decision: Allow}
		}
	}()
	a, err := c.scoped(ev)
	if err != nil {
		c.logf("%s: %v", ev.Kind, err)
		return Result{Decision: Allow}
	}
	var r Result
	switch ev.Kind {
	case SessionStart:
		r, err = c.sessionStart(a)
	case PreShell:
		r, err = c.preShell(a, ev.Command)
	case PostShell:
		r = c.postShell(ev.Command)
	case SessionEnd:
		r, err = c.sessionEnd(a, ev.Reason)
	}
	if err != nil {
		c.logf("%s: %v", ev.Kind, err)
		return Result{Decision: Allow}
	}
	return r
}

// scoped returns a copy of the App bound to the event's identity and cwd.
func (c *Core) scoped(ev Event) (*app.App, error) {
	if c.App == nil || c.App.Store == nil {
		return nil, fmt.Errorf("no registry available")
	}
	a := *c.App
	a.Ident = ident.Identity{Agent: ident.Sanitize(ev.Agent), Session: ident.Sanitize(ev.Session), HostUser: c.App.Ident.HostUser}
	if ev.Cwd != "" {
		a.Cwd = ev.Cwd
		a.Git = gitctx.Discover(ev.Cwd)
	}
	return &a, nil
}

// Logf writes one diagnostic line to the hook's error log (or c.Log, if
// set). Exported so callers outside the package — the `hook` CLI command,
// for payload failures that happen before an Event can even be constructed
// (unreadable stdin, a malformed payload, an unknown agent) — can record
// them the same way Handle records its own internal errors.
func (c *Core) Logf(format string, args ...any) {
	c.logf(format, args...)
}

func (c *Core) logf(format string, args ...any) {
	w := c.Log
	if w == nil {
		if c.App == nil || c.App.Store == nil {
			return
		}
		// gosec G304: path is built from the store's own configured state
		// directory, not from user input.
		f, err := os.OpenFile(filepath.Join(c.App.Store.Dir(), "hook-errors.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // path derives from the trusted state dir, not user input
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func (c *Core) sessionStart(a *app.App) (Result, error) {
	f, err := a.Store.Peek()
	if err != nil {
		return Result{}, err
	}
	return Result{Decision: Allow, Context: sessionContext(a, f.Entries)}, nil
}

func (c *Core) preShell(a *app.App, cmd string) (Result, error) {
	d := detect.Classify(cmd)
	if d.Class == detect.None && len(d.Ports) == 0 {
		return Result{Decision: Allow}, nil
	}
	f, err := a.Store.Peek()
	if err != nil {
		return Result{}, err
	}
	foreign := func(e registry.Entry) bool { return !ident.Owns(a.Ident, e, a.Git.Worktree) }
	switch d.Class {
	case detect.Kill:
		for _, e := range f.Entries {
			for _, p := range d.Ports {
				if e.Port == p && foreign(e) {
					return Result{Decision: Deny, Reason: denyKill(e, a.Clock())}, nil
				}
			}
			for _, pid := range d.Pids {
				if e.PID == pid && foreign(e) {
					return Result{Decision: Deny, Reason: denyKill(e, a.Clock())}, nil
				}
			}
		}
		if len(d.Ports) == 0 && len(d.Pids) == 0 {
			// A kill with no port and no pid in its text ("pkill -f node")
			// may or may not hit somebody else's server: harbormaster
			// cannot tell, so it asks the human instead of deciding.
			if others := foreignEntries(a, f.Entries); len(others) > 0 {
				return Result{Decision: Ask, Reason: blindKillContext(others, a.Clock())}, nil
			}
		}
		return Result{Decision: Allow}, nil
	case detect.ServerStart:
		for _, e := range f.Entries {
			for _, p := range d.Ports {
				if e.Port == p && foreign(e) {
					return Result{Decision: Deny, Reason: denyPort(e, a)}, nil
				}
			}
		}
		if d.Wrapped {
			return Result{Decision: Allow}, nil
		}
		if c.Strict {
			return Result{Decision: Deny, Reason: strictReason(a)}, nil
		}
		return Result{Decision: Allow, Context: wrapNudge(a)}, nil
	}
	return Result{Decision: Allow}, nil
}

func (c *Core) postShell(cmd string) Result {
	d := detect.Classify(cmd)
	if d.Class == detect.ServerStart && !d.Wrapped {
		return Result{Decision: Allow, Context: postStartNudge()}
	}
	return Result{Decision: Allow}
}

// sessionEnd releases and terminates everything this session registered,
// within the SessionEnd hook budget. The registry is updated first, in one
// locked pass, so the entries are gone even if a termination then hangs:
// Claude Code kills a hook that overruns its timeout, and a half-released
// registry is worse than a surviving process (which the next prune reaps).
//
// "clear" and "resume" are not the end of anything -- the same session keeps
// running under a new or restored transcript -- so they release nothing.
func (c *Core) sessionEnd(a *app.App, reason string) (Result, error) {
	if a.Ident.Session == "" {
		return Result{Decision: Allow}, nil
	}
	switch reason {
	case "clear", "resume":
		return Result{Decision: Allow}, nil
	}
	var removed []registry.Entry
	if err := a.Store.Update(func(f *registry.File) error {
		kept := make([]registry.Entry, 0, len(f.Entries))
		for _, e := range f.Entries {
			if e.Session == a.Ident.Session {
				removed = append(removed, e)
				continue
			}
			kept = append(kept, e)
		}
		f.Entries = kept
		return nil
	}); err != nil {
		return Result{}, err
	}
	for _, e := range removed {
		if err := a.Store.AppendHistory(registry.HistoryRecord{Entry: e, Reason: "session_end", At: a.Clock()}); err != nil {
			c.logf("session_end: history for port %d: %v", e.Port, err)
		}
	}
	for _, e := range removed {
		if err := runner.Guard(e.PID, liveness.PidUID); err != nil {
			continue
		}
		if err := runner.Terminate(e.PID, e.Spawned, c.KillTimeout); err != nil {
			c.logf("session_end: terminate pid %d (port %d): %v", e.PID, e.Port, err)
		}
	}
	return Result{Decision: Allow}, nil
}

func foreignEntries(a *app.App, es []registry.Entry) []registry.Entry {
	var out []registry.Entry
	for _, e := range es {
		if !ident.Owns(a.Ident, e, a.Git.Worktree) {
			out = append(out, e)
		}
	}
	return out
}

func denyKill(e registry.Entry, now time.Time) string {
	return fmt.Sprintf("harbormaster: port %d / pid %d belongs to %s. Do not kill it. Use `harbormaster ls` to see what runs, and `harbormaster kill <port>` only for ports this session owns.", e.Port, e.PID, render.OwnerLine(e, now))
}

func denyPort(e registry.Entry, a *app.App) string {
	return fmt.Sprintf("harbormaster: port %d is in use by %s. Start this server on this worktree's port instead: `harbormaster run --label \"<task>\" -- <command>` (port %s).", e.Port, render.OwnerLine(e, a.Clock()), myPort(a))
}

func strictReason(a *app.App) string {
	return fmt.Sprintf("harbormaster: dev servers must be started through the wrapper so other sessions can see them: `harbormaster run --label \"<task>\" -- <command>` (this worktree's port: %s).", myPort(a))
}

func myPort(a *app.App) string {
	p, err := a.MyPort()
	if err != nil {
		return "unknown"
	}
	return fmt.Sprint(p)
}
