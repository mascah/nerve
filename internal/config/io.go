package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned by load functions when the requested file doesn't exist.
var ErrNotFound = errors.New("config not found")

// ProjectConfigFilename is the conventional filename inside the .nerve/ directory.
const (
	NerveDir              = ".nerve"
	ProjectConfigFilename = "config.yaml"
	PortRegistryFilename  = "ports.json"
	PortLockFilename      = "ports.json.lock"
)

// ProjectConfigPath returns <repoRoot>/.nerve/config.yaml.
func ProjectConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, NerveDir, ProjectConfigFilename)
}

// PortRegistryPath returns <repoRoot>/.nerve/ports.json.
func PortRegistryPath(repoRoot string) string {
	return filepath.Join(repoRoot, NerveDir, PortRegistryFilename)
}

// PortLockPath returns the flock companion to PortRegistryPath.
func PortLockPath(repoRoot string) string {
	return filepath.Join(repoRoot, NerveDir, PortLockFilename)
}

// LoadProjectConfig reads <repoRoot>/.nerve/config.yaml and returns the parsed config
// with defaults applied and validation run. Returns ErrNotFound if the file is missing.
func LoadProjectConfig(repoRoot string) (*ProjectConfig, error) {
	return loadProjectConfigFile(ProjectConfigPath(repoRoot))
}

func loadProjectConfigFile(path string) (*ProjectConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg ProjectConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	ApplyDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}
	return &cfg, nil
}

// SaveProjectConfig writes cfg to <repoRoot>/.nerve/config.yaml atomically.
func SaveProjectConfig(repoRoot string, cfg *ProjectConfig) error {
	ApplyDefaults(cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	dir := filepath.Join(repoRoot, NerveDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return writeYAMLAtomic(ProjectConfigPath(repoRoot), cfg)
}

// GlobalRegistryPath returns the path to the user-wide projects registry.
// Honors $XDG_CONFIG_HOME if set, else ~/.config/nerve/projects.yaml.
func GlobalRegistryPath() (string, error) {
	return globalConfigPath("projects.yaml")
}

// LeasesPath returns the path to the user-wide port-leases store. Sibling of
// GlobalRegistryPath; same XDG rules. Used by internal/leases to track active
// per-port leases across ALL nerve projects so two projects with overlapping
// port pools can't double-allocate the same TCP port.
func LeasesPath() (string, error) {
	return globalConfigPath("ports.json")
}

// LeasesLockPath returns the flock companion to LeasesPath.
func LeasesLockPath() (string, error) {
	return globalConfigPath("ports.json.lock")
}

func globalConfigPath(filename string) (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "nerve", filename), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "nerve", filename), nil
}

// LoadGlobalRegistry reads the user-wide projects registry. If the file does not
// exist, it returns an empty registry (not an error) so callers can append cleanly.
func LoadGlobalRegistry() (*GlobalRegistry, error) {
	path, err := GlobalRegistryPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &GlobalRegistry{Version: CurrentRegistryVersion}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var reg GlobalRegistry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if reg.Version == 0 {
		reg.Version = CurrentRegistryVersion
	}
	return &reg, nil
}

// SaveGlobalRegistry writes the user-wide projects registry atomically, creating
// parent directories as needed.
func SaveGlobalRegistry(reg *GlobalRegistry) error {
	if reg.Version == 0 {
		reg.Version = CurrentRegistryVersion
	}
	path, err := GlobalRegistryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return writeYAMLAtomic(path, reg)
}

// writeYAMLAtomic marshals v as YAML and writes it to path via a temp+rename.
func writeYAMLAtomic(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
