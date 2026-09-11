package runner

import (
	"os"

	"golang.org/x/sys/unix"
)

// stdinIsTerminal reports whether harbormaster's own stdin is a tty.
func stdinIsTerminal() bool {
	_, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TIOCGETA)
	return err == nil
}
