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

// overrideHint is appended to every deny and ask reason so the agent (and
// the human reading it) can always find the escape hatch. overrideNote
// prefixes the context those same reasons become once it is active.
const (
	overrideHint = " (override: set HARBORMASTER_HOOKS=0 in Claude Code's environment)"
	overrideNote = "harbormaster (override active): "
)

// Core makes hook decisions. One Core serves one hook invocation.
type Core struct {
	App    *app.App
	Strict bool
	// Disabled is the HARBORMASTER_HOOKS=0 escape hatch: every deny and ask
	// degrades to an allow that carries the reason as context, so a wedged
	// heuristic can never stop a session from working.
	Disabled    bool
	KillTimeout time.Duration
	Log         io.Writer
	// GitDiscover resolves a directory's git context. It is a field so the
	// hot path can be proven not to fork git (see scope.git).
	GitDiscover func(string) gitctx.Context
}

// New builds a Core from the app. Strict comes from HARBORMASTER_STRICT=1,
// Disabled from HARBORMASTER_HOOKS=0.
func New(a *app.App) *Core {
	return &Core{
		App:         a,
		Strict:      os.Getenv("HARBORMASTER_STRICT") == "1",
		Disabled:    os.Getenv("HARBORMASTER_HOOKS") == "0",
		KillTimeout: defaultKillTimeout,
		GitDiscover: gitctx.Discover,
	}
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
	s, err := c.scoped(ev)
	if err != nil {
		c.logf("%s: %v", ev.Kind, err)
		return Result{Decision: Allow}
	}
	var r Result
	switch ev.Kind {
	case SessionStart:
		r, err = c.sessionStart(s)
	case PreShell:
		r, err = c.preShell(s, ev.Command)
	case PostShell:
		r = c.postShell(ev.Command)
	case SessionEnd:
		r, err = c.sessionEnd(s, ev.Reason)
	}
	if err != nil {
		c.logf("%s: %v", ev.Kind, err)
		return Result{Decision: Allow}
	}
	return c.override(r)
}

// override applies the escape hatch and the hint that advertises it.
func (c *Core) override(r Result) Result {
	if r.Decision != Deny && r.Decision != Ask {
		return r
	}
	if c.Disabled {
		return Result{Decision: Allow, Context: overrideNote + r.Reason}
	}
	r.Reason += overrideHint
	return r
}

// scope is one hook event's view of the world: the App bound to the event's
// identity and working directory, plus git discovery that happens only if
// something actually asks for it. gitctx.Discover forks `git` three times,
// which is far too much to spend on every shell command a session runs, and
// nothing on the hot path needs it (see scope.git).
type scope struct {
	app      *app.App
	core     *Core
	cwd      string
	resolved bool
}

// git resolves this event's git context, at most once per event. Only three
// callers need it: the ownership fallback for an event with no session id
// (hook payloads always carry one, so this is rare), myPort, and the
// session-start table.
func (s *scope) git() gitctx.Context {
	if !s.resolved {
		s.resolved = true
		if s.cwd != "" && s.core.GitDiscover != nil {
			s.app.Git = s.core.GitDiscover(s.cwd)
		}
	}
	return s.app.Git
}

// owns answers ownership without forking git whenever it can: an identity
// that carries a session id is matched by session alone.
func (s *scope) owns(e registry.Entry) bool {
	if s.app.Ident.Session != "" {
		return ident.Owns(s.app.Ident, e, "")
	}
	return ident.Owns(s.app.Ident, e, s.git().Worktree)
}

// scoped returns a scope over a copy of the App bound to the event's
// identity and cwd. It deliberately does not discover git.
func (c *Core) scoped(ev Event) (*scope, error) {
	if c.App == nil || c.App.Store == nil {
		return nil, fmt.Errorf("no registry available")
	}
	a := *c.App
	a.Ident = ident.Identity{Agent: ident.Sanitize(ev.Agent), Session: ident.Sanitize(ev.Session), HostUser: c.App.Ident.HostUser}
	if ev.Cwd != "" {
		a.Cwd = ev.Cwd
	}
	return &scope{app: &a, core: c, cwd: ev.Cwd}, nil
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

func (c *Core) sessionStart(s *scope) (Result, error) {
	f, err := s.app.Store.Peek()
	if err != nil {
		return Result{}, err
	}
	return Result{Decision: Allow, Context: sessionContext(s, f.Entries)}, nil
}

func (c *Core) preShell(s *scope, cmd string) (Result, error) {
	d := detect.Classify(cmd)
	if d.Class == detect.None && len(d.Ports) == 0 {
		return Result{Decision: Allow}, nil
	}
	f, err := s.app.Store.Peek()
	if err != nil {
		return Result{}, err
	}
	foreign := func(e registry.Entry) bool { return !s.owns(e) }
	switch d.Class {
	case detect.Kill:
		for _, e := range f.Entries {
			for _, p := range d.Ports {
				if e.Port == p && foreign(e) {
					return Result{Decision: Deny, Reason: denyKill(e, s.app.Clock())}, nil
				}
			}
			for _, pid := range d.Pids {
				if e.PID == pid && foreign(e) {
					return Result{Decision: Deny, Reason: denyKill(e, s.app.Clock())}, nil
				}
			}
		}
		if len(d.Ports) == 0 && len(d.Pids) == 0 {
			// A kill with no port and no pid in its text ("pkill -f node")
			// may or may not hit somebody else's server: harbormaster
			// cannot tell, so it asks the human instead of deciding.
			if others := foreignEntries(s, f.Entries); len(others) > 0 {
				return Result{Decision: Ask, Reason: blindKillContext(others, s.app.Clock())}, nil
			}
		}
		return Result{Decision: Allow}, nil
	case detect.ServerStart:
		for _, e := range f.Entries {
			for _, p := range d.Ports {
				if e.Port == p && foreign(e) {
					return Result{Decision: Deny, Reason: denyPort(e, s)}, nil
				}
			}
		}
		if d.Wrapped {
			return Result{Decision: Allow}, nil
		}
		if c.Strict {
			return Result{Decision: Deny, Reason: strictReason(s)}, nil
		}
		return Result{Decision: Allow, Context: wrapNudge(s)}, nil
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
// "clear" and "resume" are not the end of anything — the same session keeps
// running under a new or restored transcript — so they release nothing.
func (c *Core) sessionEnd(s *scope, reason string) (Result, error) {
	a := s.app
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

func foreignEntries(s *scope, es []registry.Entry) []registry.Entry {
	var out []registry.Entry
	for _, e := range es {
		if !s.owns(e) {
			out = append(out, e)
		}
	}
	return out
}

func denyKill(e registry.Entry, now time.Time) string {
	return fmt.Sprintf("harbormaster: port %d / pid %d belongs to %s. Do not kill it. Use `harbormaster ls` to see what runs, and `harbormaster kill <port>` only for ports this session owns.", e.Port, e.PID, render.OwnerLine(e, now))
}

func denyPort(e registry.Entry, s *scope) string {
	return fmt.Sprintf("harbormaster: port %d is in use by %s. Start this server on this worktree's port instead: `harbormaster run --label \"<task>\" -- <command>` (port %s).", e.Port, render.OwnerLine(e, s.app.Clock()), myPort(s))
}

func strictReason(s *scope) string {
	return fmt.Sprintf("harbormaster: dev servers must be started through the wrapper so other sessions can see them: `harbormaster run --label \"<task>\" -- <command>` (this worktree's port: %s).", myPort(s))
}

// myPort needs the worktree, so this is one of the three callers that pays
// for git discovery.
func myPort(s *scope) string {
	s.git()
	p, err := s.app.MyPort()
	if err != nil {
		return "unknown"
	}
	return fmt.Sprint(p)
}
