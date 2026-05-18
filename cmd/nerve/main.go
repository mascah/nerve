package main

import (
	"fmt"
	"os"

	"github.com/mascah/nerve/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "nerve:", err)
		os.Exit(1)
	}
}
