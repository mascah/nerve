package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/registry"
)

// projectsView is the root view: a list of all registered projects with status.
type projectsView struct {
	rows   []projectRow
	cursor int
}

type projectRow struct {
	name        string
	path        string
	configured  bool
	worktrees   int
	allocations int
	pool        config.ProjectConfig // optional, zero if lightweight
	err         string               // non-empty when the row failed to load
}

func newProjectsView() (*projectsView, error) {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return nil, err
	}
	rows := make([]projectRow, 0, len(reg.Projects))
	for _, p := range reg.Projects {
		r := projectRow{name: p.Name, path: p.Path}
		if info, err := gitutil.Discover(p.Path); err != nil {
			r.err = err.Error()
			rows = append(rows, r)
			continue
		} else if info.MainCheckout != p.Path {
			r.err = "registry path != main checkout"
		}
		if cfg, err := config.LoadProjectConfig(p.Path); err == nil {
			r.configured = cfg.IsConfigured()
		}
		if wts, err := gitutil.ListWorktrees(p.Path); err == nil {
			r.worktrees = len(wts) - 1 // exclude main
			if r.worktrees < 0 {
				r.worktrees = 0
			}
		}
		if reg, err := registry.Open(p.Path).Read(); err == nil {
			r.allocations = len(reg.Allocations)
		}
		rows = append(rows, r)
	}
	return &projectsView{rows: rows}, nil
}

func (v *projectsView) Update(msg tea.Msg) tea.Cmd {
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch m.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "j", "down":
		if v.cursor < len(v.rows)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "a":
		return func() tea.Msg { return switchViewMsg{to: viewAddProject} }
	case "enter":
		if len(v.rows) == 0 {
			return nil
		}
		r := v.rows[v.cursor]
		return func() tea.Msg {
			return switchViewMsg{to: viewProject, payload: projectPayload{name: r.name, path: r.path}}
		}
	}
	return nil
}

func (v *projectsView) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("nerve — projects"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("%d registered", len(v.rows))))
	b.WriteString("\n\n")

	if len(v.rows) == 0 {
		b.WriteString(muted.Render("no projects registered yet. press [a] to add one."))
	} else {
		header := fmt.Sprintf("  %-20s  %-10s  %-9s  %-7s  %s", "NAME", "MODE", "WORKTREES", "PORTS", "PATH")
		b.WriteString(subtitleStyle.Render(header))
		b.WriteString("\n")
		for i, r := range v.rows {
			mode := "lightweight"
			modeStyle := muted
			if r.configured {
				mode = "configured"
				modeStyle = statusOK
			}
			if r.err != "" {
				mode = "err"
				modeStyle = statusErr
			}
			line := fmt.Sprintf("  %-20s  %-10s  %-9d  %-7d  %s",
				r.name,
				modeStyle.Render(fmt.Sprintf("%-10s", mode)),
				r.worktrees,
				r.allocations,
				r.path,
			)
			if i == v.cursor {
				b.WriteString(selectedRow.Render("▸ " + strings.TrimPrefix(line, "  ")))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString(helpStyle.Render("↑↓ navigate  ⏎ open  [a] add project  [q] quit"))
	return b.String()
}
