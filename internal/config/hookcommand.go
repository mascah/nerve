package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// HookCommand is one post_create hook. In YAML it accepts either a bare scalar
// string (the legacy form, always foreground) or a mapping with an explicit
// background flag:
//
//	post_create:
//	  - direnv allow            # foreground: sequential, blocks boot
//	  - run: uv sync
//	    background: true        # background: detached, runs concurrently
//
// Foreground commands run synchronously in declared order before the worktree path
// is reported, so env-shapers like `direnv allow` are guaranteed to have taken
// effect by the time a session starts. Background commands are handed to a detached
// runner that executes them concurrently with each other and with session boot.
type HookCommand struct {
	Run        string
	Background bool
	// backgroundSet records whether the mapping form explicitly specified
	// `background`. When false, EffectiveBackground falls back to the (deprecated)
	// project-level background_post_create default. The bare-string form never sets
	// it, so a plain string defaults to foreground unless that project flag is on.
	backgroundSet bool
}

// Hook returns a foreground HookCommand — the programmatic equivalent of the bare
// string form in YAML.
func Hook(run string) HookCommand { return HookCommand{Run: run} }

// BackgroundHook returns a HookCommand explicitly marked to run in the background,
// equivalent to the `{run: ..., background: true}` YAML form.
func BackgroundHook(run string) HookCommand {
	return HookCommand{Run: run, Background: true, backgroundSet: true}
}

// EffectiveBackground reports whether this command should run in the background,
// honoring an explicit per-command `background:` and otherwise falling back to the
// deprecated project-level default (background_post_create).
func (h HookCommand) EffectiveBackground(projectDefault bool) bool {
	if h.backgroundSet {
		return h.Background
	}
	return projectDefault
}

// UnmarshalYAML accepts the scalar string form and the mapping form, so existing
// `post_create: [cmd, cmd]` configs keep parsing unchanged.
func (h *HookCommand) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		h.Run = value.Value
		h.Background = false
		h.backgroundSet = false
		return nil
	case yaml.MappingNode:
		var raw struct {
			Run        string `yaml:"run"`
			Background *bool  `yaml:"background"`
		}
		if err := value.Decode(&raw); err != nil {
			return err
		}
		if raw.Run == "" {
			return fmt.Errorf("hook command: 'run' is required in the object form")
		}
		h.Run = raw.Run
		if raw.Background != nil {
			h.Background = *raw.Background
			h.backgroundSet = true
		}
		return nil
	default:
		return fmt.Errorf("hook command: expected a string or a {run, background} mapping, got %v", value.Kind)
	}
}

// MarshalYAML round-trips back to the cleanest form: a bare string for foreground
// commands, the mapping only when background is set, so writing config out doesn't
// noise up the common case.
func (h HookCommand) MarshalYAML() (any, error) {
	if !h.backgroundSet && !h.Background {
		return h.Run, nil
	}
	return map[string]any{"run": h.Run, "background": h.Background}, nil
}

// HookCommands is the ordered post_create list.
type HookCommands []HookCommand

// Partition splits the list into the foreground commands (run synchronously, in
// order) and the background commands (run detached, concurrently), resolving each
// command's mode against the deprecated project-level default. Order within each
// group is preserved.
func (cs HookCommands) Partition(projectDefault bool) (foreground, background []string) {
	for _, c := range cs {
		if c.EffectiveBackground(projectDefault) {
			background = append(background, c.Run)
		} else {
			foreground = append(foreground, c.Run)
		}
	}
	return foreground, background
}

// ForegroundStrings returns the foreground command strings in order.
func (cs HookCommands) ForegroundStrings(projectDefault bool) []string {
	fg, _ := cs.Partition(projectDefault)
	return fg
}

// BackgroundStrings returns the background command strings in order.
func (cs HookCommands) BackgroundStrings(projectDefault bool) []string {
	_, bg := cs.Partition(projectDefault)
	return bg
}
