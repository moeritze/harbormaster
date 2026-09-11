package liveness_test

import (
	"net"
	"os"
	"os/exec"
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
