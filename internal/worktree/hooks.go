package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"sync"
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
// stdout/stderr to logOut. Each command runs with the parent environment plus the
// key=value pairs in extraEnv (used to expose the worktree's allocated ports and
// template vars to post_create setup scripts). The first non-zero exit aborts the
// sequence and returns a *HookError naming the failing command and its exit code.
func RunHooks(workdir string, commands []string, extraEnv map[string]string, logOut io.Writer) error {
	env := hookEnviron(extraEnv)
	for _, c := range commands {
		if logOut != nil {
			fmt.Fprintf(logOut, "  $ %s\n", c)
		}
		if err := runOneHook(workdir, c, env, logOut); err != nil {
			return err
		}
	}
	return nil
}

// RunHooksParallel runs every command in commands concurrently in workdir, each with
// the same environment RunHooks uses. It blocks until all have finished. Output is
// buffered per command and flushed to logOut in declared order once everything
// completes, so concurrent writes don't interleave mid-line and each command's log
// stays attributable. If one or more commands fail, the *HookError for the
// earliest-declared failing command is returned, but all commands still run to
// completion (background installs are independent — one failing shouldn't abort the
// others). Used for the detached background subset of post_create hooks.
func RunHooksParallel(workdir string, commands []string, extraEnv map[string]string, logOut io.Writer) error {
	env := hookEnviron(extraEnv)
	outs := make([]bytes.Buffer, len(commands))
	errs := make([]error, len(commands))
	var wg sync.WaitGroup
	for i, c := range commands {
		wg.Add(1)
		go func(i int, c string) {
			defer wg.Done()
			fmt.Fprintf(&outs[i], "  $ %s\n", c)
			errs[i] = runOneHook(workdir, c, env, &outs[i])
		}(i, c)
	}
	wg.Wait()

	var firstErr error
	for i := range commands {
		if logOut != nil {
			_, _ = logOut.Write(outs[i].Bytes())
		}
		if errs[i] != nil && firstErr == nil {
			firstErr = errs[i]
		}
	}
	return firstErr
}

// runOneHook executes a single shell command, returning a *HookError on non-zero exit.
func runOneHook(workdir, c string, env []string, out io.Writer) error {
	cmd := exec.Command("/bin/sh", "-c", c)
	cmd.Dir = workdir
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return &HookError{Command: c, ExitCode: code, Err: err}
	}
	return nil
}

// hookEnviron returns os.Environ() with extraEnv appended in sorted key order
// (deterministic for reproducible runs and stable tests).
func hookEnviron(extraEnv map[string]string) []string {
	env := os.Environ()
	if len(extraEnv) == 0 {
		return env
	}
	keys := make([]string, 0, len(extraEnv))
	for k := range extraEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+extraEnv[k])
	}
	return env
}
