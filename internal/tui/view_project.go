package tui

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/hookstatus"
	"github.com/mascah/nerve/internal/worktree"
)

// tab indices for projectView. Keep these in sync with tabNames and the cursors array.
const (
	tabServices   = 0
	tabCloneFiles = 1
	tabTemplates  = 2
	tabWorktrees  = 3
)

// worktreesLoadedMsg delivers the result of an async worktree-list load (the git
// fork-fest that used to freeze the UI). err is set when listing failed.
type worktreesLoadedMsg struct {
	rows []worktreeRow
	err  error
}

// worktreeRemovedMsg delivers the result of an async worktree.Remove. err is set when
// removal failed.
type worktreeRemovedMsg struct{ err error }

// projectView is the per-project detail screen. It tabs between services,
// clone files, templates, and worktrees.
type projectView struct {
	name string
	path string
	cfg  *config.ProjectConfig

	tab     int
	cursors [4]int

	// Worktree tab state.
	worktrees []worktreeRow
	// loadedWorktrees is true once an async load has completed (success or error).
	loadedWorktrees bool
	// loadingWorktrees is true while a load command is in flight; drives the
	// "loading…" placeholder and the "(…)" tab-label count.
	loadingWorktrees bool
	// removing is true while a worktree.Remove command is in flight; guards against
	// double-firing on repeated keypresses.
	removing bool
	// confirmIdx is the row index awaiting a second `d` press to confirm removal,
	// or -1 when no confirmation is pending.
	confirmIdx int
	// status is a transient banner shown under the tab body (e.g. "press d again
	// to confirm removal"). Cleared on next interaction.
	status string
}

var tabNames = []string{"Services", "Clone Files", "Templates", "Worktrees"}

func newProjectView(name, path string) (*projectView, error) {
	cfg, err := config.LoadProjectConfig(path)
	if err != nil {
		if err == config.ErrNotFound {
			// Scaffold an empty config so the user can populate it.
			fresh := config.Defaults()
			if err := config.SaveProjectConfig(path, &fresh); err != nil {
				return nil, err
			}
			cfg = &fresh
		} else {
			return nil, err
		}
	}
	return &projectView{name: name, path: path, cfg: cfg, confirmIdx: -1}, nil
}

func (v *projectView) Update(msg tea.Msg) tea.Cmd {
	// Async results arrive as non-key messages; handle them before the key
	// type-assertion below drops everything that isn't a keypress.
	switch m := msg.(type) {
	case worktreesLoadedMsg:
		v.loadingWorktrees = false
		v.loadedWorktrees = true
		if m.err != nil {
			v.worktrees = nil
			v.status = "error: " + m.err.Error()
		} else {
			v.worktrees = m.rows
			// Drop a stale load error now that we have fresh rows.
			if strings.HasPrefix(v.status, "error: ") {
				v.status = ""
			}
		}
		v.clampWorktreeCursor()
		return nil
	case worktreeRemovedMsg:
		v.removing = false
		v.status = ""
		if m.err != nil {
			return func() tea.Msg { return errMsg{m.err} }
		}
		// Refresh the list off the UI loop now that a worktree is gone.
		v.loadingWorktrees = true
		return v.loadWorktreesCmd()
	}

	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch m.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "esc":
		// On the Worktrees tab, clear any pending confirm before bailing back.
		if v.tab == tabWorktrees && v.confirmIdx >= 0 {
			v.confirmIdx = -1
			v.status = ""
			return nil
		}
		return func() tea.Msg { return switchViewMsg{to: viewProjects} }
	case "tab":
		// Tab navigation never triggers synchronous git work — worktrees are loaded
		// eagerly when the project view opens (see App.switchTo).
		v.tab = (v.tab + 1) % len(tabNames)
		v.clearConfirm()
	case "shift+tab":
		v.tab = (v.tab + len(tabNames) - 1) % len(tabNames)
		v.clearConfirm()
	case "j", "down":
		if v.cursors[v.tab] < v.tabLen()-1 {
			v.cursors[v.tab]++
		}
		v.clearConfirm()
	case "k", "up":
		if v.cursors[v.tab] > 0 {
			v.cursors[v.tab]--
		}
		v.clearConfirm()
	case "a":
		switch v.tab {
		case tabServices:
			return func() tea.Msg { return switchViewMsg{to: viewAddService, payload: v.path} }
		case tabCloneFiles:
			return func() tea.Msg { return switchViewMsg{to: viewAddClone, payload: v.path} }
		case tabTemplates:
			// Templates editing deferred — show a hint via banner.
			return func() tea.Msg {
				return errMsg{err: fmt.Errorf("template editing not in TUI yet — edit .nerve/config.yaml directly")}
			}
		case tabWorktrees:
			// Creating worktrees from the TUI is out of scope (needs base-ref selection,
			// hook plumbing, etc.). Surface a hint so the keybinding doesn't feel broken.
			return func() tea.Msg {
				return errMsg{err: fmt.Errorf("use `nerve new <branch>` to create a worktree")}
			}
		}
	case "d":
		if v.tab == tabWorktrees {
			return v.handleWorktreeDelete()
		}
		if err := v.deleteCurrent(); err != nil {
			return func() tea.Msg { return errMsg{err} }
		}
	}
	return nil
}

func (v *projectView) tabLen() int {
	switch v.tab {
	case tabServices:
		return len(v.cfg.Services)
	case tabCloneFiles:
		return len(v.cfg.CloneFiles)
	case tabTemplates:
		return len(v.cfg.Templates)
	case tabWorktrees:
		return len(v.worktrees)
	}
	return 0
}

func (v *projectView) deleteCurrent() error {
	idx := v.cursors[v.tab]
	switch v.tab {
	case tabServices:
		if idx < 0 || idx >= len(v.cfg.Services) {
			return nil
		}
		v.cfg.Services = append(v.cfg.Services[:idx], v.cfg.Services[idx+1:]...)
	case tabCloneFiles:
		if idx < 0 || idx >= len(v.cfg.CloneFiles) {
			return nil
		}
		v.cfg.CloneFiles = append(v.cfg.CloneFiles[:idx], v.cfg.CloneFiles[idx+1:]...)
	case tabTemplates:
		if idx < 0 || idx >= len(v.cfg.Templates) {
			return nil
		}
		v.cfg.Templates = append(v.cfg.Templates[:idx], v.cfg.Templates[idx+1:]...)
	}
	if v.cursors[v.tab] > 0 && v.cursors[v.tab] >= v.tabLen() {
		v.cursors[v.tab] = v.tabLen() - 1
	}
	return config.SaveProjectConfig(v.path, v.cfg)
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
	if idx < 0 || idx >= len(v.worktrees) {
		return nil
	}
	row := v.worktrees[idx]
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

// loadWorktreesCmd returns a tea.Cmd that loads the worktree rows in a goroutine.
// It captures path/cfg by value so the command is safe to run off the UI loop.
func (v *projectView) loadWorktreesCmd() tea.Cmd {
	repoRoot, cfg := v.path, v.cfg
	return func() tea.Msg {
		rows, err := loadWorktreeRows(repoRoot, cfg)
		return worktreesLoadedMsg{rows: rows, err: err}
	}
}

// clampWorktreeCursor keeps the Worktrees cursor within bounds after the row count
// changes (e.g. after a removal refresh).
func (v *projectView) clampWorktreeCursor() {
	if v.cursors[tabWorktrees] >= len(v.worktrees) && v.cursors[tabWorktrees] > 0 {
		v.cursors[tabWorktrees] = len(v.worktrees) - 1
	}
}

func (v *projectView) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("nerve — %s", v.name)))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(v.path))
	b.WriteString("\n\n")

	// Tab bar.
	var tabs []string
	for i, name := range tabNames {
		count := 0
		switch i {
		case tabServices:
			count = len(v.cfg.Services)
		case tabCloneFiles:
			count = len(v.cfg.CloneFiles)
		case tabTemplates:
			count = len(v.cfg.Templates)
		case tabWorktrees:
			// Count is best-effort — only meaningful once loaded.
			count = len(v.worktrees)
		}
		var label string
		if i == tabWorktrees && !v.loadedWorktrees {
			// Worktrees load eagerly when the view opens; show a pending ellipsis until
			// the async result lands so the user never has to tab on to see the count.
			label = fmt.Sprintf("%s (…)", name)
		} else {
			label = fmt.Sprintf("%s (%d)", name, count)
		}
		if i == v.tab {
			tabs = append(tabs, tabActive.Render(label))
		} else {
			tabs = append(tabs, tabInactive.Render(label))
		}
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...))
	b.WriteString("\n\n")

	switch v.tab {
	case tabServices:
		b.WriteString(v.renderServices())
	case tabCloneFiles:
		b.WriteString(v.renderCloneFiles())
	case tabTemplates:
		b.WriteString(v.renderTemplates())
	case tabWorktrees:
		// No synchronous git work here — the list is loaded via loadWorktreesCmd off
		// the UI loop. Show a placeholder until the async result lands.
		if v.loadingWorktrees || !v.loadedWorktrees {
			b.WriteString(muted.Render("loading worktrees…"))
		} else {
			b.WriteString(v.renderWorktrees())
		}
	}

	if v.status != "" {
		b.WriteString("\n")
		b.WriteString(statusWarn.Render(v.status))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render(v.helpLine()))
	return b.String()
}

// helpLine returns a context-aware footer line for the active tab.
func (v *projectView) helpLine() string {
	switch v.tab {
	case tabWorktrees:
		return "tab switch  ↑↓ navigate  [d] remove (press twice)  esc back/cancel  [q] quit"
	default:
		return "tab switch  ↑↓ navigate  [a] add  [d] delete  esc back  [q] quit"
	}
}

func (v *projectView) renderServices() string {
	if len(v.cfg.Services) == 0 {
		return muted.Render("no services yet — press [a] to add one")
	}
	var b strings.Builder
	header := fmt.Sprintf("  %-18s  %-7s  %-30s  %s", "ID", "BASE", "ENV_KEY", "PRIMARY")
	b.WriteString(subtitleStyle.Render(header))
	b.WriteString("\n")
	for i, s := range v.cfg.Services {
		primary := ""
		if s.Primary {
			primary = statusOK.Render("yes")
		}
		line := fmt.Sprintf("  %-18s  %-7d  %-30s  %s", s.ID, s.BasePort, s.EnvKey, primary)
		if i == v.cursors[v.tab] {
			b.WriteString(selectedRow.Render("▸ " + strings.TrimPrefix(line, "  ")))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (v *projectView) renderCloneFiles() string {
	if len(v.cfg.CloneFiles) == 0 {
		return muted.Render("no clone files yet — press [a] to add one")
	}
	var b strings.Builder
	header := fmt.Sprintf("  %-40s  %-10s  %s", "PATH", "KIND", "REQUIRED")
	b.WriteString(subtitleStyle.Render(header))
	b.WriteString("\n")
	for i, f := range v.cfg.CloneFiles {
		kind := f.Kind
		if kind == "" {
			kind = "(auto)"
		}
		req := ""
		if f.Required {
			req = "yes"
		}
		line := fmt.Sprintf("  %-40s  %-10s  %s", f.Path, kind, req)
		if i == v.cursors[v.tab] {
			b.WriteString(selectedRow.Render("▸ " + strings.TrimPrefix(line, "  ")))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (v *projectView) renderTemplates() string {
	if len(v.cfg.Templates) == 0 {
		return muted.Render("no templates yet — edit .nerve/config.yaml directly to add")
	}
	var b strings.Builder
	header := fmt.Sprintf("  %-30s  %-30s  %s", "SOURCE", "DEST", "MERGE")
	b.WriteString(subtitleStyle.Render(header))
	b.WriteString("\n")
	for i, t := range v.cfg.Templates {
		merge := ""
		if t.Merge {
			merge = "yes"
		}
		line := fmt.Sprintf("  %-30s  %-30s  %s", t.Source, t.Dest, merge)
		if i == v.cursors[v.tab] {
			b.WriteString(selectedRow.Render("▸ " + strings.TrimPrefix(line, "  ")))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (v *projectView) renderWorktrees() string {
	if len(v.worktrees) == 0 {
		return muted.Render("no worktrees — use `nerve new <branch>` to create one")
	}
	var b strings.Builder
	header := fmt.Sprintf("  %-24s  %-13s  %-44s  %-18s  %s", "BRANCH", "PRIMARY_PORT", "PATH", "STATE", "HOOKS")
	b.WriteString(subtitleStyle.Render(header))
	b.WriteString("\n")
	for i, w := range v.worktrees {
		port := "-"
		if w.PrimaryPort > 0 {
			port = fmt.Sprintf("%d", w.PrimaryPort)
		}
		line := fmt.Sprintf("  %-24s  %-13s  %-44s  %-18s  %s",
			w.Branch, port, w.Path, w.State, hookStateLabel(w.HookState))
		switch {
		case i == v.confirmIdx:
			b.WriteString(statusErr.Render("▸ " + strings.TrimPrefix(line, "  ")))
		case i == v.cursors[tabWorktrees]:
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
