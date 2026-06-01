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
	// loaded is true once the async per-project status load has completed; until then
	// the rows carry only name/path (enough for navigation) and View shows "loading…".
	loaded bool
}

type projectRow struct {
	name        string
	path        string
	configured  bool
	worktrees   int
	allocations int
	err         string // non-empty when the row failed to load
}

// projectsLoadedMsg delivers the result of the async per-project status load (the
// gitutil.Discover + ListWorktrees + flock'd registry read done once per project, which
// used to run synchronously inside Update and freeze the UI loop).
type projectsLoadedMsg struct {
	rows []projectRow
	err  error
}

// newProjectsView reads the (cheap) global project registry and builds skeleton rows
// carrying only name/path — enough to navigate. The heavy per-project status (git
// discovery, worktree listing, registry read) is loaded off the UI loop via loadCmd so
// the event loop never blocks. Caller is responsible for firing loadCmd.
func newProjectsView() (*projectsView, error) {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return nil, err
	}
	rows := make([]projectRow, 0, len(reg.Projects))
	for _, p := range reg.Projects {
		rows = append(rows, projectRow{name: p.Name, path: p.Path})
	}
	return &projectsView{rows: rows}, nil
}

// loadCmd returns a tea.Cmd that computes each project's status off the UI loop and
// delivers a projectsLoadedMsg. Safe to run in a goroutine — it touches only the global
// registry (read again for a stable snapshot) and per-project git/registry state.
func (v *projectsView) loadCmd() tea.Cmd {
	return func() tea.Msg {
		rows, err := loadProjectRows()
		return projectsLoadedMsg{rows: rows, err: err}
	}
}

// loadProjectRows is the synchronous body invoked from loadCmd's goroutine (never from
// Update/View). For each registered project it forks git (Discover + ListWorktrees) and
// reads the flock'd per-project registry — work that must stay off the UI loop.
func loadProjectRows() ([]projectRow, error) {
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
	return rows, nil
}

func (v *projectsView) Update(msg tea.Msg) tea.Cmd {
	// The async status load arrives as a non-key message; handle it before the key
	// type-assertion drops everything that isn't a keypress.
	if m, ok := msg.(projectsLoadedMsg); ok {
		v.loaded = true
		if m.err == nil {
			v.rows = m.rows
			if v.cursor >= len(v.rows) && v.cursor > 0 {
				v.cursor = len(v.rows) - 1
			}
		}
		return nil
	}

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
	} else if !v.loaded {
		// Per-project status (mode/worktrees/ports) loads off the UI loop via loadCmd;
		// show a placeholder until projectsLoadedMsg lands so Update never blocks.
		b.WriteString(muted.Render("loading projects…"))
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
