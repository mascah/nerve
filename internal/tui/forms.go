package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mascah/nerve/internal/config"
)

// backToProject returns a command that switches back to the project view for
// the repo at repoRoot. It looks up the project's logical name from the global
// registry so the project view has both name and path.
//
// The registry read is synchronous on purpose: backToProject is only invoked
// from a form's Update on a user-initiated transition (esc/submit), not per
// keystroke, so the tiny read is fine inline.
func backToProject(repoRoot string) tea.Cmd {
	name := ""
	if reg, err := config.LoadGlobalRegistry(); err == nil {
		if e := reg.FindProjectByPath(repoRoot); e != nil {
			name = e.Name
		}
	}
	return func() tea.Msg {
		return switchViewMsg{to: viewProject, payload: projectPayload{name: name, path: repoRoot}}
	}
}

// focusRing models a circular focus index over a fixed set of stops. The first
// len(*inputs) stops correspond to text inputs (in order); the remaining stops
// (up to total) are non-input controls (toggles, selectors, submit). The ring
// owns the modular arithmetic and keeps the text inputs' focus state in sync.
type focusRing struct {
	inputs *[]textinput.Model
	total  int
	idx    int
}

// newFocusRing builds a ring over the given inputs slice with the given total
// number of stops. total must be >= len(*inputs).
func newFocusRing(inputs *[]textinput.Model, total int) focusRing {
	return focusRing{inputs: inputs, total: total}
}

// cur is the current focus stop.
func (r *focusRing) cur() int { return r.idx }

// next advances to the next stop (wrapping) and applies focus.
func (r *focusRing) next() {
	r.idx = (r.idx + 1) % r.total
	r.apply()
}

// prev moves to the previous stop (wrapping) and applies focus.
func (r *focusRing) prev() {
	r.idx = (r.idx + r.total - 1) % r.total
	r.apply()
}

// onInput reports whether the current stop is a text input.
func (r *focusRing) onInput() bool { return r.idx < len(*r.inputs) }

// apply blurs every input and focuses the current one iff it is an input stop.
func (r *focusRing) apply() {
	in := *r.inputs
	for i := range in {
		in[i].Blur()
	}
	if r.onInput() {
		in[r.idx].Focus()
	}
}
