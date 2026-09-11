package cli

import (
	"errors"
	"fmt"
)

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

func exitf(code int, format string, args ...any) error {
	return &ExitError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// registryErr maps a failure from a registry.Store call to the registry exit
// code. An *ExitError that travelled up from the function passed to
// Store.Update is an ownership decision, not a registry failure, so it keeps
// its own code and message.
func registryErr(err error) error {
	if err == nil {
		return nil
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee
	}
	return exitf(ExitRegistry, "registry: %v", err)
}
