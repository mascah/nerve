//go:build unix

package worktree

import (
	"os"
	"os/exec"
	"syscall"
)

// spawnDetached re-execs the running nerve binary with args in a fully detached
// child process and returns as soon as it has started (it is never waited on).
//
// Setsid puts the child in a new session with no controlling terminal, so it
// survives the parent exiting and is immune to SIGHUP / signals sent to the
// parent's process group (e.g. Claude Code tearing down the hook's group). The
// std streams are left nil so Go wires them to /dev/null — the child must NOT
// inherit the parent's stdout, since the WorktreeCreate hook reads the worktree
// path from nerve's stdout and stray child output would corrupt it.
func spawnDetached(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// backgroundSupported reports whether detached background execution is available
// on this platform.
func backgroundSupported() bool { return true }
