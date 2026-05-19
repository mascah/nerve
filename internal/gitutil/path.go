package gitutil

import (
	"os"
	"path/filepath"
	"strings"
)

// CanonicalPath returns the absolute, symlink-resolved form of path. If symlink
// resolution fails (commonly because the path does not exist yet), it falls back
// to filepath.Abs(path). Used to keep worktree-path identity consistent between
// the registry (where paths are stored at create time) and lookup callers like
// `nerve env --inject` (where git may return the symlink-resolved form via
// `git rev-parse --show-toplevel`).
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, nil
	}
	return resolved, nil
}

// ExpandPath expands a user-supplied path the way a shell would: a leading "~"
// becomes the user's home directory, and $VAR / ${VAR} references are substituted
// from the environment. Returns the input unchanged if no expansion applies.
// Errors only when "~" expansion is requested but the home directory can't be
// resolved.
func ExpandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path), nil
}
