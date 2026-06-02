package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

func TestFocusRingWraparound(t *testing.T) {
	inputs := []textinput.Model{textinput.New(), textinput.New()}
	r := newFocusRing(&inputs, 3) // stops: 0,1 = inputs, 2 = non-input

	if r.cur() != 0 {
		t.Fatalf("initial idx = %d, want 0", r.cur())
	}
	if !r.onInput() {
		t.Errorf("stop 0 should be an input stop")
	}

	r.next() // 1
	if r.cur() != 1 {
		t.Fatalf("after next idx = %d, want 1", r.cur())
	}
	if !inputs[1].Focused() {
		t.Errorf("input 1 should be focused at stop 1")
	}
	if inputs[0].Focused() {
		t.Errorf("input 0 should be blurred at stop 1")
	}

	r.next() // 2 (non-input)
	if r.cur() != 2 {
		t.Fatalf("after next idx = %d, want 2", r.cur())
	}
	if r.onInput() {
		t.Errorf("stop 2 should be a non-input stop")
	}
	if inputs[0].Focused() || inputs[1].Focused() {
		t.Errorf("no input should be focused at non-input stop 2")
	}

	r.next() // wrap to 0
	if r.cur() != 0 {
		t.Fatalf("after wrap next idx = %d, want 0", r.cur())
	}
	if !inputs[0].Focused() {
		t.Errorf("input 0 should be focused after wrapping to stop 0")
	}

	r.prev() // wrap back to 2
	if r.cur() != 2 {
		t.Fatalf("after wrap prev idx = %d, want 2", r.cur())
	}

	r.prev() // 1
	if r.cur() != 1 {
		t.Fatalf("after prev idx = %d, want 1", r.cur())
	}
}
