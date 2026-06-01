// Package hooks generates and merges the .claude/settings.json snippet that wires
// Claude Code's lifecycle hooks to nerve.
//
// The merge strategy is conservative: any user-written hooks in the same event
// arrays are preserved; nerve's own entries are tagged with a sentinel ("# nerve-managed")
// in their command string so Uninstall can find and remove only its own entries.
//
// Install/Uninstall operate directly on the generic map[string]any / []any tree
// loaded from settings.json. They never decode user entries into typed structs, so
// fields nerve doesn't model (e.g. Claude Code's per-hook "timeout") survive a
// round-trip untouched.
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
	// Matcher scopes a group to a tool (e.g. "Bash" for PreToolUse). omitempty keeps
	// the matcher-less lifecycle events (WorktreeCreate, SessionStart, …) serializing
	// exactly as before.
	Matcher string       `json:"matcher,omitempty"`
	Hooks   []singleHook `json:"hooks"`
}

// SettingsSnippet is the structure nerve produces; matches Claude Code's hook config shape.
type SettingsSnippet struct {
	Hooks map[string][]hookGroup `json:"hooks"`
}

// Snippet returns the canonical nerve hook entries. When bashPreamble is true it also
// includes the opt-in PreToolUse:Bash hook (`nerve bash-preamble`), which rewrites Bash
// commands to load the worktree env after Claude's EnterWorktree tool.
func Snippet(bashPreamble bool) SettingsSnippet {
	cmd := func(sub string) singleHook {
		return singleHook{Type: "command", Command: fmt.Sprintf("nerve %s %s", sub, sentinel)}
	}
	s := SettingsSnippet{
		Hooks: map[string][]hookGroup{
			"WorktreeCreate": {{Hooks: []singleHook{cmd("worktree-create")}}},
			"WorktreeRemove": {{Hooks: []singleHook{cmd("worktree-remove --from-hook")}}},
			"SessionStart":   {{Hooks: []singleHook{cmd("env --inject")}}},
			"CwdChanged":     {{Hooks: []singleHook{cmd("env --inject")}}},
		},
	}
	if bashPreamble {
		s.Hooks["PreToolUse"] = []hookGroup{{Matcher: "Bash", Hooks: []singleHook{cmd("bash-preamble")}}}
	}
	return s
}

// Install reads the existing settings.json at path (if present), merges in nerve's
// hook entries (idempotently), and returns the merged JSON as a string. When
// bashPreamble is true the opt-in PreToolUse:Bash hook is included.
//
// User-authored entries (and any fields nerve doesn't model, such as a per-hook
// "timeout") are preserved verbatim — the merge edits the generic tree in place
// rather than round-tripping through nerve's typed structs.
func Install(path string, bashPreamble bool) (string, error) {
	doc, err := loadJSON(path)
	if err != nil {
		return "", err
	}
	hooks, err := ensureHooksMap(doc)
	if err != nil {
		return "", err
	}
	for event, groups := range Snippet(bashPreamble).Hooks {
		existing, err := asAnySlice(hooks[event])
		if err != nil {
			return "", fmt.Errorf("hooks.%s: %w", event, err)
		}
		merged, err := mergeEvent(existing, groups)
		if err != nil {
			return "", fmt.Errorf("hooks.%s: %w", event, err)
		}
		hooks[event] = merged
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// Uninstall removes nerve-managed hook entries from path. Returns the resulting
// JSON, a flag indicating whether anything changed, and an error.
//
// Only entries whose command contains the sentinel are dropped; every other entry —
// and every field on it — is left untouched.
func Uninstall(path string) (string, bool, error) {
	doc, err := loadJSON(path)
	if err != nil {
		return "", false, err
	}
	hooks, err := ensureHooksMap(doc)
	if err != nil {
		return "", false, err
	}
	changed := false
	for event, raw := range hooks {
		groups, err := asAnySlice(raw)
		if err != nil {
			return "", false, fmt.Errorf("hooks.%s: %w", event, err)
		}
		cleaned := make([]any, 0, len(groups))
		for _, g := range groups {
			group, ok := g.(map[string]any)
			if !ok {
				// Unrecognized group shape — leave it intact.
				cleaned = append(cleaned, g)
				continue
			}
			inner, err := asAnySlice(group["hooks"])
			if err != nil {
				return "", false, fmt.Errorf("hooks.%s.hooks: %w", event, err)
			}
			keep := make([]any, 0, len(inner))
			for _, h := range inner {
				if hookIsNerve(h) {
					changed = true
					continue
				}
				keep = append(keep, h)
			}
			if len(keep) == 0 {
				continue
			}
			group["hooks"] = keep
			cleaned = append(cleaned, group)
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

// mergeEvent edits the existing event group slice (a generic []any of group maps),
// dropping any prior nerve-managed entries (so install is idempotent) and appending
// nerve's own entries. User entries and any fields nerve doesn't model are preserved.
func mergeEvent(existing []any, incoming []hookGroup) ([]any, error) {
	cleaned := make([]any, 0, len(existing)+len(incoming))
	for _, g := range existing {
		group, ok := g.(map[string]any)
		if !ok {
			// Unrecognized group shape — leave it intact.
			cleaned = append(cleaned, g)
			continue
		}
		inner, err := asAnySlice(group["hooks"])
		if err != nil {
			return nil, fmt.Errorf("hooks: %w", err)
		}
		keep := make([]any, 0, len(inner))
		for _, h := range inner {
			if hookIsNerve(h) {
				continue
			}
			keep = append(keep, h)
		}
		if len(keep) == 0 {
			continue
		}
		group["hooks"] = keep
		cleaned = append(cleaned, group)
	}
	// Append nerve's own entries as plain maps so they serialize like the rest of the doc.
	for _, g := range incoming {
		m, err := groupToMap(g)
		if err != nil {
			return nil, err
		}
		cleaned = append(cleaned, m)
	}
	return cleaned, nil
}

// groupToMap converts nerve's typed hookGroup into the generic map tree, so appended
// entries share the same shape as the rest of the document.
func groupToMap(g hookGroup) (map[string]any, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// hookIsNerve reports whether a generic hook entry is one of nerve's own (its command
// string contains the sentinel). Non-map entries and entries without a string command
// are treated as not-nerve and preserved.
func hookIsNerve(h any) bool {
	m, ok := h.(map[string]any)
	if !ok {
		return false
	}
	cmd, _ := m["command"].(string)
	return isNerveCommand(cmd)
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

// ensureHooksMap returns the "hooks" object, creating it when absent. A present-but-
// non-object "hooks" value is an error rather than silently discarded — overwriting it
// would lose user data.
func ensureHooksMap(doc map[string]any) (map[string]any, error) {
	v, ok := doc["hooks"]
	if !ok {
		m := map[string]any{}
		doc["hooks"] = m
		return m, nil
	}
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	return nil, fmt.Errorf("settings.json: %q is %T, expected an object", "hooks", v)
}

// asAnySlice coerces a generic JSON value into a []any so it can be edited in place.
// nil yields a nil slice. A present-but-non-array value is an error rather than
// silently dropped — overwriting it would lose user data.
func asAnySlice(v any) ([]any, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.([]any); ok {
		return s, nil
	}
	return nil, fmt.Errorf("expected an array, got %T", v)
}
