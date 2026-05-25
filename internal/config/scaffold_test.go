package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestScaffold_NonEmpty guards the embed: an empty scaffold would silently ship a
// useless init.
func TestScaffold_NonEmpty(t *testing.T) {
	if len(Scaffold()) == 0 {
		t.Fatal("Scaffold() returned empty bytes")
	}
}

// TestScaffold_AsShippedParsesToValidLightweightConfig confirms the scaffold as
// shipped (comments intact, only version: + project: active) parses and validates,
// and that it produces a lightweight config (no services/clone_files/templates).
func TestScaffold_AsShippedParsesToValidLightweightConfig(t *testing.T) {
	var cfg ProjectConfig
	if err := yaml.Unmarshal(Scaffold(), &cfg); err != nil {
		t.Fatalf("as-shipped scaffold failed to unmarshal: %v", err)
	}
	ApplyDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		t.Fatalf("as-shipped scaffold failed to validate: %v", err)
	}
	if cfg.IsConfigured() {
		t.Errorf("as-shipped scaffold should be lightweight (no services/clone_files/templates), got configured: %+v", cfg)
	}
	if cfg.Version != CurrentConfigVersion {
		t.Errorf("scaffold version = %d, want %d", cfg.Version, CurrentConfigVersion)
	}
}

// scaffoldExampleSection is a fully-uncommented copy of the example sections shown
// COMMENTED in config.scaffold.yaml. It is the drift guard's parallel fixture: when
// you change an example in the scaffold, change it here too, and this test proves
// the shape still unmarshals + validates against the current structs (catching e.g.
// a stale src/dst or bare-string clone_file). TestScaffold_FixtureMirrorsScaffold
// keeps the two from silently diverging by asserting every fixture key also appears
// (commented) in the scaffold bytes.
const scaffoldExampleSection = `
version: 1

project:
    port_offset: 0
    worktree_root: .worktrees/{branch}
    pool_size: 10
    background_post_create: false
    background_remove: false
    bash_preamble: 'eval "$(direnv export bash 2>/dev/null)"'

services:
    - id: django
      base_port: 8000
      env_key: DJANGO_PORT
      primary: true
    - id: postgres
      base_port: 5432
      env_key: DATABASE_PORT
    - id: vite
      base_port: 5173
      env_key: VITE_PORT

vars:
    - { env_key: WORKTREE_ID, value: "{{branch}}" }

clone_files:
    - { path: .env,   kind: file, required: true }
    - { path: .npmrc, kind: file, required: false }

templates:
    - { source: .env.example, dest: .env.local, merge: true }

hooks:
    post_create:
        - direnv allow
        - { run: uv sync, background: true }
        - { run: pnpm install, background: true }
    pre_remove:
        - "echo pre-remove ran"
`

// TestScaffold_UncommentedExamplesValidate is the drift guard: the fully-uncommented
// fixture (mirroring the scaffold's commented examples) must unmarshal + validate.
// This catches the scaffold/fixture rotting away from the structs.
func TestScaffold_UncommentedExamplesValidate(t *testing.T) {
	var cfg ProjectConfig
	if err := yaml.Unmarshal([]byte(scaffoldExampleSection), &cfg); err != nil {
		t.Fatalf("uncommented example fixture failed to unmarshal: %v", err)
	}
	ApplyDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		t.Fatalf("uncommented example fixture failed to validate: %v", err)
	}

	// Sanity: the fixture must actually exercise every optional section, so the test
	// can't pass vacuously.
	if len(cfg.Services) == 0 {
		t.Error("fixture has no services")
	}
	if len(cfg.Vars) == 0 {
		t.Error("fixture has no vars")
	}
	if len(cfg.CloneFiles) == 0 {
		t.Error("fixture has no clone_files")
	}
	if len(cfg.Templates) == 0 {
		t.Error("fixture has no templates")
	}
	if len(cfg.Hooks.PostCreate) == 0 {
		t.Error("fixture has no post_create hooks")
	}
	if len(cfg.Hooks.PreRemove) == 0 {
		t.Error("fixture has no pre_remove hooks")
	}
	// The post_create examples document both forms; assert at least one foreground and
	// one background hook so both the bare-string and {run, background: true} forms
	// stay exercised.
	fg, bg := cfg.Hooks.PostCreate.Partition(false)
	if len(fg) == 0 {
		t.Error("fixture has no foreground post_create hook (bare-string form unexercised)")
	}
	if len(bg) == 0 {
		t.Error("fixture has no background post_create hook ({run, background: true} form unexercised)")
	}
}

// TestScaffold_FixtureMirrorsScaffold keeps the parallel fixture honest: every YAML
// key the fixture exercises must also appear in the as-shipped scaffold (as a
// commented example or active line). If someone edits the scaffold's example keys
// without updating the fixture (or vice versa), this fails — so the drift guard
// can't be defeated by the fixture quietly diverging from what we ship.
func TestScaffold_FixtureMirrorsScaffold(t *testing.T) {
	shipped := string(Scaffold())
	// Tokens that must be documented in the scaffold. Cover every key the issue
	// requires the scaffold to document.
	tokens := []string{
		"port_offset:", "worktree_root:", "pool_size:",
		"background_post_create:", "background_remove:", "bash_preamble:",
		"services:", "id:", "base_port:", "env_key:", "primary:",
		"vars:", "value:",
		"clone_files:", "path:", "kind:", "required:",
		"templates:", "source:", "dest:", "merge:",
		"hooks:", "post_create:", "pre_remove:", "background:", "run:",
	}
	for _, tok := range tokens {
		if !strings.Contains(shipped, tok) {
			t.Errorf("scaffold is missing documented key %q", tok)
		}
	}
}
