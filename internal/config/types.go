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
	// BackgroundPostCreate is the DEPRECATED project-wide background switch. It is
	// still honored as the fallback default for post_create commands that don't set
	// their own `background:` — when true, such commands run in the background.
	// Prefer per-command control instead (HookCommand.Background): leave env-shapers
	// like `direnv allow` foreground (the default) and tag slow, independent installs
	// with `background: true`. Background hooks run detached and concurrently; their
	// progress + a terminal status are written under .nerve/hooks/<branch_slug>/.
	BackgroundPostCreate bool `yaml:"background_post_create,omitempty"`
	// BackgroundRemove, when true, makes worktree teardown return immediately by
	// renaming the worktree dir into .nerve/trash/ and deleting the bytes in a
	// detached child (git's metadata is reconciled synchronously via prune, so its
	// view never goes out of sync). Default false: teardown runs a synchronous
	// `git worktree remove`, which is slower for large node_modules/.venv trees but
	// fully complete before the command returns.
	BackgroundRemove bool `yaml:"background_remove,omitempty"`
	// BashPreamble, when set, is the shell snippet `nerve bash-preamble` prepends to
	// each Bash-tool command run inside a registered worktree (the PreToolUse:Bash hook,
	// installed opt-in via `nerve hooks install --bash-preamble`). It exists to re-load
	// the worktree's env after Claude's EnterWorktree tool, which fires no env-injecting
	// hook (see docs/claude-code-worktree-env.md). Empty (default) → nerve prepends its
	// own `export KEY=VALUE` port lines, computed in-hook. Set it to e.g.
	// `eval "$(direnv export bash 2>/dev/null)"` to delegate the env load to direnv.
	BashPreamble string `yaml:"bash_preamble,omitempty"`
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
// PostCreate entries support the per-command background form (see HookCommand);
// PreRemove always runs synchronously (it gates a destructive teardown), so it stays
// a plain string list.
type LifecycleHooks struct {
	PostCreate HookCommands `yaml:"post_create,omitempty"`
	PreRemove  []string     `yaml:"pre_remove,omitempty"`
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
