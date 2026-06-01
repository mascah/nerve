package tui

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/hookstatus"
	"github.com/mascah/nerve/internal/registry"
	"github.com/mascah/nerve/internal/worktree"
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

// handleWorktreeDelete implements the two-press confirmation for removing a worktree.
// First press arms confirmation (warning on the file count when dirty); second press
// (on the same row) kicks off an async worktree.Remove so the UI loop never blocks.
func (v *projectView) handleWorktreeDelete() tea.Cmd {
	// Ignore further presses while a removal is already in flight.
	if v.removing {
		return nil
	}
	idx := v.cursors[tabWorktrees]
	if idx < 0 || idx >= v.worktrees.len() {
		return nil
	}
	row := v.worktrees.rows[idx]
	if v.confirmIdx != idx {
		v.confirmIdx = idx
		if row.DirtyCount > 0 {
			v.status = fmt.Sprintf("⚠ %s has %d uncommitted changes — press d again to delete anyway, esc to cancel",
				row.Branch, row.DirtyCount)
		} else {
			v.status = "press d again to confirm removal, esc to cancel"
		}
		return nil
	}
	// Confirmed — run the removal off the UI loop. worktree.Remove forks git and runs
	// pre_remove hooks, so doing it inline here would hang the whole TUI.
	v.confirmIdx = -1
	v.removing = true
	v.status = fmt.Sprintf("removing %s…", row.Branch)
	repoRoot, cfg := v.path, v.cfg
	return func() tea.Msg {
		_, err := worktree.Remove(worktree.RemoveOptions{
			RepoRoot:     repoRoot,
			WorktreePath: row.Path,
			Branch:       row.Branch,
			Cfg:          cfg,
			Force:        true,
			Log:          io.Discard,
		})
		return worktreeRemovedMsg{err: err}
	}
}

// clearConfirm drops any pending d-press confirmation. Called on cursor movement
// or tab changes so the user can't accidentally confirm against a different row.
// Leaves an in-flight "removing…" status alone.
func (v *projectView) clearConfirm() {
	if v.removing {
		return
	}
	if v.confirmIdx != -1 || v.status != "" {
		v.confirmIdx = -1
		v.status = ""
	}
}

// renderWorktrees draws the Worktrees tab: one row per linked worktree with its primary
// port, path, dirty/unpushed state, and backgrounded-hook phase. The cursor row is
// highlighted; a row armed for removal is shown in the error style.
func (v *projectView) renderWorktrees() string {
	if v.worktrees.len() == 0 {
		return muted.Render("no worktrees — use `nerve new <branch>` to create one")
	}
	var b strings.Builder
	header := fmt.Sprintf("  %-24s  %-13s  %-44s  %-18s  %s", "BRANCH", "PRIMARY_PORT", "PATH", "STATE", "HOOKS")
	b.WriteString(subtitleStyle.Render(header))
	b.WriteString("\n")
	for i, w := range v.worktrees.rows {
		port := "-"
		if w.PrimaryPort > 0 {
			port = fmt.Sprintf("%d", w.PrimaryPort)
		}
		line := fmt.Sprintf("  %-24s  %-13s  %-44s  %-18s  %s",
			w.Branch, port, w.Path, w.State, hookStateLabel(w.HookState))
		switch i {
		case v.confirmIdx:
			b.WriteString(statusErr.Render("▸ " + strings.TrimPrefix(line, "  ")))
		case v.cursors[tabWorktrees]:
			b.WriteString(selectedRow.Render("▸ " + strings.TrimPrefix(line, "  ")))
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// hookStateLabel renders the backgrounded post_create hook phase for the HOOKS column.
// Blank when there's no status (synchronous projects); a failed run is highlighted red.
func hookStateLabel(s hookstatus.State) string {
	switch s {
	case hookstatus.StateRunning:
		return statusWarn.Render("running…")
	case hookstatus.StateFailed:
		return statusErr.Render("failed")
	case hookstatus.StateOK:
		return statusOK.Render("✓")
	default:
		return ""
	}
}
