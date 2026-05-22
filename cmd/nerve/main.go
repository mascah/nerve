package main

import (
	"fmt"
	"os"

	"github.com/mascah/nerve/internal/cli"
)

func main() {
	os.Exit(run())
}

// run executes the root command and returns the appropriate exit code.
// A nil error → 0; an exitCodeError → its Code; any other error → 1 (ExitUsage).
func run() int {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "nerve:", err)
		return cli.ExitCode(err)
	}
	return cli.ExitOK
}
