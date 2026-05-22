//go:build !unix

package worktree

import "fmt"

// spawnDetached is unsupported off unix; callers fall back to running synchronously.
func spawnDetached(_ map[string]string, args ...string) error {
	return fmt.Errorf("background execution is not supported on this platform")
}

// backgroundSupported reports whether detached background execution is available
// on this platform.
func backgroundSupported() bool { return false }
