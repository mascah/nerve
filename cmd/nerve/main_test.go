package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mascah/nerve/internal/cli"
)

func TestRunExitCode_nil(t *testing.T) {
	if got := cli.ExitCode(nil); got != cli.ExitOK {
		t.Fatalf("ExitCode(nil) = %d, want %d (ExitOK)", got, cli.ExitOK)
	}
}

func TestRunExitCode_plainError(t *testing.T) {
	err := errors.New("something went wrong")
	if got := cli.ExitCode(err); got != cli.ExitUsage {
		t.Fatalf("ExitCode(plain error) = %d, want %d (ExitUsage)", got, cli.ExitUsage)
	}
}

func TestRunExitCode_wrappedPlainError(t *testing.T) {
	// A wrapped plain error (no exitCodeError in chain) should still return ExitUsage.
	err := fmt.Errorf("outer: %w", errors.New("inner"))
	if got := cli.ExitCode(err); got != cli.ExitUsage {
		t.Fatalf("ExitCode(wrapped plain) = %d, want %d (ExitUsage)", got, cli.ExitUsage)
	}
}
