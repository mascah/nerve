// Package hooks generates and merges the .claude/settings.json snippet that wires
// Claude Code's lifecycle hooks to nerve.
//
// The merge strategy is conservative: any user-written hooks in the same event
// arrays are preserved; nerve's own entries are tagged with a sentinel ("nerve")
// in their command string so Uninstall can find and remove only its own entries.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Each hook event uses the same nerve command. The "# nerve-managed" sentinel makes
// uninstall reliable without storing extra state.
const sentinel = "# nerve-managed"

type singleHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type hookGroup struct {
	Hooks []singleHook `json:"hooks"`
}

// SettingsSnippet is the structure nerve produces; matches Claude Code's hook config shape.
type SettingsSnippet struct {
	Hooks map[string][]hookGroup `json:"hooks"`
}

// Snippet returns the canonical nerve hook entries.
func Snippet() SettingsSnippet {
	cmd := func(sub string) singleHook {
		return singleHook{Type: "command", Command: fmt.Sprintf("nerve %s %s", sub, sentinel)}
	}
	return SettingsSnippet{
		Hooks: map[string][]hookGroup{
			"WorktreeCreate": {{Hooks: []singleHook{cmd("worktree-create")}}},
			"WorktreeRemove": {{Hooks: []singleHook{cmd("worktree-remove --from-hook")}}},
			"SessionStart":   {{Hooks: []singleHook{cmd("env --inject")}}},
			"CwdChanged":     {{Hooks: []singleHook{cmd("env --inject")}}},
		},
	}
}

// Install reads the existing settings.json at path (if present), merges in nerve's
// hook entries (idempotently), and returns the merged JSON as a string.
func Install(path string) (string, error) {
	doc, err := loadJSON(path)
	if err != nil {
		return "", err
	}
	hooks := ensureHooksMap(doc)
	for event, groups := range Snippet().Hooks {
		hooks[event] = mergeEvent(asGroupSlice(hooks[event]), groups)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// Uninstall removes nerve-managed hook entries from path. Returns the resulting
// JSON, a flag indicating whether anything changed, and an error.
func Uninstall(path string) (string, bool, error) {
	doc, err := loadJSON(path)
	if err != nil {
		return "", false, err
	}
	hooks := ensureHooksMap(doc)
	changed := false
	for event, raw := range hooks {
		groups := asGroupSlice(raw)
		cleaned := make([]hookGroup, 0, len(groups))
		for _, g := range groups {
			keep := make([]singleHook, 0, len(g.Hooks))
			for _, h := range g.Hooks {
				if !isNerveCommand(h.Command) {
					keep = append(keep, h)
				} else {
					changed = true
				}
			}
			if len(keep) > 0 {
				cleaned = append(cleaned, hookGroup{Hooks: keep})
			}
		}
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}
	if !changed {
		return "", false, nil
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", true, err
	}
	return string(out) + "\n", true, nil
}

func mergeEvent(existing, incoming []hookGroup) []hookGroup {
	// Drop any prior nerve-managed entries (treat install as idempotent), then append.
	cleaned := make([]hookGroup, 0, len(existing))
	for _, g := range existing {
		keep := make([]singleHook, 0, len(g.Hooks))
		for _, h := range g.Hooks {
			if !isNerveCommand(h.Command) {
				keep = append(keep, h)
			}
		}
		if len(keep) > 0 {
			cleaned = append(cleaned, hookGroup{Hooks: keep})
		}
	}
	return append(cleaned, incoming...)
}

func isNerveCommand(cmd string) bool { return strings.Contains(cmd, sentinel) }

func loadJSON(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func ensureHooksMap(doc map[string]any) map[string]any {
	v, ok := doc["hooks"]
	if !ok {
		m := map[string]any{}
		doc["hooks"] = m
		return m
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	// Unexpected type — start fresh.
	m := map[string]any{}
	doc["hooks"] = m
	return m
}

// asGroupSlice converts a generic JSON value back into []hookGroup so we can edit it.
// Errors during round-trip cause the entry to be treated as empty.
func asGroupSlice(v any) []hookGroup {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out []hookGroup
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
