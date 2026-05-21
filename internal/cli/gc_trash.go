package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// newGCTrashCmd registers the hidden `gc-trash` entry point. It is not for humans:
// the worktree teardown fast path (background_remove) renames a worktree dir into
// <repo>/.nerve/trash/ and spawns this detached child to delete the bytes, so the
// caller returns immediately. It empties the entire trash dir, so it also sweeps up
// leftovers from any prior delete that was interrupted. The human-facing equivalent
// is `nerve gc`.
func newGCTrashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "gc-trash",
		Short:  "Hook entry point: delete trashed worktree bytes (backgrounded)",
		Hidden: true,
		RunE:   runGCTrash,
	}
	cmd.Flags().String("repo", "", "main checkout whose .nerve/trash dir to empty")
	return cmd
}

func runGCTrash(cmd *cobra.Command, _ []string) error {
	repo, _ := cmd.Flags().GetString("repo")
	if repo == "" {
		return fmt.Errorf("gc-trash: --repo is required")
	}
	_, err := clearTrash(repo)
	return err
}

// trashDirFor returns the trash staging dir for a repo's main checkout.
func trashDirFor(repoRoot string) string {
	return filepath.Join(repoRoot, ".nerve", "trash")
}

// clearTrash removes every top-level entry under the repo's .nerve/trash dir,
// returning how many it removed. A missing trash dir is not an error.
func clearTrash(repoRoot string) (int, error) {
	dir := trashDirFor(repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err == nil {
			removed++
		}
	}
	return removed, nil
}

// trashStats reports the number of top-level leftover entries and their total size
// in the repo's trash dir. Used by `nerve doctor` to surface clutter from an
// interrupted background delete (normally the detached gc-trash empties it promptly).
func trashStats(repoRoot string) (count int, bytes int64) {
	dir := trashDirFor(repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		count++
		bytes += treeSize(filepath.Join(dir, e.Name()))
	}
	return count, bytes
}

// treeSize sums the sizes of all regular files under path (best-effort).
func treeSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// humanBytes renders a byte count as a compact human-readable string.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
