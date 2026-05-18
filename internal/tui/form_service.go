package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mascah/nerve/internal/config"
)

// serviceForm is a 4-field form for adding a Service to a project's config:
// id, base_port, env_key, primary (toggle).
type serviceForm struct {
	repoRoot string
	inputs   []textinput.Model
	primary  bool
	focus    int // 0..3=inputs, 4=primary toggle, 5=submit
	status   string
}

func newServiceForm(repoRoot string) *serviceForm {
	mk := func(placeholder string, width int) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.Prompt = ""
		ti.CharLimit = 64
		ti.Width = width
		return ti
	}
	f := &serviceForm{
		repoRoot: repoRoot,
		inputs: []textinput.Model{
			mk("django", 30),
			mk("8000", 10),
			mk("DOCKER_HOST_DJANGO_PORT", 40),
		},
	}
	f.inputs[0].Focus()
	return f
}

func (f *serviceForm) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "esc":
			return f.backToProject()
		case "tab":
			f.focus = (f.focus + 1) % 5
			f.applyFocus()
			return nil
		case "shift+tab":
			f.focus = (f.focus + 4) % 5
			f.applyFocus()
			return nil
		case "enter":
			if f.focus == 4 {
				return f.submit()
			}
			f.focus = (f.focus + 1) % 5
			f.applyFocus()
			return nil
		case " ":
			if f.focus == 3 {
				f.primary = !f.primary
				return nil
			}
		}
	}
	var cmd tea.Cmd
	if f.focus >= 0 && f.focus < len(f.inputs) {
		f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	}
	return cmd
}

func (f *serviceForm) applyFocus() {
	for i := range f.inputs {
		f.inputs[i].Blur()
	}
	if f.focus >= 0 && f.focus < len(f.inputs) {
		f.inputs[f.focus].Focus()
	}
}

func (f *serviceForm) submit() tea.Cmd {
	id := strings.TrimSpace(f.inputs[0].Value())
	basePortStr := strings.TrimSpace(f.inputs[1].Value())
	envKey := strings.TrimSpace(f.inputs[2].Value())
	if id == "" || basePortStr == "" || envKey == "" {
		f.status = "id, base_port, and env_key are required"
		return nil
	}
	basePort, err := strconv.Atoi(basePortStr)
	if err != nil {
		f.status = "base_port must be an integer"
		return nil
	}
	cfg, err := config.LoadProjectConfig(f.repoRoot)
	if err != nil {
		if err == config.ErrNotFound {
			fresh := config.Defaults()
			cfg = &fresh
		} else {
			f.status = "load config: " + err.Error()
			return nil
		}
	}
	// If marking this primary, demote any existing primary.
	if f.primary {
		for i := range cfg.Services {
			cfg.Services[i].Primary = false
		}
	}
	cfg.Services = append(cfg.Services, config.Service{
		ID:       id,
		BasePort: basePort,
		EnvKey:   envKey,
		Primary:  f.primary,
	})
	if err := config.SaveProjectConfig(f.repoRoot, cfg); err != nil {
		f.status = "save: " + err.Error()
		return nil
	}
	return f.backToProject()
}

func (f *serviceForm) backToProject() tea.Cmd {
	// We need the project name + path to switch back to viewProject. We have
	// the path; look up the name from the global registry to be exact.
	name := ""
	if reg, err := config.LoadGlobalRegistry(); err == nil {
		if e := reg.FindProjectByPath(f.repoRoot); e != nil {
			name = e.Name
		}
	}
	return func() tea.Msg {
		return switchViewMsg{to: viewProject, payload: projectPayload{name: name, path: f.repoRoot}}
	}
}

func (f *serviceForm) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("nerve — add service"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(f.repoRoot))
	b.WriteString("\n\n")

	labels := []string{"id", "base_port", "env_key"}
	for i, l := range labels {
		b.WriteString(formLabel.Render(l) + "\n")
		b.WriteString(panelStyle.Render(f.inputs[i].View()))
		b.WriteString("\n\n")
	}

	primaryLabel := "[ ] mark primary (drives port pool start)"
	if f.primary {
		primaryLabel = "[x] mark primary (drives port pool start)"
	}
	if f.focus == 3 {
		b.WriteString(selectedRow.Render(primaryLabel))
	} else {
		b.WriteString(muted.Render(primaryLabel))
	}
	b.WriteString("\n\n")

	submit := "[ Submit ]"
	if f.focus == 4 {
		b.WriteString(selectedRow.Render(submit))
	} else {
		b.WriteString(muted.Render(submit))
	}

	if f.status != "" {
		b.WriteString("\n\n")
		b.WriteString(statusErr.Render(f.status))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("tab next  space toggle primary  ⏎ submit  esc cancel"))
	_ = fmt.Sprintf
	return b.String()
}
