// Package tui implements the interactive project-configuration TUI launched by
// `nerve` with no arguments.
//
// Architecture: a top-level App holds the current view and delegates updates/
// renders to that view. Views send back navigation intents via switchViewMsg
// (or, where it makes sense, a tea.Quit).
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the TUI. Blocks until the user quits.
func Run() error {
	app, err := newApp()
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

// reloadMsg asks the current view to reload its underlying data (e.g. after a
// mutation written to disk by a sibling view).
type reloadMsg struct{}

// errMsg surfaces a non-fatal error as a transient banner.
type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

// App is the root bubbletea model.
type App struct {
	width, height int
	view          viewKey
	banner        string

	projects   *projectsView
	addProject *addProjectView
	project    *projectView
	addService *serviceForm
	addClone   *cloneForm
}

func newApp() (*App, error) {
	pv, err := newProjectsView()
	if err != nil {
		return nil, err
	}
	return &App{view: viewProjects, projects: pv}, nil
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
	if a.banner != "" {
		body += "\n" + statusErr.Render(a.banner)
	}
	return body
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
