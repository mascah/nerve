package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mascah/nerve/internal/config"
)

// projectView is the per-project detail screen. It tabs between services,
// clone files, and templates.
type projectView struct {
	name string
	path string
	cfg  *config.ProjectConfig

	tab     int // 0=services, 1=clone files, 2=templates
	cursors [3]int
}

var tabNames = []string{"Services", "Clone Files", "Templates"}

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
	return &projectView{name: name, path: path, cfg: cfg}, nil
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
		return func() tea.Msg { return switchViewMsg{to: viewProjects} }
	case "tab":
		v.tab = (v.tab + 1) % len(tabNames)
	case "shift+tab":
		v.tab = (v.tab + len(tabNames) - 1) % len(tabNames)
	case "j", "down":
		if v.cursors[v.tab] < v.tabLen()-1 {
			v.cursors[v.tab]++
		}
	case "k", "up":
		if v.cursors[v.tab] > 0 {
			v.cursors[v.tab]--
		}
	case "a":
		switch v.tab {
		case 0:
			return func() tea.Msg { return switchViewMsg{to: viewAddService, payload: v.path} }
		case 1:
			return func() tea.Msg { return switchViewMsg{to: viewAddClone, payload: v.path} }
		case 2:
			// Templates editing deferred — show a hint via banner.
			return func() tea.Msg {
				return errMsg{err: fmt.Errorf("template editing not in TUI yet — edit .nerve/config.yaml directly")}
			}
		}
	case "d":
		if err := v.deleteCurrent(); err != nil {
			return func() tea.Msg { return errMsg{err} }
		}
	}
	return nil
}

func (v *projectView) tabLen() int {
	switch v.tab {
	case 0:
		return len(v.cfg.Services)
	case 1:
		return len(v.cfg.CloneFiles)
	case 2:
		return len(v.cfg.Templates)
	}
	return 0
}

func (v *projectView) deleteCurrent() error {
	idx := v.cursors[v.tab]
	switch v.tab {
	case 0:
		if idx < 0 || idx >= len(v.cfg.Services) {
			return nil
		}
		v.cfg.Services = append(v.cfg.Services[:idx], v.cfg.Services[idx+1:]...)
	case 1:
		if idx < 0 || idx >= len(v.cfg.CloneFiles) {
			return nil
		}
		v.cfg.CloneFiles = append(v.cfg.CloneFiles[:idx], v.cfg.CloneFiles[idx+1:]...)
	case 2:
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
		case 0:
			count = len(v.cfg.Services)
		case 1:
			count = len(v.cfg.CloneFiles)
		case 2:
			count = len(v.cfg.Templates)
		}
		label := fmt.Sprintf("%s (%d)", name, count)
		if i == v.tab {
			tabs = append(tabs, tabActive.Render(label))
		} else {
			tabs = append(tabs, tabInactive.Render(label))
		}
	}
	b.WriteString(strings.Join(tabs, ""))
	b.WriteString("\n\n")

	switch v.tab {
	case 0:
		b.WriteString(v.renderServices())
	case 1:
		b.WriteString(v.renderCloneFiles())
	case 2:
		b.WriteString(v.renderTemplates())
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("tab switch  ↑↓ navigate  [a] add  [d] delete  esc back  [q] quit"))
	return b.String()
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
