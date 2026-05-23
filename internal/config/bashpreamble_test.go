package config

import (
	"os"
	"strings"
	"testing"
)

// TestBashPreambleRoundTrip checks the project.bash_preamble field survives a
// save/load cycle, and that an empty value stays out of the YAML (omitempty).
func TestBashPreambleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &ProjectConfig{
		Version:  CurrentConfigVersion,
		Project:  ProjectSettings{PoolSize: DefaultPoolSize, BashPreamble: `eval "$(direnv export bash)"`},
		Services: []Service{{ID: "django", BasePort: 8000, EnvKey: "DJANGO_PORT", Primary: true}},
	}
	if err := SaveProjectConfig(dir, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Project.BashPreamble != cfg.Project.BashPreamble {
		t.Errorf("bash_preamble = %q, want %q", got.Project.BashPreamble, cfg.Project.BashPreamble)
	}
}

func TestBashPreambleOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := &ProjectConfig{
		Version:  CurrentConfigVersion,
		Project:  ProjectSettings{PoolSize: DefaultPoolSize},
		Services: []Service{{ID: "django", BasePort: 8000, EnvKey: "DJANGO_PORT", Primary: true}},
	}
	if err := SaveProjectConfig(dir, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(ProjectConfigPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "bash_preamble") {
		t.Errorf("empty bash_preamble should be omitted from YAML, got:\n%s", raw)
	}
}
