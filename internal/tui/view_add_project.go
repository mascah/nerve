package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
)

// addProjectView prompts the user for a repo path (and optional logical name),
// then writes the global registry entry.
type addProjectView struct {
	pathInput textinput.Model
	nameInput textinput.Model
	focus     int // 0=path, 1=name, 2=submit
	status    string
}

func newAddProjectView() *addProjectView {
	cwd, _ := os.Getwd()
	pi := textinput.New()
	pi.Placeholder = "/path/to/repo"
	pi.Prompt = ""
	pi.SetValue(cwd)
	pi.Focus()
	pi.CharLimit = 256
	pi.Width = 60

	ni := textinput.New()
	ni.Placeholder = "(auto from dir name)"
	ni.Prompt = ""
	ni.CharLimit = 64
	ni.Width = 40

	return &addProjectView{pathInput: pi, nameInput: ni, focus: 0}
}

func (v *addProjectView) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "esc":
			return func() tea.Msg { return switchViewMsg{to: viewProjects} }
		case "tab":
			v.focus = (v.focus + 1) % 3
			v.applyFocus()
			return nil
		case "shift+tab":
			v.focus = (v.focus + 2) % 3
			v.applyFocus()
			return nil
		case "enter":
			if v.focus == 2 || v.focus == 1 {
				return v.submit()
			}
			v.focus = (v.focus + 1) % 3
			v.applyFocus()
			return nil
		}
	}
	var cmd tea.Cmd
	switch v.focus {
	case 0:
		v.pathInput, cmd = v.pathInput.Update(msg)
	case 1:
		v.nameInput, cmd = v.nameInput.Update(msg)
	}
	return cmd
}

func (v *addProjectView) applyFocus() {
	v.pathInput.Blur()
	v.nameInput.Blur()
	switch v.focus {
	case 0:
		v.pathInput.Focus()
	case 1:
		v.nameInput.Focus()
	}
}

func (v *addProjectView) submit() tea.Cmd {
	path := strings.TrimSpace(v.pathInput.Value())
	if path == "" {
		v.status = "path is required"
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		v.status = "bad path: " + err.Error()
		return nil
	}
	info, err := gitutil.Discover(abs)
	if err != nil {
		v.status = "not a git repo: " + err.Error()
		return nil
	}
	root := info.MainCheckout
	name := strings.TrimSpace(v.nameInput.Value())
	if name == "" {
		name = filepath.Base(root)
	}
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		v.status = "load registry: " + err.Error()
		return nil
	}
	if err := reg.AddProject(config.ProjectEntry{Name: name, Path: root}); err != nil {
		v.status = err.Error()
		return nil
	}
	if err := config.SaveGlobalRegistry(reg); err != nil {
		v.status = "save: " + err.Error()
		return nil
	}
	return func() tea.Msg { return switchViewMsg{to: viewProjects} }
}

func (v *addProjectView) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("nerve — add project"))
	b.WriteString("\n\n")
	b.WriteString(formLabel.Render("repo path") + "\n")
	b.WriteString(panelStyle.Render(v.pathInput.View()))
	b.WriteString("\n\n")
	b.WriteString(formLabel.Render("logical name (optional)") + "\n")
	b.WriteString(panelStyle.Render(v.nameInput.View()))
	b.WriteString("\n\n")
	submitLabel := "[ Submit ]"
	if v.focus == 2 {
		b.WriteString(selectedRow.Render(submitLabel))
	} else {
		b.WriteString(muted.Render(submitLabel))
	}
	if v.status != "" {
		b.WriteString("\n\n")
		b.WriteString(statusErr.Render(v.status))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("tab next  ⏎ submit  esc cancel"))
	return b.String()
}
