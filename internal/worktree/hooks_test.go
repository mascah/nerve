package worktree

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHooks_Success(t *testing.T) {
	if err := RunHooks(t.TempDir(), []string{"true", "echo hi"}, nil, io.Discard); err != nil {
		t.Fatalf("RunHooks: unexpected err %v", err)
	}
}

// TestRunHooks_ExtraEnvReachesHook confirms extraEnv pairs are exported into the hook
// process environment (how post_create hooks read allocated ports / identity vars).
func TestRunHooks_ExtraEnvReachesHook(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "env.out")
	err := RunHooks(dir, []string{"printf '%s' \"$WEB_PORT\" > " + out},
		map[string]string{"WEB_PORT": "8042"}, io.Discard)
	if err != nil {
		t.Fatalf("RunHooks: %v", err)
	}
	got, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatalf("read hook output: %v", readErr)
	}
	if strings.TrimSpace(string(got)) != "8042" {
		t.Fatalf("hook saw WEB_PORT=%q; want 8042", string(got))
	}
}

// TestRunHooks_FailureCarriesCommandAndExitCode confirms a non-zero hook surfaces a
// *HookError naming the failing command and its exit code (used by the backgrounded
// runner to record status), and that the sequence aborts at the first failure.
func TestRunHooks_FailureCarriesCommandAndExitCode(t *testing.T) {
	err := RunHooks(t.TempDir(), []string{"true", "exit 3", "echo should-not-run"}, nil, io.Discard)
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
