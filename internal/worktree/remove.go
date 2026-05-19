package worktree

import (
	"errors"
	"fmt"
	"io"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/leases"
	"github.com/mascah/nerve/internal/registry"
)

// Errors returned by Remove that callers may surface as specific exit codes.
var (
	ErrDirty    = errors.New("worktree has uncommitted changes or untracked files")
	ErrUnpushed = errors.New("worktree has unpushed commits")
)

// DirtyError is returned by Remove when the worktree has uncommitted changes or
// untracked files and Force was not set. It carries the list of dirty files so the
// CLI can show the user what they would lose. errors.Is(err, ErrDirty) returns true
// for a *DirtyError, so existing callers that only check ErrDirty still work.
type DirtyError struct {
	Files []string
}

func (e *DirtyError) Error() string { return ErrDirty.Error() }

// Unwrap lets errors.Is(err, ErrDirty) match a *DirtyError.
func (e *DirtyError) Unwrap() error { return ErrDirty }

// RemoveOptions feeds Remove.
type RemoveOptions struct {
	RepoRoot     string
	WorktreePath string                // absolute path of the worktree to remove
	Branch       string                // optional; deleted iff CreatedByNerve and !KeepBranch
	Cfg          *config.ProjectConfig // nil => skip lifecycle hooks
	Force        bool                  // override dirty/unpushed checks; force git worktree remove
	KeepBranch   bool
	Log          io.Writer
}

// RemoveResult summarizes what was cleaned up.
type RemoveResult struct {
	ReleasedPort string // registry key, empty if no allocation existed
	BranchDeleted bool
}

// Remove tears down a worktree: runs pre_remove hooks, releases its port allocation,
// removes the git worktree, and (optionally) deletes the branch. Returns ErrDirty or
// ErrUnpushed if those checks fail and Force is false.
func Remove(opts RemoveOptions) (*RemoveResult, error) {
	if opts.RepoRoot == "" || opts.WorktreePath == "" {
		return nil, fmt.Errorf("worktree.Remove: RepoRoot and WorktreePath are required")
	}
	log := discardIfNil(opts.Log)

	if !opts.Force {
		dirtyFiles, err := gitutil.DirtyFiles(opts.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("check dirty: %w", err)
		}
		if len(dirtyFiles) > 0 {
			return nil, &DirtyError{Files: dirtyFiles}
		}
		unpushed, err := gitutil.HasUnpushedCommits(opts.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("check unpushed: %w", err)
		}
		if unpushed {
			return nil, ErrUnpushed
		}
	}

	res := &RemoveResult{}

	// Run pre_remove hooks before any destructive action.
	if opts.Cfg != nil && len(opts.Cfg.Hooks.PreRemove) > 0 {
		fmt.Fprintln(log, "running pre_remove hooks:")
		if err := RunHooks(opts.WorktreePath, opts.Cfg.Hooks.PreRemove, log); err != nil {
			return res, err
		}
	}

	// Release port allocation. Capture whether the branch was created by nerve while
	// we have the registry open so we can decide on branch deletion later.
	createdByNerve := false
	handle := registry.Open(opts.RepoRoot)
	target, _ := gitutil.CanonicalPath(opts.WorktreePath)
	err := handle.With(func(reg *registry.Registry) error {
		for port, a := range reg.Allocations {
			stored, err := gitutil.CanonicalPath(a.WorktreePath)
			if err != nil {
				continue
			}
			if stored == target {
				createdByNerve = a.CreatedByNerve
				res.ReleasedPort = port
				delete(reg.Allocations, port)
				break
			}
		}
		return nil
	})
	if err != nil {
		return res, fmt.Errorf("release port: %w", err)
	}

	// Release any global cross-project leases held by this worktree. Best-effort:
	// failures are logged but do not block worktree removal — we want `nerve
	// remove` to be as forgiving as possible.
	if leasesStore, err := leases.Open(); err != nil {
		fmt.Fprintf(log, "warning: open leases store failed: %v\n", err)
	} else if released, err := leasesStore.Release(opts.WorktreePath); err != nil {
		fmt.Fprintf(log, "warning: release leases failed: %v\n", err)
	} else if len(released) > 0 {
		fmt.Fprintf(log, "released %d global port lease(s)\n", len(released))
	}

	// Remove the git worktree.
	fmt.Fprintf(log, "git worktree remove %s\n", opts.WorktreePath)
	if err := gitutil.RemoveWorktree(opts.RepoRoot, opts.WorktreePath, opts.Force); err != nil {
		return res, fmt.Errorf("git worktree remove: %w", err)
	}

	// Delete branch if appropriate.
	if opts.Branch != "" && createdByNerve && !opts.KeepBranch {
		if err := gitutil.DeleteBranch(opts.RepoRoot, opts.Branch, true); err != nil {
			fmt.Fprintf(log, "warning: branch delete failed: %v\n", err)
		} else {
			res.BranchDeleted = true
			fmt.Fprintf(log, "deleted branch %s\n", opts.Branch)
		}
	}

	return res, nil
}
