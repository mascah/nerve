// Package atomicfile centralizes nerve's "write a file without anyone observing a
// half-written copy" discipline: create a temp file in the SAME directory as the
// target, write the bytes, set the mode, then os.Rename over the target. Keeping
// the temp file in the destination directory (not $TMPDIR) means the rename stays
// on one filesystem, so it's a truly atomic replace rather than a copy that can
// fail with EXDEV across mounts.
//
// Every JSON/YAML/env writer in nerve (config, registry/leases via jsonstore,
// hookstatus, envfile, worktree templates) goes through Write so the temp+rename
// dance lives in exactly one place.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write writes data to path atomically with the given file mode. The parent
// directory must already exist (callers that can't assume that should MkdirAll
// first). On any failure the temp file is removed and path is left untouched.
func Write(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
