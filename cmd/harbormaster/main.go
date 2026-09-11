// Command harbormaster is the CLI entry point.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/moeritze/harbormaster/internal/app"
	"github.com/moeritze/harbormaster/internal/cli"
)

func main() {
	a, err := app.New(os.Getenv, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitRegistry)
	}
	root := cli.NewRoot(a)
	if err := root.Execute(); err != nil {
		var ee *cli.ExitError
		if errors.As(err, &ee) {
			fmt.Fprintln(os.Stderr, ee.Msg)
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitUsage)
	}
}
