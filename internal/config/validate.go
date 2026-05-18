package config

import (
	"fmt"
	"strings"
)

// Validate returns a non-nil error describing the first structural problem found in cfg.
func Validate(cfg *ProjectConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Version != CurrentConfigVersion {
		return fmt.Errorf("unsupported config version %d (expected %d)", cfg.Version, CurrentConfigVersion)
	}
	if cfg.Project.PoolSize < 1 {
		return fmt.Errorf("project.pool_size must be >= 1")
	}
	if cfg.Project.WorktreeRoot == "" {
		return fmt.Errorf("project.worktree_root must be set")
	}

	seenSvcID := make(map[string]bool, len(cfg.Services))
	seenEnvKey := make(map[string]bool, len(cfg.Services))
	primaries := 0
	for i, s := range cfg.Services {
		if s.ID == "" {
			return fmt.Errorf("services[%d]: id is required", i)
		}
		if seenSvcID[s.ID] {
			return fmt.Errorf("services[%d]: duplicate id %q", i, s.ID)
		}
		seenSvcID[s.ID] = true
		if s.BasePort < 1 || s.BasePort > 65535 {
			return fmt.Errorf("services[%d] (%s): base_port out of range", i, s.ID)
		}
		if s.EnvKey == "" {
			return fmt.Errorf("services[%d] (%s): env_key is required", i, s.ID)
		}
		if seenEnvKey[s.EnvKey] {
			return fmt.Errorf("services[%d] (%s): duplicate env_key %q", i, s.ID, s.EnvKey)
		}
		seenEnvKey[s.EnvKey] = true
		if s.Primary {
			primaries++
		}
	}
	if primaries > 1 {
		return fmt.Errorf("only one service may be marked primary")
	}

	for i, f := range cfg.CloneFiles {
		if f.Path == "" {
			return fmt.Errorf("clone_files[%d]: path is required", i)
		}
		if strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") {
			return fmt.Errorf("clone_files[%d] (%s): path must be relative and stay inside the repo", i, f.Path)
		}
		switch f.Kind {
		case "", CloneKindFile, CloneKindDirectory:
		default:
			return fmt.Errorf("clone_files[%d] (%s): unknown kind %q", i, f.Path, f.Kind)
		}
	}

	for i, t := range cfg.Templates {
		if t.Source == "" || t.Dest == "" {
			return fmt.Errorf("templates[%d]: source and dest are required", i)
		}
	}

	return nil
}
