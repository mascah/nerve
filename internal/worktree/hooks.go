package worktree

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
)

// RunHooks executes each shell command in commands sequentially in workdir, piping
// stdout/stderr to logOut. Each command runs with the parent environment plus the
// key=value pairs in extraEnv (used to expose the worktree's allocated ports and
// template vars to post_create setup scripts). The first non-zero exit aborts the
// sequence and the error is returned with the failing command included.
func RunHooks(workdir string, commands []string, extraEnv map[string]string, logOut io.Writer) error {
	env := os.Environ()
	if len(extraEnv) > 0 {
		// Sort for deterministic, reproducible env ordering (and stable tests).
		keys := make([]string, 0, len(extraEnv))
		for k := range extraEnv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			env = append(env, k+"="+extraEnv[k])
		}
	}
	for _, c := range commands {
		if logOut != nil {
			fmt.Fprintf(logOut, "  $ %s\n", c)
		}
		cmd := exec.Command("/bin/sh", "-c", c)
		cmd.Dir = workdir
		cmd.Stdout = logOut
		cmd.Stderr = logOut
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("hook failed (%q): %w", c, err)
		}
	}
	return nil
}
