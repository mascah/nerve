package config

// ProjectConfig is the per-project config persisted at <repo>/.nerve/config.yaml.
type ProjectConfig struct {
	Version    int             `yaml:"version"`
	Project    ProjectSettings `yaml:"project"`
	Services   []Service       `yaml:"services,omitempty"`
	Vars       []Var           `yaml:"vars,omitempty"`
	CloneFiles []CloneFile     `yaml:"clone_files,omitempty"`
	Templates  []Template      `yaml:"templates,omitempty"`
	Hooks      LifecycleHooks  `yaml:"hooks,omitempty"`
}

// ProjectSettings holds top-level knobs for a project.
type ProjectSettings struct {
	PortOffset   int    `yaml:"port_offset"`
	WorktreeRoot string `yaml:"worktree_root"`
	PoolSize     int    `yaml:"pool_size"`
	// BackgroundPostCreate, when true, runs post_create hooks in a detached child
	// process so `nerve new` / the WorktreeCreate hook return (and `claude
	// --worktree` boots) immediately instead of blocking on slow installs (uv
	// sync, pnpm i). Hook progress + a terminal status are written under
	// .nerve/hooks/<branch_slug>/. Default false: hooks run synchronously and the
	// worktree env is guaranteed ready before the path is printed.
	BackgroundPostCreate bool `yaml:"background_post_create,omitempty"`
	// BackgroundRemove, when true, makes worktree teardown return immediately by
	// renaming the worktree dir into .nerve/trash/ and deleting the bytes in a
	// detached child (git's metadata is reconciled synchronously via prune, so its
	// view never goes out of sync). Default false: teardown runs a synchronous
	// `git worktree remove`, which is slower for large node_modules/.venv trees but
	// fully complete before the command returns.
	BackgroundRemove bool `yaml:"background_remove,omitempty"`
}

// Service is one network-bound component whose port gets offset per worktree.
type Service struct {
	ID       string `yaml:"id"`
	BasePort int    `yaml:"base_port"`
	EnvKey   string `yaml:"env_key"`
	Primary  bool   `yaml:"primary,omitempty"`
}

// Var is a static, per-worktree environment value written to .env.local alongside
// the per-service port keys. Value is rendered through the same {{...}} template
// engine as templates, so the worktree's template vars (branch, project,
// worktree_path, ports.<id>) are available. Use it for non-port strings that a
// non-shell consumer reads straight from .env.local — e.g. a WORKTREE_ID for
// `docker compose --env-file`, which interpolates ${WORKTREE_ID} for container,
// volume, and network names and never runs a shell loader like direnv.
type Var struct {
	EnvKey string `yaml:"env_key"`
	Value  string `yaml:"value"`
}

// CloneFile is a file or directory copied from the main checkout into a new worktree.
type CloneFile struct {
	Path     string `yaml:"path"`
	Kind     string `yaml:"kind"`
	Required bool   `yaml:"required,omitempty"`
}

// Template is a source file rendered with variable substitution into a worktree.
// When Merge is true, the destination is treated as a dotenv file: existing keys are
// preserved, new keys from source are appended.
type Template struct {
	Source string `yaml:"source"`
	Dest   string `yaml:"dest"`
	Merge  bool   `yaml:"merge,omitempty"`
}

// LifecycleHooks are nerve's own pre/post worktree hooks (distinct from Claude Code hooks).
type LifecycleHooks struct {
	PostCreate []string `yaml:"post_create,omitempty"`
	PreRemove  []string `yaml:"pre_remove,omitempty"`
}

// GlobalRegistry is the user-wide project list at ~/.config/nerve/projects.yaml.
type GlobalRegistry struct {
	Version  int            `yaml:"version"`
	Projects []ProjectEntry `yaml:"projects"`
}

// ProjectEntry maps a logical project name to its main-checkout path.
type ProjectEntry struct {
	Name        string `yaml:"name"`
	Path        string `yaml:"path"`
	DefaultBase string `yaml:"default_base,omitempty"`
}

// CloneFileKind enum-ish values.
const (
	CloneKindFile      = "file"
	CloneKindDirectory = "directory"
)

const (
	CurrentConfigVersion   = 1
	CurrentRegistryVersion = 1

	DefaultWorktreeRoot = ".worktrees/{branch}"
	DefaultPoolSize     = 10
)

// PrimaryService returns the service marked primary, or the first service if none is
// explicitly marked, or nil if the project has no services.
func (c *ProjectConfig) PrimaryService() *Service {
	if c == nil || len(c.Services) == 0 {
		return nil
	}
	for i := range c.Services {
		if c.Services[i].Primary {
			return &c.Services[i]
		}
	}
	return &c.Services[0]
}

// IsConfigured reports whether this project has services/clone files/templates.
// A registered project with no .nerve/config.yaml on disk is "lightweight".
func (c *ProjectConfig) IsConfigured() bool {
	if c == nil {
		return false
	}
	return len(c.Services) > 0 || len(c.CloneFiles) > 0 || len(c.Templates) > 0
}
