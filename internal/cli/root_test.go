package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/cli"
)

func TestVersionPrintsVersion(t *testing.T) {
	var out bytes.Buffer
	a := &app.App{Stdout: &out, Stderr: &out}
	cli.Version = "1.2.3-test"
	root := cli.NewRoot(a)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "harbormaster 1.2.3-test") {
		t.Fatalf("got %q", out.String())
	}
}
