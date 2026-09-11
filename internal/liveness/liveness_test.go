package liveness_test

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/liveness"
)

func TestPidAliveSelfTrueDeadFalse(t *testing.T) {
	p := liveness.OS{}
	if !p.PidAlive(os.Getpid()) {
		t.Fatal("own pid should be alive")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("no `true` binary")
	}
	if p.PidAlive(cmd.Process.Pid) {
		t.Fatal("exited pid should be dead")
	}
}

func TestPortListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	p := liveness.OS{DialTimeout: 200 * time.Millisecond}
	if !p.PortListening(port) {
		t.Fatal("expected listening")
	}
	ln.Close() //nolint:errcheck,gosec // test cleanup; close error is not actionable
	if p.PortListening(port) {
		t.Fatal("expected closed")
	}

	// A server bound to the IPv6 loopback only must count as listening too.
	ln6, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		if strings.Contains(err.Error(), "cannot assign") || strings.Contains(err.Error(), "not supported") ||
			strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "no route") {
			t.Skip("IPv6 loopback unavailable here:", err)
		}
		t.Fatal(err)
	}
	port6 := ln6.Addr().(*net.TCPAddr).Port
	if !p.PortListening(port6) {
		t.Fatal("expected ::1 listener to count as listening")
	}
	ln6.Close() //nolint:errcheck,gosec // test cleanup; close error is not actionable
	if p.PortListening(port6) {
		t.Fatal("expected closed after the ::1 listener went away")
	}
}

func TestPidUIDSelf(t *testing.T) {
	uid, err := liveness.PidUID(os.Getpid())
	if err != nil {
		t.Skip("PidUID unsupported here:", err)
	}
	if uid != os.Getuid() {
		t.Fatalf("uid %d != %d", uid, os.Getuid())
	}
}

func TestPidOnPortFindsListener(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not available")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck // test cleanup; close error is not actionable
	port := ln.Addr().(*net.TCPAddr).Port
	pid, _, ok := liveness.PidOnPort(port)
	if !ok || pid != os.Getpid() {
		t.Fatalf("ok=%v pid=%d want %d", ok, pid, os.Getpid())
	}
}

func TestPidStartTimeSelf(t *testing.T) {
	st, err := liveness.PidStartTime(os.Getpid())
	if err != nil {
		t.Skip("PidStartTime unsupported here:", err)
	}
	if st == "" {
		t.Fatal("expected a non-empty start time")
	}
	again, _ := liveness.PidStartTime(os.Getpid())
	if again != st {
		t.Fatalf("start time must be stable: %q vs %q", st, again)
	}
}
