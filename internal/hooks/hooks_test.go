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
	out, err := Install(path)
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
	out, err := Install(path)
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
	first, err := Install(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Install(path)
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
	if _, err := Install(path); err != nil {
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
