package worktree

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
)

// spawnDetachedFn is the detached re-exec used by the teardown fast path and the
// background hook runner. It's a package var so tests can inject a hermetic stub
// instead of forking a real process (mirrors the ports.ProbeFunc injection point).
var spawnDetachedFn = spawnDetached

// removeWorktreeDir deletes a worktree's working directory and reconciles git's
// administrative metadata.
//
// Synchronous path (background == false, the default): a plain `git worktree
// remove`, which is fully complete when it returns but recursively unlinks the
// whole tree (slow for large node_modules/.venv).
//
// Background path (the project set background_remove): rename the worktree dir
// into <repo>/.nerve/trash/ — instant on the same filesystem — then run `git
// worktree prune` SYNCHRONOUSLY so git's view never lags (it auto-prunes a
// worktree whose dir has vanished), and finally spawn a detached child to delete
// the trashed bytes. The caller returns as soon as the rename + prune finish. If
// the rename can't be done atomically (e.g. a worktree_root on another
// filesystem) it falls back to the synchronous path; if the detached spawn fails
// it deletes the bytes inline so nothing leaks.
func removeWorktreeDir(repoRoot, worktreePath, branch string, force, background bool, log io.Writer) error {
	if !background || !backgroundSupported() {
		return gitutil.RemoveWorktree(repoRoot, worktreePath, force)
	}

	trashDir := filepath.Join(repoRoot, ".nerve", "trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		return gitutil.RemoveWorktree(repoRoot, worktreePath, force)
	}
	dest := filepath.Join(trashDir, trashName(branch))
	if err := os.Rename(worktreePath, dest); err != nil {
		// Cross-filesystem (or any other) rename failure — fall back to the
		// synchronous delete so removal still completes correctly.
		return gitutil.RemoveWorktree(repoRoot, worktreePath, force)
	}

	// The worktree dir is gone; reconcile git's metadata now so its view is never
	// out of sync with the filesystem. Non-fatal on failure: the dir is already
	// moved, and git (or the next prune) treats the missing worktree as prunable.
	if err := gitutil.PruneWorktrees(repoRoot); err != nil {
		fmt.Fprintf(log, "warning: git worktree prune failed (will self-heal on next run): %v\n", err)
	}

	// Delete the trashed bytes detached so we return immediately. gc-trash empties
	// the whole trash dir, also sweeping leftovers from any prior crashed delete.
	if err := spawnDetachedFn("gc-trash", "--repo", repoRoot); err != nil {
		fmt.Fprintf(log, "warning: could not background trash delete (%v); deleting inline\n", err)
		_ = os.RemoveAll(dest)
	}
	return nil
}

// trashName builds a collision-resistant directory name for a trashed worktree:
// the branch slug plus random hex, e.g. "feat_x-9f3a1c20".
func trashName(branch string) string {
	slug := config.Slugify(branch)
	if slug == "" {
		slug = "wt"
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return slug + "-" + hex.EncodeToString(b[:])
}

// chdirAwayIfInside moves the current process out of target (to repoRoot) when its
// cwd is target or a descendant of it. Removing the directory a live process is
// sitting in leaves it in a half-deleted, hung state on macOS; this guard makes
// `nerve remove` / the TUI safe to run from inside the worktree being removed.
func chdirAwayIfInside(target, repoRoot string) {
	if target == "" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	canon, err := gitutil.CanonicalPath(cwd)
	if err != nil {
		canon = cwd
	}
	if canon == target || strings.HasPrefix(canon, target+string(filepath.Separator)) {
		_ = os.Chdir(repoRoot)
	}
}
