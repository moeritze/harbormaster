package runner

import "golang.org/x/sys/unix"

// ioctlReadTermios is the linux request that reads a tty's settings.
const ioctlReadTermios = unix.TCGETS
