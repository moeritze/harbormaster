// Package hooks holds the agent-agnostic hook event model and the core
// decisions shared by every adapter (spec §9.1, §9.2).
package hooks

// Kind is the normalized hook event.
type Kind int

// Kind values.
const (
	SessionStart Kind = iota
	PreShell
	PostShell
	SessionEnd
)

func (k Kind) String() string {
	return [...]string{"session_start", "pre_shell", "post_shell", "session_end"}[k]
}

// Event is what an adapter hands to the core.
type Event struct {
	Kind    Kind
	Agent   string // "claude", "cursor", ...
	Session string // agent session id from the hook payload
	Cwd     string // working directory from the hook payload
	Command string // shell command for PreShell/PostShell, else ""
}

// Decision is allow or deny; there is no "ask" in v1.
type Decision int

// Decision values.
const (
	Allow Decision = iota
	Deny
)

// Result is the core's answer. Reason is shown to the agent on deny;
// Context is extra text injected into the agent's context on allow.
type Result struct {
	Decision Decision
	Reason   string
	Context  string
}
