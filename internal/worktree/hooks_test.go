package worktree

import (
	"errors"
	"io"
	"testing"
)

func TestRunHooks_Success(t *testing.T) {
	if err := RunHooks(t.TempDir(), []string{"true", "echo hi"}, io.Discard); err != nil {
		t.Fatalf("RunHooks: unexpected err %v", err)
	}
}

// TestRunHooks_FailureCarriesCommandAndExitCode confirms a non-zero hook surfaces a
// *HookError naming the failing command and its exit code (used by the backgrounded
// runner to record status), and that the sequence aborts at the first failure.
func TestRunHooks_FailureCarriesCommandAndExitCode(t *testing.T) {
	err := RunHooks(t.TempDir(), []string{"true", "exit 3", "echo should-not-run"}, io.Discard)
	if err == nil {
		t.Fatalf("expected RunHooks to fail")
	}
	var he *HookError
	if !errors.As(err, &he) {
		t.Fatalf("errors.As(err, *HookError) = false; err = %v", err)
	}
	if he.Command != "exit 3" {
		t.Fatalf("HookError.Command = %q; want %q", he.Command, "exit 3")
	}
	if he.ExitCode != 3 {
		t.Fatalf("HookError.ExitCode = %d; want 3", he.ExitCode)
	}
}
