//go:build unix && !linux && !darwin

package runner

// stdinIsTerminal is not implemented for this unix flavour; treating stdin
// as a pipe passes it through to the child unchanged (the pre-Plan-2
// behaviour), which is the safe default when the tty ioctl is unknown.
func stdinIsTerminal() bool { return false }
