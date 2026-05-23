package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallOnEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	out, err := Install(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nerve worktree-create") {
		t.Errorf("expected worktree-create hook in output, got:\n%s", out)
	}
	if !strings.Contains(out, "nerve env --inject") {
		t.Errorf("expected env --inject in output, got:\n%s", out)
	}
}

func TestInstallPreservesUserHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	existing := `{
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "echo user-hook"}]}
    ],
    "OtherEvent": [
      {"hooks": [{"type": "command", "command": "echo unrelated"}]}
    ]
  },
  "unrelated": {"foo": "bar"}
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Install(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "echo user-hook") {
		t.Errorf("user hook lost during install:\n%s", out)
	}
	if !strings.Contains(out, "echo unrelated") {
		t.Errorf("unrelated event lost during install:\n%s", out)
	}
	if !strings.Contains(out, `"foo": "bar"`) {
		t.Errorf("unrelated top-level key lost during install:\n%s", out)
	}
	if !strings.Contains(out, "nerve env --inject # nerve-managed") {
		t.Errorf("nerve hook not added:\n%s", out)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	first, err := Install(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Install(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("install not idempotent. first:\n%s\nsecond:\n%s", first, second)
	}
}

func TestUninstallRemovesOnlyNerve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if _, err := Install(path, false); err != nil {
		t.Fatal(err)
	}
	// Reinstall with extra user hooks.
	combined := `{
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "echo user-hook"}]},
      {"hooks": [{"type": "command", "command": "nerve env --inject # nerve-managed"}]}
    ],
    "CwdChanged": [
      {"hooks": [{"type": "command", "command": "nerve env --inject # nerve-managed"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(combined), 0o644); err != nil {
		t.Fatal(err)
	}
	out, changed, err := Uninstall(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected uninstall to report change")
	}
	if !strings.Contains(out, "echo user-hook") {
		t.Errorf("user hook lost during uninstall:\n%s", out)
	}
	if strings.Contains(out, "nerve env --inject") {
		t.Errorf("nerve hook still present after uninstall:\n%s", out)
	}
	// CwdChanged had only nerve entries; the whole event should be gone.
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if _, present := hooks["CwdChanged"]; present {
		t.Errorf("CwdChanged should be removed when its only entries were nerve-managed")
	}
}

func TestUninstallNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"hooks": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, changed, err := Uninstall(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Errorf("uninstall should be no-op when no nerve hooks present")
	}
}

func TestInstallBashPreambleOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Default install: no PreToolUse / bash-preamble.
	out, err := Install(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "bash-preamble") || strings.Contains(out, "PreToolUse") {
		t.Errorf("default install should not include bash-preamble:\n%s", out)
	}

	// Opt-in install: PreToolUse:Bash → bash-preamble, sentinel preserved.
	out, err = Install(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nerve bash-preamble # nerve-managed") {
		t.Errorf("opt-in install should include bash-preamble hook:\n%s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	groups := eventGroups(t, doc, "PreToolUse")
	if len(groups) != 1 {
		t.Fatalf("expected 1 PreToolUse group, got %d", len(groups))
	}
	if groups[0]["matcher"] != "Bash" {
		t.Errorf("expected PreToolUse matcher \"Bash\", got %v", groups[0]["matcher"])
	}
}

// TestInstallLifecycleEventsHaveNoMatcher guards the omitempty on hookGroup.Matcher:
// the four matcher-less lifecycle events must still serialize without a "matcher" key.
func TestInstallLifecycleEventsHaveNoMatcher(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	out, err := Install(path, true)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"WorktreeCreate", "WorktreeRemove", "SessionStart", "CwdChanged"} {
		for _, g := range eventGroups(t, doc, event) {
			if _, present := g["matcher"]; present {
				t.Errorf("%s group should have no matcher key, got %v", event, g)
			}
		}
	}
}

// TestUninstallPreservesSiblingMatcher checks that removing the nerve bash-preamble
// hook leaves a user's own PreToolUse hook — and its matcher — intact.
func TestUninstallPreservesSiblingMatcher(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	combined := `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Write", "hooks": [{"type": "command", "command": "echo user-write-hook"}]},
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "nerve bash-preamble # nerve-managed"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(combined), 0o644); err != nil {
		t.Fatal(err)
	}
	out, changed, err := Uninstall(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected uninstall to report change")
	}
	if strings.Contains(out, "bash-preamble") {
		t.Errorf("nerve bash-preamble should be removed:\n%s", out)
	}
	if !strings.Contains(out, "echo user-write-hook") {
		t.Errorf("user PreToolUse hook lost during uninstall:\n%s", out)
	}
	if !strings.Contains(out, `"matcher": "Write"`) {
		t.Errorf("user hook's matcher lost during uninstall:\n%s", out)
	}
}

// eventGroups returns the hook groups for an event as generic maps.
func eventGroups(t *testing.T, doc map[string]any, event string) []map[string]any {
	t.Helper()
	hooksMap, _ := doc["hooks"].(map[string]any)
	raw, ok := hooksMap[event].([]any)
	if !ok {
		return nil
	}
	groups := make([]map[string]any, 0, len(raw))
	for _, g := range raw {
		if m, ok := g.(map[string]any); ok {
			groups = append(groups, m)
		}
	}
	return groups
}
