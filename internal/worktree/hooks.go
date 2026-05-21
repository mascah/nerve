package worktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// HookError is returned by RunHooks when a hook command exits non-zero. It carries
// the failing command and its exit code so the backgrounded runner can record them
// in the hook status file. Its Error() string matches the previous plain-wrapped
// form, and Unwrap exposes the underlying *exec.ExitError, so existing callers that
// only print or errors.Is the error are unaffected.
type HookError struct {
	Command  string
	ExitCode int
	Err      error
}

func (e *HookError) Error() string { return fmt.Sprintf("hook failed (%q): %v", e.Command, e.Err) }
func (e *HookError) Unwrap() error { return e.Err }

// RunHooks executes each shell command in commands sequentially in workdir, piping
// stdout/stderr to logOut. The first non-zero exit aborts the sequence and returns a
// *HookError naming the failing command and its exit code.
func RunHooks(workdir string, commands []string, logOut io.Writer) error {
	for _, c := range commands {
		if logOut != nil {
			fmt.Fprintf(logOut, "  $ %s\n", c)
		}
		cmd := exec.Command("/bin/sh", "-c", c)
		cmd.Dir = workdir
		cmd.Stdout = logOut
		cmd.Stderr = logOut
		cmd.Env = os.Environ()
		if err := cmd.Run(); err != nil {
			code := -1
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			}
			return &HookError{Command: c, ExitCode: code, Err: err}
		}
	}
	return nil
}
