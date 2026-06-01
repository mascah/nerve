package worktree

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/config"
)

// TestRenderEnv_IncludesVarsAndRendersTemplates guards the regression where
// `nerve refresh` rewrote .env.local from service ports only — dropping every
// `vars` entry and never re-rendering templates, despite its docstring. Both
// create and refresh now funnel through RenderEnv, so this asserts the shared
// path writes ports AND vars and re-renders templates.
func TestRenderEnv_IncludesVarsAndRendersTemplates(t *testing.T) {
	repo := t.TempDir()
	wt := filepath.Join(repo, ".worktrees", "feat-x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}

	// A template that interpolates a per-worktree port.
	if err := os.WriteFile(filepath.Join(repo, ".env.example"), []byte("WEB_FROM_TEMPLATE={{ports.web}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.ProjectConfig{
		Services: []config.Service{
			{ID: "web", BasePort: 8000, EnvKey: "APP_PORT", Primary: true},
			{ID: "db", BasePort: 5432, EnvKey: "DB_PORT"},
		},
		Vars: []config.Var{
			{EnvKey: "GIT_LOCATION", Value: "wt:{{branch}}:{{ports.web}}"},
		},
		Templates: []config.Template{
			{Source: ".env.example", Dest: ".env.fromtmpl"},
		},
	}
	portByService := map[string]int{"web": 8001, "db": 5433}

	env, err := RenderEnv(repo, wt, "feat-x", "demo", "feat_x", portByService, cfg, io.Discard)
	if err != nil {
		t.Fatalf("RenderEnv: %v", err)
	}

	// Returned env map carries ports AND the rendered var.
	want := map[string]string{"APP_PORT": "8001", "DB_PORT": "5433", "GIT_LOCATION": "wt:feat-x:8001"}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%q] = %q, want %q", k, env[k], v)
		}
	}

	// .env.local on disk must contain the var (the dropped-on-refresh regression).
	body, err := os.ReadFile(filepath.Join(wt, ".env.local"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"APP_PORT=8001", "GIT_LOCATION", "wt:feat-x:8001"} {
		if !strings.Contains(string(body), sub) {
			t.Errorf(".env.local missing %q; got:\n%s", sub, body)
		}
	}

	// The template was re-rendered with the per-worktree port.
	tmpl, err := os.ReadFile(filepath.Join(wt, ".env.fromtmpl"))
	if err != nil {
		t.Fatalf("template not rendered: %v", err)
	}
	if got := strings.TrimSpace(string(tmpl)); got != "WEB_FROM_TEMPLATE=8001" {
		t.Errorf("rendered template = %q, want %q", got, "WEB_FROM_TEMPLATE=8001")
	}
}
