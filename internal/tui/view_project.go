package tui

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/worktree"
)

// tab indices for projectView. Keep these in sync with tabNames and the cursors array.
const (
	tabServices   = 0
	tabCloneFiles = 1
	tabTemplates  = 2
	tabWorktrees  = 3
)

// projectView is the per-project detail screen. It tabs between services,
// clone files, templates, and worktrees.
type projectView struct {
	name string
	path string
	cfg  *config.ProjectConfig

	tab     int
	cursors [4]int

	// Worktree tab state.
	worktrees       []worktreeRow
	loadedWorktrees bool
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
		v.tab = (v.tab + 1) % len(tabNames)
		v.clearConfirm()
		v.ensureWorktreesLoaded()
	case "shift+tab":
		v.tab = (v.tab + len(tabNames) - 1) % len(tabNames)
		v.clearConfirm()
		v.ensureWorktreesLoaded()
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
// First press arms confirmation; second press (on the same row) executes worktree.Remove.
func (v *projectView) handleWorktreeDelete() tea.Cmd {
	idx := v.cursors[tabWorktrees]
	if idx < 0 || idx >= len(v.worktrees) {
		return nil
	}
	if v.confirmIdx != idx {
		v.confirmIdx = idx
		v.status = "press d again to confirm removal, esc to cancel"
		return nil
	}
	// Confirmed — execute.
	row := v.worktrees[idx]
	_, err := worktree.Remove(worktree.RemoveOptions{
		RepoRoot:     v.path,
		WorktreePath: row.Path,
		Branch:       row.Branch,
		Cfg:          v.cfg,
		Force:        true,
		Log:          io.Discard,
	})
	v.confirmIdx = -1
	v.status = ""
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	v.refreshWorktrees()
	// Clamp cursor after removal.
	if v.cursors[tabWorktrees] >= len(v.worktrees) && v.cursors[tabWorktrees] > 0 {
		v.cursors[tabWorktrees] = len(v.worktrees) - 1
	}
	return nil
}

// clearConfirm drops any pending d-press confirmation. Called on cursor movement
// or tab changes so the user can't accidentally confirm against a different row.
func (v *projectView) clearConfirm() {
	if v.confirmIdx != -1 || v.status != "" {
		v.confirmIdx = -1
		v.status = ""
	}
}

// ensureWorktreesLoaded lazy-loads the worktree list the first time the Worktrees
// tab is visited. Subsequent visits keep the cached list until refreshWorktrees.
func (v *projectView) ensureWorktreesLoaded() {
	if v.tab != tabWorktrees || v.loadedWorktrees {
		return
	}
	v.refreshWorktrees()
}

func (v *projectView) refreshWorktrees() {
	rows, err := loadWorktreeRows(v.path, v.cfg)
	if err != nil {
		// Render nothing but stash the error in the status banner so the user sees it.
		v.worktrees = nil
		v.status = "error: " + err.Error()
	} else {
		v.worktrees = rows
		// Status only cleared if it was a previous error — otherwise leave alone.
		if strings.HasPrefix(v.status, "error: ") {
			v.status = ""
		}
	}
	v.loadedWorktrees = true
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
			// Before first lazy load we don't know the count yet; omit it.
			label = name
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
		// Lazy-load on first View() of this tab so callers that don't go through
		// Update (e.g. tests calling View directly) still see real data.
		if !v.loadedWorktrees {
			v.refreshWorktrees()
		}
		b.WriteString(v.renderWorktrees())
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
	header := fmt.Sprintf("  %-24s  %-13s  %-50s  %s", "BRANCH", "PRIMARY_PORT", "PATH", "STATE")
	b.WriteString(subtitleStyle.Render(header))
	b.WriteString("\n")
	for i, w := range v.worktrees {
		port := "-"
		if w.PrimaryPort > 0 {
			port = fmt.Sprintf("%d", w.PrimaryPort)
		}
		line := fmt.Sprintf("  %-24s  %-13s  %-50s  %s", w.Branch, port, w.Path, w.State)
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
