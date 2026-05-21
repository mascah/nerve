package tui

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/hookstatus"
	"github.com/mascah/nerve/internal/registry"
)

// worktreeRow is one row in the Worktrees tab.
type worktreeRow struct {
	Branch      string
	Path        string
	State       string
	PrimaryPort int
	// DirtyCount is the number of uncommitted (modified + untracked) files, computed
	// alongside State so the removal-confirm prompt can warn without a second git call.
	DirtyCount int
	// HookState is the backgrounded post_create hook phase for this worktree, or ""
	// when there's no status (the common case for synchronous projects).
	HookState hookstatus.State
}

// loadWorktreeRows enumerates the linked worktrees for repoRoot (excluding the
// main checkout itself) and joins each with its registry allocation, dirty/unpushed
// state, etc. cfg may be nil (lightweight project) — in that case the PrimaryPort
// column will be zero.
//
// This is the synchronous body invoked from a tea.Cmd goroutine (never directly from
// Update/View) — it forks ~1+3N git subprocesses and so must not run on the UI loop.
func loadWorktreeRows(repoRoot string, cfg *config.ProjectConfig) ([]worktreeRow, error) {
	wts, err := gitutil.ListWorktrees(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}

	// Canonicalize the main checkout once for filtering.
	mainCanon, err := gitutil.CanonicalPath(repoRoot)
	if err != nil {
		mainCanon = repoRoot
	}

	// Pre-load registry allocations keyed by canonical worktree path → primary port.
	portByPath := map[string]int{}
	// Registry lookup is best-effort: a missing/corrupt registry should not block
	// listing of worktrees from git.
	if cfg != nil && cfg.PrimaryService() != nil {
		if reg, regErr := registry.Open(repoRoot).Read(); regErr == nil {
			for portKey, a := range reg.Allocations {
				canon, err := gitutil.CanonicalPath(a.WorktreePath)
				if err != nil {
					canon = a.WorktreePath
				}
				port, err := strconv.Atoi(portKey)
				if err != nil {
					continue
				}
				portByPath[canon] = port
			}
		}
	}

	var rows []worktreeRow
	for _, wt := range wts {
		canon, err := gitutil.CanonicalPath(wt.Path)
		if err != nil {
			canon = wt.Path
		}
		if canon == mainCanon {
			continue
		}

		state, dirty := worktreeStateFor(wt.Path)
		row := worktreeRow{
			Branch:      wt.Branch,
			Path:        wt.Path,
			PrimaryPort: portByPath[canon],
			State:       state,
			DirtyCount:  dirty,
		}
		if row.Branch == "" {
			// Detached HEAD — fall back to short head sha.
			row.Branch = "(detached)"
		}
		// Backgrounded hook status is best-effort: ignore errors and leave the state
		// blank when there's no status file (synchronous projects, older worktrees).
		if s, found, err := hookstatus.Read(repoRoot, config.Slugify(wt.Branch)); err == nil && found {
			row.HookState = s.State
		}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Branch < rows[j].Branch
	})
	return rows, nil
}

// worktreeStateFor returns a short human label describing the worktree at path
// ("dirty (N files)", "unpushed", "clean", or an error description), plus the count
// of uncommitted files so callers can warn on removal without re-running git status.
func worktreeStateFor(path string) (state string, dirtyCount int) {
	dirty, err := gitutil.DirtyFiles(path)
	if err != nil {
		return "err: " + err.Error(), 0
	}
	if len(dirty) > 0 {
		return fmt.Sprintf("dirty (%d files)", len(dirty)), len(dirty)
	}
	unpushed, err := gitutil.HasUnpushedCommits(path)
	if err != nil {
		return "err: " + err.Error(), 0
	}
	if unpushed {
		return "unpushed", 0
	}
	return "clean", 0
}
