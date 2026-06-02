package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mascah/nerve/internal/config"
)

// tab indices for projectView. Keep these in sync with tabNames and the cursors array.
const (
	tabServices   = 0
	tabCloneFiles = 1
	tabTemplates  = 2
	tabWorktrees  = 3
	tabPorts      = 4
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

// portsLoadedMsg delivers the result of an async pool-probe (the pool_size×services
// bind-probe sweep). err is set when the registry read failed; rows is built off the
// UI loop by loadPortsCmd.
type portsLoadedMsg struct {
	rows []portsRow
	err  error
}

// projectView is the per-project detail screen. It tabs between services,
// clone files, templates, and worktrees.
type projectView struct {
	name string
	path string
	cfg  *config.ProjectConfig

	tab     int
	cursors [5]int

	// Worktree tab state. Rows load eagerly off the UI loop when the view opens
	// (loadWorktreesCmd) so the count shows on the tab label without tabbing in.
	worktrees asyncList[worktreeRow]
	// removing is true while a worktree.Remove command is in flight; guards against
	// double-firing on repeated keypresses.
	removing bool
	// confirmIdx is the row index awaiting a second `d` press to confirm removal,
	// or -1 when no confirmation is pending.
	confirmIdx int
	// status is a transient banner shown under the tab body (e.g. "press d again
	// to confirm removal"). Cleared on next interaction.
	status string

	// Ports tab state. Probing pool_size×services ports must never run on the UI loop,
	// so the rows load lazily (on first focus) via loadPortsCmd, mirroring the worktree
	// tab's async pattern.
	ports asyncList[portsRow]
}

var tabNames = []string{"Services", "Clone Files", "Templates", "Worktrees", "Ports"}

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
		if m.err != nil {
			v.worktrees.fail()
			v.status = "error: " + m.err.Error()
		} else {
			v.worktrees.set(m.rows)
			// Drop a stale load error now that we have fresh rows.
			if strings.HasPrefix(v.status, "error: ") {
				v.status = ""
			}
		}
		v.cursors[tabWorktrees] = v.worktrees.clamp(v.cursors[tabWorktrees])
		return nil
	case worktreeRemovedMsg:
		v.removing = false
		v.status = ""
		if m.err != nil {
			return func() tea.Msg { return errMsg(m) }
		}
		// Refresh the list off the UI loop now that a worktree is gone.
		v.worktrees.begin()
		return v.loadWorktreesCmd()
	case portsLoadedMsg:
		if m.err != nil {
			v.ports.fail()
			v.status = "error: " + m.err.Error()
		} else {
			v.ports.set(m.rows)
			if strings.HasPrefix(v.status, "error: ") {
				v.status = ""
			}
		}
		v.cursors[tabPorts] = v.ports.clamp(v.cursors[tabPorts])
		return nil
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
		// eagerly when the project view opens (see App.switchTo). The Ports tab probes
		// lazily on first focus (see focusPortsIfNeeded).
		v.tab = (v.tab + 1) % len(tabNames)
		v.clearConfirm()
		return v.focusPortsIfNeeded()
	case "shift+tab":
		v.tab = (v.tab + len(tabNames) - 1) % len(tabNames)
		v.clearConfirm()
		return v.focusPortsIfNeeded()
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
		case tabPorts:
			// Ports has nothing to add — no-op.
		}
	case "d":
		if v.tab == tabWorktrees {
			return v.handleWorktreeDelete()
		}
		if v.tab == tabPorts {
			// Ports has nothing to delete — no-op.
			return nil
		}
		if err := v.deleteCurrent(); err != nil {
			return func() tea.Msg { return errMsg{err} }
		}
	case "r":
		// Re-probe the pool while on the Ports tab (a running dev server may have come
		// up or gone away since the last sweep). No-op on other tabs.
		if v.tab == tabPorts && !v.ports.loading {
			v.ports.begin()
			return v.loadPortsCmd()
		}
	}
	return nil
}

// focusPortsIfNeeded kicks off the lazy pool probe the first time the user lands on the
// Ports tab. Returns nil when not on the Ports tab or when the rows are already loaded /
// loading. Mirrors the worktree-tab async pattern so the UI loop never blocks on probing.
func (v *projectView) focusPortsIfNeeded() tea.Cmd {
	if v.tab != tabPorts || v.ports.loaded || v.ports.loading {
		return nil
	}
	v.ports.begin()
	return v.loadPortsCmd()
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
		return v.worktrees.len()
	case tabPorts:
		return v.ports.len()
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

// loadWorktreesCmd returns a tea.Cmd that loads the worktree rows in a goroutine.
// It captures path/cfg by value so the command is safe to run off the UI loop.
func (v *projectView) loadWorktreesCmd() tea.Cmd {
	repoRoot, cfg := v.path, v.cfg
	return func() tea.Msg {
		rows, err := loadWorktreeRows(repoRoot, cfg)
		return worktreesLoadedMsg{rows: rows, err: err}
	}
}

// loadPortsCmd returns a tea.Cmd that reads the registry and probes every pool port in
// a goroutine. It captures path/cfg by value so the command is safe to run off the UI
// loop (the pool_size×services bind-probe sweep must never block Update/View).
func (v *projectView) loadPortsCmd() tea.Cmd {
	repoRoot, cfg := v.path, v.cfg
	return func() tea.Msg {
		rows, err := loadPortsRows(repoRoot, cfg)
		return portsLoadedMsg{rows: rows, err: err}
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
			count = v.worktrees.len()
		case tabPorts:
			// The pool size is known synchronously from config, so the count is always
			// meaningful (no pending ellipsis needed) regardless of probe state.
			if v.cfg.PrimaryService() != nil {
				count = v.cfg.Project.PoolSize
			}
		}
		var label string
		if i == tabWorktrees && !v.worktrees.loaded {
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
		if v.worktrees.loading || !v.worktrees.loaded {
			b.WriteString(muted.Render("loading worktrees…"))
		} else {
			b.WriteString(v.renderWorktrees())
		}
	case tabPorts:
		// Lightweight projects have no pool to visualize — show a hint, never probe.
		if v.cfg.PrimaryService() == nil {
			b.WriteString(muted.Render("no services configured — add services to see port allocations"))
		} else if v.ports.loading || !v.ports.loaded {
			// Probing runs off the UI loop via loadPortsCmd; placeholder until it lands.
			b.WriteString(muted.Render("loading ports…"))
		} else {
			b.WriteString(v.renderPorts())
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
	case tabPorts:
		return "tab switch  ↑↓ navigate  ● in use  ○ free  [r] re-probe  esc back  [q] quit"
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
