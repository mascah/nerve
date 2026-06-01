package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mascah/nerve/internal/config"
)

// cloneForm adds a CloneFile entry: path, kind (file|directory|auto), required toggle.
type cloneForm struct {
	repoRoot string
	inputs   []textinput.Model // len 1: [0]=path
	kindIdx  int               // 0=auto, 1=file, 2=directory
	required bool
	ring     focusRing // stops: 0=path, 1=kind, 2=required, 3=submit
	status   string

	// suggestions is the rolling autocomplete list shown below the path field
	// while focus == 0. suggestionCursor highlights one entry that the user
	// can accept with Tab or Enter (Enter does not submit while suggestions
	// are visible — it only inserts the suggestion).
	suggestions      []string
	suggestionCursor int
	// lastQuery caches the path-input value we last fed to listPathSuggestions
	// so we only re-walk the filesystem when the user actually changed it.
	lastQuery string
}

// maxPathSuggestions is the visible suggestion list size.
const maxPathSuggestions = 8

var kindOptions = []string{"auto", "file", "directory"}

func newCloneForm(repoRoot string) *cloneForm {
	pi := textinput.New()
	pi.Placeholder = ".env"
	pi.Prompt = ""
	pi.CharLimit = 256
	pi.Width = 50
	pi.Focus()
	f := &cloneForm{repoRoot: repoRoot, inputs: []textinput.Model{pi}}
	f.ring = newFocusRing(&f.inputs, 4)
	return f
}

func (f *cloneForm) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(tea.KeyMsg); ok {
		key := m.String()
		// Suggestion-list keys only activate while the path field has focus
		// AND there are suggestions to act on. They take precedence over the
		// generic form-navigation handlers below.
		if f.ring.cur() == 0 && len(f.suggestions) > 0 {
			switch key {
			case "down":
				if f.suggestionCursor < len(f.suggestions)-1 {
					f.suggestionCursor++
				}
				return nil
			case "up":
				if f.suggestionCursor > 0 {
					f.suggestionCursor--
				}
				return nil
			case "tab", "enter":
				// Accept the highlighted suggestion: replace input value,
				// move caret to the end, and clear the suggestion list so
				// subsequent Tab/Enter behave normally (next field / submit).
				pick := f.suggestions[f.suggestionCursor]
				f.inputs[0].SetValue(pick)
				f.inputs[0].CursorEnd()
				f.suggestions = nil
				f.suggestionCursor = 0
				f.lastQuery = pick
				return nil
			}
		}

		switch key {
		case "esc":
			return backToProject(f.repoRoot)
		case "tab":
			f.ring.next()
			return nil
		case "shift+tab":
			f.ring.prev()
			return nil
		case "enter":
			if f.ring.cur() == 3 {
				return f.submit()
			}
			f.ring.next()
			return nil
		case " ":
			if f.ring.cur() == 2 {
				f.required = !f.required
				return nil
			}
		case "left", "h":
			if f.ring.cur() == 1 {
				f.kindIdx = (f.kindIdx + len(kindOptions) - 1) % len(kindOptions)
				return nil
			}
		case "right", "l":
			if f.ring.cur() == 1 {
				f.kindIdx = (f.kindIdx + 1) % len(kindOptions)
				return nil
			}
		}
	}
	if f.ring.cur() == 0 {
		var cmd tea.Cmd
		f.inputs[0], cmd = f.inputs[0].Update(msg)
		f.refreshSuggestions()
		return cmd
	}
	return nil
}

// refreshSuggestions recomputes the autocomplete list from the current path
// input value. It is cheap when the value hasn't changed (cached via
// lastQuery) and bounded by listPathSuggestions's depth/scan ceilings when it
// does change.
func (f *cloneForm) refreshSuggestions() {
	q := f.inputs[0].Value()
	if q == f.lastQuery {
		return
	}
	f.lastQuery = q
	f.suggestions = listPathSuggestions(f.repoRoot, q, maxPathSuggestions)
	if f.suggestionCursor >= len(f.suggestions) {
		f.suggestionCursor = 0
	}
}

func (f *cloneForm) submit() tea.Cmd {
	path := strings.TrimSpace(f.inputs[0].Value())
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
	return backToProject(f.repoRoot)
}

func (f *cloneForm) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("nerve — add clone file"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(f.repoRoot))
	b.WriteString("\n\n")

	b.WriteString(formLabel.Render("path (relative to repo)") + "\n")
	b.WriteString(panelStyle.Render(f.inputs[0].View()))
	b.WriteString("\n")
	// Suggestion list — only rendered while the path field has focus and we
	// actually have matches. Up/down move the cursor; Tab/Enter accept.
	if f.ring.cur() == 0 && len(f.suggestions) > 0 {
		for i, sug := range f.suggestions {
			if i == f.suggestionCursor {
				b.WriteString(selectedRow.Render("▸ " + sug))
			} else {
				b.WriteString(muted.Render("  " + sug))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	b.WriteString(formLabel.Render("kind") + "\n")
	kindTabs := make([]string, 0, len(kindOptions))
	for i, k := range kindOptions {
		// Use the same plain label for every tab; styling (not bracketing)
		// distinguishes the selected option and the focused strip. This keeps
		// every tab the same width so the bottom border stays aligned.
		var s string
		switch {
		case i == f.kindIdx && f.ring.cur() == 1:
			s = tabKindActive.Render(k)
		case i == f.kindIdx:
			// Selected option, but focus is elsewhere — show it as active but
			// without the reverse-video focus emphasis.
			s = tabActive.Render(k)
		case f.ring.cur() == 1:
			s = tabInactive.Render(k)
		default:
			s = tabInactive.Render(k)
		}
		kindTabs = append(kindTabs, s)
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Bottom, kindTabs...))
	b.WriteString("\n\n")

	reqLabel := "[ ] required (fail nerve new if missing)"
	if f.required {
		reqLabel = "[x] required (fail nerve new if missing)"
	}
	if f.ring.cur() == 2 {
		b.WriteString(selectedRow.Render(reqLabel))
	} else {
		b.WriteString(muted.Render(reqLabel))
	}
	b.WriteString("\n\n")

	submit := "[ Submit ]"
	if f.ring.cur() == 3 {
		b.WriteString(selectedRow.Render(submit))
	} else {
		b.WriteString(muted.Render(submit))
	}

	if f.status != "" {
		b.WriteString("\n\n")
		b.WriteString(statusErr.Render(f.status))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("tab next  ↑↓ suggestion  ⏎/tab accept  ←→ kind  space required  esc cancel"))
	return b.String()
}
