package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// isolateConfig points the global registry at a temp dir so tests don't touch the
// real ~/.config/nerve/projects.yaml.
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // belt-and-suspenders for UserHomeDir fallback
}

func TestAppLaunches(t *testing.T) {
	isolateConfig(t)
	app, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	if app.view != viewProjects {
		t.Errorf("initial view should be viewProjects, got %d", app.view)
	}
	out := app.View()
	if !strings.Contains(out, "nerve — projects") {
		t.Errorf("expected projects title in initial view; got:\n%s", out)
	}
	if !strings.Contains(out, "no projects registered") {
		t.Errorf("expected empty-state message; got:\n%s", out)
	}
}

func TestAppNavigateToAddProject(t *testing.T) {
	isolateConfig(t)
	app, err := newApp("")
	if err != nil {
		t.Fatal(err)
	}
	// Press 'a' to open the add-project form.
	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("expected cmd from pressing 'a'")
	}
	// Drain the cmd to get the switchViewMsg, then deliver it to the app.
	msg := cmd()
	app2, _ := updated.(*App).Update(msg)
	a := app2.(*App)
	if a.view != viewAddProject {
		t.Fatalf("expected to transition to viewAddProject, got %d", a.view)
	}
	out := a.View()
	if !strings.Contains(out, "add project") {
		t.Errorf("expected add-project header; got:\n%s", out)
	}
}

func TestServiceFormFieldsRender(t *testing.T) {
	isolateConfig(t)
	f := newServiceForm("/tmp/nerve-test-repo")
	out := f.View()
	for _, want := range []string{"add service", "id", "base_port", "env_key", "primary", "Submit"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in form view; got:\n%s", want, out)
		}
	}
}

func TestCloneFormFieldsRender(t *testing.T) {
	isolateConfig(t)
	f := newCloneForm("/tmp/nerve-test-repo")
	out := f.View()
	for _, want := range []string{"add clone file", "path", "kind", "required", "Submit"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in form view; got:\n%s", want, out)
		}
	}
}
