package runner

// stdinIsTerminal is always false on windows: there are no process groups
// and no SIGTTIN, so the child's stdin is never detached.
func stdinIsTerminal() bool { return false }
