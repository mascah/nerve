package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mascah/nerve/internal/config"
)

// cloneForm adds a CloneFile entry: path, kind (file|directory|auto), required toggle.
type cloneForm struct {
	repoRoot string
	pathIn   textinput.Model
	kindIdx  int // 0=auto, 1=file, 2=directory
	required bool
	focus    int // 0=path, 1=kind, 2=required, 3=submit
	status   string
}

var kindOptions = []string{"auto", "file", "directory"}

func newCloneForm(repoRoot string) *cloneForm {
	pi := textinput.New()
	pi.Placeholder = ".env"
	pi.Prompt = ""
	pi.CharLimit = 256
	pi.Width = 50
	pi.Focus()
	return &cloneForm{repoRoot: repoRoot, pathIn: pi}
}

func (f *cloneForm) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "esc":
			return f.backToProject()
		case "tab":
			f.focus = (f.focus + 1) % 4
			f.applyFocus()
			return nil
		case "shift+tab":
			f.focus = (f.focus + 3) % 4
			f.applyFocus()
			return nil
		case "enter":
			if f.focus == 3 {
				return f.submit()
			}
			f.focus = (f.focus + 1) % 4
			f.applyFocus()
			return nil
		case " ":
			if f.focus == 2 {
				f.required = !f.required
				return nil
			}
		case "left", "h":
			if f.focus == 1 {
				f.kindIdx = (f.kindIdx + len(kindOptions) - 1) % len(kindOptions)
				return nil
			}
		case "right", "l":
			if f.focus == 1 {
				f.kindIdx = (f.kindIdx + 1) % len(kindOptions)
				return nil
			}
		}
	}
	if f.focus == 0 {
		var cmd tea.Cmd
		f.pathIn, cmd = f.pathIn.Update(msg)
		return cmd
	}
	return nil
}

func (f *cloneForm) applyFocus() {
	f.pathIn.Blur()
	if f.focus == 0 {
		f.pathIn.Focus()
	}
}

func (f *cloneForm) submit() tea.Cmd {
	path := strings.TrimSpace(f.pathIn.Value())
	if path == "" {
		f.status = "path is required"
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
	kind := kindOptions[f.kindIdx]
	if kind == "auto" {
		kind = ""
	}
	cfg.CloneFiles = append(cfg.CloneFiles, config.CloneFile{
		Path:     path,
		Kind:     kind,
		Required: f.required,
	})
	if err := config.SaveProjectConfig(f.repoRoot, cfg); err != nil {
		f.status = "save: " + err.Error()
		return nil
	}
	return f.backToProject()
}

func (f *cloneForm) backToProject() tea.Cmd {
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

func (f *cloneForm) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("nerve — add clone file"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(f.repoRoot))
	b.WriteString("\n\n")

	b.WriteString(formLabel.Render("path (relative to repo)") + "\n")
	b.WriteString(panelStyle.Render(f.pathIn.View()))
	b.WriteString("\n\n")

	b.WriteString(formLabel.Render("kind") + "\n")
	for i, k := range kindOptions {
		s := "  " + k + "  "
		if i == f.kindIdx {
			s = "[" + k + "]"
		}
		if f.focus == 1 {
			s = tabActive.Render(s)
		} else {
			s = tabInactive.Render(s)
		}
		b.WriteString(s)
	}
	b.WriteString("\n\n")

	reqLabel := "[ ] required (fail nerve new if missing)"
	if f.required {
		reqLabel = "[x] required (fail nerve new if missing)"
	}
	if f.focus == 2 {
		b.WriteString(selectedRow.Render(reqLabel))
	} else {
		b.WriteString(muted.Render(reqLabel))
	}
	b.WriteString("\n\n")

	submit := "[ Submit ]"
	if f.focus == 3 {
		b.WriteString(selectedRow.Render(submit))
	} else {
		b.WriteString(muted.Render(submit))
	}

	if f.status != "" {
		b.WriteString("\n\n")
		b.WriteString(statusErr.Render(f.status))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("tab next  ←→ kind  space toggle required  ⏎ submit  esc cancel"))
	return b.String()
}
