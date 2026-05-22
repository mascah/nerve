package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHookCommand_UnmarshalForms(t *testing.T) {
	const doc = `
post_create:
  - direnv allow
  - run: uv sync
    background: true
  - run: pnpm install
    background: false
  - run: just bootstrap
`
	var hooks LifecycleHooks
	if err := yaml.Unmarshal([]byte(doc), &hooks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(hooks.PostCreate) != 4 {
		t.Fatalf("expected 4 hooks, got %d", len(hooks.PostCreate))
	}

	cases := []struct {
		run        string
		background bool // explicit per-command value
		set        bool // whether `background:` was explicitly present
	}{
		{"direnv allow", false, false},   // bare string → foreground, not set
		{"uv sync", true, true},          // object, background: true
		{"pnpm install", false, true},    // object, background: false (explicit)
		{"just bootstrap", false, false}, // object without background key → not set
	}
	for i, want := range cases {
		got := hooks.PostCreate[i]
		if got.Run != want.run {
			t.Errorf("hook[%d].Run = %q, want %q", i, got.Run, want.run)
		}
		if got.Background != want.background {
			t.Errorf("hook[%d].Background = %v, want %v", i, got.Background, want.background)
		}
		if got.backgroundSet != want.set {
			t.Errorf("hook[%d].backgroundSet = %v, want %v", i, got.backgroundSet, want.set)
		}
	}
}

func TestHookCommand_UnmarshalMissingRun(t *testing.T) {
	const doc = `
post_create:
  - background: true
`
	var hooks LifecycleHooks
	err := yaml.Unmarshal([]byte(doc), &hooks)
	if err == nil {
		t.Fatal("expected error for object hook missing 'run', got nil")
	}
	if !strings.Contains(err.Error(), "'run' is required") {
		t.Errorf("error should mention 'run' is required: %v", err)
	}
}

func TestHookCommand_EffectiveBackground_InheritsProjectDefault(t *testing.T) {
	// A bare-string / unset command inherits the (deprecated) project default.
	plain := Hook("direnv allow")
	if plain.EffectiveBackground(false) {
		t.Error("unset hook with projectDefault=false should be foreground")
	}
	if !plain.EffectiveBackground(true) {
		t.Error("unset hook with projectDefault=true should inherit background")
	}

	// An explicit per-command value wins over the project default in both directions.
	bg := BackgroundHook("uv sync")
	if !bg.EffectiveBackground(false) {
		t.Error("explicit background:true should stay background even when projectDefault=false")
	}
	explicitFalse := HookCommand{Run: "direnv allow", Background: false, backgroundSet: true}
	if explicitFalse.EffectiveBackground(true) {
		t.Error("explicit background:false should override projectDefault=true")
	}
}

func TestHookCommands_Partition(t *testing.T) {
	hooks := HookCommands{
		Hook("direnv allow"),
		BackgroundHook("uv sync"),
		BackgroundHook("pnpm install"),
		Hook("echo done"),
	}
	fg, bg := hooks.Partition(false)
	if want := []string{"direnv allow", "echo done"}; !equalStrings(fg, want) {
		t.Errorf("foreground = %v, want %v", fg, want)
	}
	if want := []string{"uv sync", "pnpm install"}; !equalStrings(bg, want) {
		t.Errorf("background = %v, want %v", bg, want)
	}

	// With the deprecated project default on, the unset commands flip to background,
	// but explicit ones are unaffected (there are none here, so all become background).
	fg2, bg2 := hooks.Partition(true)
	if len(fg2) != 0 {
		t.Errorf("with projectDefault=true expected no foreground, got %v", fg2)
	}
	if len(bg2) != 4 {
		t.Errorf("with projectDefault=true expected all 4 background, got %v", bg2)
	}
}

func TestHookCommand_MarshalRoundTrip(t *testing.T) {
	// Foreground marshals back to a bare scalar; background to a mapping.
	fgOut, err := yaml.Marshal(Hook("direnv allow"))
	if err != nil {
		t.Fatalf("marshal foreground: %v", err)
	}
	if strings.Contains(string(fgOut), "run:") {
		t.Errorf("foreground hook should marshal as a bare string, got: %q", fgOut)
	}

	bgOut, err := yaml.Marshal(BackgroundHook("uv sync"))
	if err != nil {
		t.Fatalf("marshal background: %v", err)
	}
	if !strings.Contains(string(bgOut), "run: uv sync") || !strings.Contains(string(bgOut), "background: true") {
		t.Errorf("background hook should marshal as a {run, background} mapping, got: %q", bgOut)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
