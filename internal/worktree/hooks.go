package worktree

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// RunHooks executes each shell command in commands sequentially in workdir, piping
// stdout/stderr to logOut. The first non-zero exit aborts the sequence and the error
// is returned with the failing command included.
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
			return fmt.Errorf("hook failed (%q): %w", c, err)
		}
	}
	return nil
}
