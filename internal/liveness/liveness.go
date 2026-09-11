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

// loopbacks are the addresses a local dev server may bind. A server that
// listens on ::1 only (node's default on some setups, `--host localhost` in
// an IPv6-first resolver) is just as live as one on 127.0.0.1.
var loopbacks = []string{"127.0.0.1", "::1"}

// PortListening dials the IPv4 and then the IPv6 loopback and reports true if
// either connects. Works on every platform without lsof.
func (o OS) PortListening(port int) bool {
	d := o.DialTimeout
	if d == 0 {
		d = 200 * time.Millisecond
	}
	p := strconv.Itoa(port)
	for _, host := range loopbacks {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, p), d)
		if err != nil {
			continue
		}
		_ = c.Close()
		return true
	}
	return false
}
