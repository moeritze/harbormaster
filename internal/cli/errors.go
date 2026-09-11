package cli

// Exit codes per spec §6.5.
const (
	ExitOK           = 0
	ExitDenied       = 1
	ExitUnregistered = 2
	ExitUsage        = 3
	ExitRegistry     = 4
)

// ExitError carries a process exit code and a single actionable message.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }
