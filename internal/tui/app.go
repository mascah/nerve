// Package tui implements the interactive project-configuration TUI launched by
// `nerve` with no arguments.
//
// Architecture: a top-level App holds the current view and delegates updates/
// renders to that view. Views send back navigation intents via switchViewMsg
// (or, where it makes sense, a tea.Quit).
package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mascah/nerve/internal/envinject"
	"github.com/mascah/nerve/internal/gitutil"
)

// Run starts the TUI. cwd is the directory the binary was launched from; it's used
// once at startup to surface the current worktree's ports. Blocks until the user quits.
func Run(cwd string) error {
	app, err := newApp(cwd)
	if err != nil {
		return err
	}
	prog := tea.NewProgram(app, tea.WithAltScreen())
	_, err = prog.Run()
	return err
}

type viewKey int

const (
	viewProjects viewKey = iota
	viewAddProject
	viewProject
	viewAddService
	viewAddClone
)

// switchViewMsg requests a view change. Optional payload carries context (e.g.
// the project being edited, or a refresh hint).
type switchViewMsg struct {
	to      viewKey
	payload any
}

// errMsg surfaces a non-fatal error as a transient banner.
type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

// currentWorktreeInfo captures, once at startup, the worktree the TUI was launched
// from (when that's a configured worktree). Rendered as a header so the user can see
// the running tree's service ports without leaving the projects overview.
type currentWorktreeInfo struct {
	Branch string
	Path   string
	Ports  map[string]string // EnvKey → port string
}

// App is the root bubbletea model.
type App struct {
	width, height int
	view          viewKey
	banner        string

	// current is the worktree the TUI was launched from, or nil when launched from
	// the main checkout / outside a worktree. Captured once in newApp.
	current *currentWorktreeInfo

	projects   *projectsView
	addProject *addProjectView
	project    *projectView
	addService *serviceForm
	addClone   *cloneForm
}

func newApp(cwd string) (*App, error) {
	pv, err := newProjectsView()
	if err != nil {
		return nil, err
	}
	return &App{view: viewProjects, projects: pv, current: detectCurrentWorktree(cwd)}, nil
}

// detectCurrentWorktree inspects cwd once at startup. Returns nil unless cwd is inside
// a configured worktree with an allocation (envinject.Compute returns non-empty). This
// is the only synchronous git work done on launch and runs exactly once — never per
// keypress.
func detectCurrentWorktree(cwd string) *currentWorktreeInfo {
	if cwd == "" {
		return nil
	}
	info, err := gitutil.Discover(cwd)
	if err != nil || !info.IsWorktree {
		return nil
	}
	ports, err := envinject.Compute(cwd)
	if err != nil || len(ports) == 0 {
		return nil
	}
	branch := branchForWorktree(info.MainCheckout, info.CurrentWorktree)
	if branch == "" {
		branch = filepath.Base(info.CurrentWorktree)
	}
	return &currentWorktreeInfo{Branch: branch, Path: info.CurrentWorktree, Ports: ports}
}

// branchForWorktree matches wtPath against the repo's worktree list to recover its
// branch name. Returns "" when no match is found (detached HEAD or transient state).
func branchForWorktree(mainCheckout, wtPath string) string {
	canonTarget, err := gitutil.CanonicalPath(wtPath)
	if err != nil {
		canonTarget = wtPath
	}
	wts, err := gitutil.ListWorktrees(mainCheckout)
	if err != nil {
		return ""
	}
	for _, wt := range wts {
		canon, err := gitutil.CanonicalPath(wt.Path)
		if err != nil {
			canon = wt.Path
		}
		if canon == canonTarget {
			return wt.Branch
		}
	}
	return ""
}

func (a *App) Init() tea.Cmd { return nil }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
	case errMsg:
		a.banner = "error: " + m.err.Error()
		return a, nil
	case switchViewMsg:
		return a.switchTo(m)
	}

	var cmd tea.Cmd
	switch a.view {
	case viewProjects:
		cmd = a.projects.Update(msg)
	case viewAddProject:
		cmd = a.addProject.Update(msg)
	case viewProject:
		cmd = a.project.Update(msg)
	case viewAddService:
		cmd = a.addService.Update(msg)
	case viewAddClone:
		cmd = a.addClone.Update(msg)
	}
	return a, cmd
}

func (a *App) View() string {
	var body string
	switch a.view {
	case viewProjects:
		body = a.projects.View()
	case viewAddProject:
		body = a.addProject.View()
	case viewProject:
		body = a.project.View()
	case viewAddService:
		body = a.addService.View()
	case viewAddClone:
		body = a.addClone.View()
	}
	if header := a.current.render(); header != "" {
		body = header + "\n" + body
	}
	if a.banner != "" {
		body += "\n" + statusErr.Render(a.banner)
	}
	return body
}

// render returns the styled current-worktree header line, or "" when there's no
// current worktree. Env keys are sorted for a stable rendering.
func (c *currentWorktreeInfo) render() string {
	if c == nil || len(c.Ports) == 0 {
		return ""
	}
	keys := make([]string, 0, len(c.Ports))
	for k := range c.Ports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, c.Ports[k]))
	}
	label := titleStyle.Render(fmt.Sprintf("▸ current worktree: %s", c.Branch))
	return label + "  " + muted.Render("·  "+strings.Join(pairs, "  "))
}

func (a *App) switchTo(msg switchViewMsg) (tea.Model, tea.Cmd) {
	a.banner = ""
	a.view = msg.to
	switch msg.to {
	case viewProjects:
		// Refresh on return.
		pv, err := newProjectsView()
		if err != nil {
			return a, func() tea.Msg { return errMsg{err} }
		}
		a.projects = pv
	case viewAddProject:
		a.addProject = newAddProjectView()
	case viewProject:
		entry, ok := msg.payload.(projectPayload)
		if !ok {
			return a, func() tea.Msg { return errMsg{fmt.Errorf("internal: missing project payload")} }
		}
		pv, err := newProjectView(entry.name, entry.path)
		if err != nil {
			return a, func() tea.Msg { return errMsg{err} }
		}
		a.project = pv
		// Load worktrees eagerly off the UI loop so the count shows on the tab label
		// without the user having to tab onto the Worktrees tab (and without freezing
		// the UI while git forks subprocesses).
		a.project.loadingWorktrees = true
		return a, a.project.loadWorktreesCmd()
	case viewAddService:
		repoRoot, ok := msg.payload.(string)
		if !ok {
			return a, func() tea.Msg { return errMsg{fmt.Errorf("internal: missing repoRoot for service form")} }
		}
		a.addService = newServiceForm(repoRoot)
	case viewAddClone:
		repoRoot, ok := msg.payload.(string)
		if !ok {
			return a, func() tea.Msg { return errMsg{fmt.Errorf("internal: missing repoRoot for clone form")} }
		}
		a.addClone = newCloneForm(repoRoot)
	}
	return a, nil
}

// projectPayload is carried by switchViewMsg when transitioning into the
// project-detail view.
type projectPayload struct {
	name string
	path string
}
