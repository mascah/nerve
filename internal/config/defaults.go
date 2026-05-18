package config

// Defaults returns a ProjectConfig populated with sane defaults for a brand-new project.
func Defaults() ProjectConfig {
	return ProjectConfig{
		Version: CurrentConfigVersion,
		Project: ProjectSettings{
			PortOffset:   0,
			WorktreeRoot: DefaultWorktreeRoot,
			PoolSize:     DefaultPoolSize,
		},
	}
}

// ApplyDefaults fills in zero-valued required fields on cfg so callers can rely on them.
// Idempotent.
func ApplyDefaults(cfg *ProjectConfig) {
	if cfg.Version == 0 {
		cfg.Version = CurrentConfigVersion
	}
	if cfg.Project.WorktreeRoot == "" {
		cfg.Project.WorktreeRoot = DefaultWorktreeRoot
	}
	if cfg.Project.PoolSize == 0 {
		cfg.Project.PoolSize = DefaultPoolSize
	}
}
