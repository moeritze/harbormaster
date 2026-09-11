// Package liveness answers "is this pid alive" and "is this port listening".
package liveness

import (
	"net"
	"strconv"
	"time"
)

// OS probes the real operating system.
type OS struct {
	DialTimeout time.Duration
}

// PortListening dials 127.0.0.1:port. Works on every platform without lsof.
func (o OS) PortListening(port int) bool {
	d := o.DialTimeout
	if d == 0 {
		d = 200 * time.Millisecond
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), d)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
