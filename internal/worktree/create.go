// Package worktree orchestrates the create/remove lifecycle for nerve-managed worktrees.
// It glues together gitutil, registry, ports allocator, clone, envfile, templates, and
// nerve's own lifecycle hooks. The CLI and the Claude Code hook entry points both call
// into Create/Remove here so the behavior stays consistent.
package worktree

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/mascah/nerve/internal/clone"
	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/hookstatus"
	"github.com/mascah/nerve/internal/leases"
	"github.com/mascah/nerve/internal/ports"
	"github.com/mascah/nerve/internal/registry"
)

// CreateOptions feeds the Create function.
type CreateOptions struct {
	RepoRoot    string                // absolute path of the project's main checkout
	ProjectName string                // logical project name (for template substitution); may be empty
	Branch      string                // branch name (created if it doesn't exist)
	BaseRef     string                // optional base ref for new branches
	Cfg         *config.ProjectConfig // nil => lightweight mode
	SkipHooks   bool                  // skip cfg.Hooks.PostCreate
	Log         io.Writer             // progress writer; nil => discard
}

// CreateResult summarizes what was created.
type CreateResult struct {
	Path           string
	Branch         string
	Offset         int            // 0 in lightweight mode
	PrimaryPort    int            // 0 in lightweight mode
	PortByService  map[string]int // empty in lightweight mode
	GitignoreAdded []string
}

// Create runs the full new-worktree flow. For lightweight projects (Cfg == nil or
// Cfg has no services), it falls back to a plain `git worktree add` with optional
// .worktreeinclude file copying.
func Create(opts CreateOptions) (*CreateResult, error) {
	if opts.RepoRoot == "" || opts.Branch == "" {
		return nil, fmt.Errorf("worktree.Create: RepoRoot and Branch are required")
	}
	log := discardIfNil(opts.Log)

	// Determine target path (pure: tmpl pick, slugify, traversal/escape checks).
	tp, err := resolveTargetPath(opts)
	if err != nil {
		return nil, err
	}
	projectName := tp.Project
	branchSlug := tp.BranchSlug
	worktreePath := tp.Path

	// Always make sure .worktrees/ is gitignored when we're placing worktrees inside
	// the repo (the default). Also gitignore .nerve/ports.json + lock if a config is
	// present, and .env.local (always written by nerve on create — leaving it
	// tracked would make every nerve-managed worktree permanently "dirty" from
	// git's perspective and block the WorktreeRemove hook's dirty check).
	added, err := EnsureGitignore(opts.RepoRoot, gitignoreEntriesFor(opts.RepoRoot, worktreePath, opts.Cfg))
	if err != nil {
		return nil, fmt.Errorf("gitignore: %w", err)
	}

	// Create the git worktree first; if this fails, we haven't claimed a port yet.
	fmt.Fprintf(log, "git worktree add %s -b %s\n", worktreePath, opts.Branch)
	if err := gitutil.AddWorktree(opts.RepoRoot, worktreePath, opts.Branch, opts.BaseRef); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}

	// Canonicalize so registry entries match what `git rev-parse --show-toplevel`
	// returns later when `nerve env --inject` looks the path up. EvalSymlinks
	// requires the path to exist, so this must come after `git worktree add`.
	if canon, err := gitutil.CanonicalPath(worktreePath); err == nil {
		worktreePath = canon
	}

	// Replicate .claude/settings.json so the SessionStart / CwdChanged hooks
	// installed against the main checkout are visible inside the linked worktree.
	// Best-effort: non-fatal so we don't break worktree create for users without
	// Claude Code configured.
	if err := copyClaudeSettings(opts.RepoRoot, worktreePath, log); err != nil {
		fmt.Fprintf(log, "warning: copy .claude settings: %v\n", err)
	}

	res := &CreateResult{
		Path:           worktreePath,
		Branch:         opts.Branch,
		GitignoreAdded: added,
	}

	// Lightweight short-circuit: no services configured.
	if opts.Cfg == nil || !opts.Cfg.IsConfigured() {
		if opts.Cfg == nil {
			if err := copyWorktreeInclude(opts.RepoRoot, worktreePath, log); err != nil {
				return res, fmt.Errorf("worktreeinclude: %w", err)
			}
		}
		fmt.Fprintf(log, "lightweight worktree ready at %s\n", worktreePath)
		return res, nil
	}

	// Configured path: allocate ports, clone files, render templates, write env, run hooks.
	cfg := opts.Cfg
	handle := registry.Open(opts.RepoRoot)

	// Snapshot the cross-project leases store once for the allocator's pre-check.
	// The leases Reserve call below is the authoritative gate; this pre-check just
	// helps the allocator skip offsets it would otherwise have to undo. Failures
	// here are non-fatal — fall back to nil (no cross-project check) and let
	// Reserve catch any collision.
	leasesStore, leasesOpenErr := leases.Open()
	var checker ports.LeaseChecker
	if leasesOpenErr == nil {
		c, err := leases.NewChecker(leasesStore, worktreePath)
		if err == nil {
			checker = c
		}
	}

	var allocation *ports.Result
	// Lock order: per-project registry FIRST, then the global leases store
	// (acquired inside Reserve below). Reversing this can deadlock concurrent
	// `nerve new` invocations across projects.
	err = handle.With(func(reg *registry.Registry) error {
		if reg.Project == "" {
			reg.Project = projectName
		}
		registry.ConfigurePool(reg, cfg)
		if _, err := registry.CleanStale(reg, opts.RepoRoot); err != nil {
			return err
		}
		alloc, err := ports.Allocate(reg, cfg, worktreePath, opts.Branch, nil, checker)
		if err != nil {
			return err
		}
		allocation = alloc
		return nil
	})
	if err != nil {
		// Roll back the git worktree we just created so we don't leak it.
		_ = gitutil.RemoveWorktree(opts.RepoRoot, worktreePath, true)
		return nil, fmt.Errorf("port allocation: %w", err)
	}

	// Reserve the per-service ports in the global leases store. If this fails
	// (another project beat us to it between snapshot and now), roll back BOTH
	// the local registry claim and the git worktree so we don't leak state.
	if leasesStore != nil {
		lease := leases.Lease{
			Project:      projectName,
			ProjectPath:  opts.RepoRoot,
			WorktreePath: worktreePath,
			Branch:       opts.Branch,
		}
		if reserveErr := leasesStore.Reserve(allocation.PortByService, lease); reserveErr != nil {
			// Roll back local registry claim.
			_ = handle.With(func(reg *registry.Registry) error {
				reg.ReleaseByWorktreePath(worktreePath)
				return nil
			})
			_ = gitutil.RemoveWorktree(opts.RepoRoot, worktreePath, true)
			return nil, fmt.Errorf("port lease: %w", reserveErr)
		}
	}

	// From here on we hold a registry allocation and (when leasesStore != nil) a
	// global lease. Any failure in the remaining setup steps — clone, templates,
	// vars, .env.local, post_create hooks — must release BOTH and remove the git
	// worktree, otherwise we leak a half-initialized worktree plus its port claims
	// (a failing post_create hook is the common case). committed is flipped true
	// only on the successful return at the end of the function.
	committed := false
	defer func() {
		if committed {
			return
		}
		if leasesStore != nil {
			_, _ = leasesStore.Release(worktreePath)
		}
		_ = handle.With(func(reg *registry.Registry) error {
			reg.ReleaseByWorktreePath(worktreePath)
			return nil
		})
		_ = gitutil.RemoveWorktree(opts.RepoRoot, worktreePath, true)
	}()

	res.Offset = allocation.Offset
	res.PrimaryPort = allocation.PrimaryPort
	res.PortByService = allocation.PortByService

	// Copy clone_files (one-time at create; refresh deliberately does not re-copy).
	if err := copyCloneFiles(opts.RepoRoot, worktreePath, cfg, log); err != nil {
		return res, err
	}

	// Render templates + write .env.local (per-service ports + static vars).
	// Shared with `nerve refresh` via RenderEnv so the two paths can't drift.
	envVars, err := RenderEnv(opts.RepoRoot, worktreePath, opts.Branch, projectName, branchSlug, allocation.PortByService, cfg, log)
	if err != nil {
		return res, err
	}

	// Run post_create hooks with the allocated ports + identity vars in their
	// environment. A foreground failure rolls back the worktree via the defer above
	// (the helper touches neither res nor committed). The SkipHooks / empty-hooks
	// guard lives inside the helper.
	if err := runPostCreateHooks(opts, worktreePath, projectName, branchSlug, envVars, cfg, log); err != nil {
		return res, err
	}

	fmt.Fprintf(log, "worktree ready at %s (offset %d, primary port %d)\n", worktreePath, allocation.Offset, allocation.PrimaryPort)
	committed = true
	return res, nil
}

// targetPath is the result of resolveTargetPath: the computed on-disk worktree
// path plus the identity values derived alongside it.
type targetPath struct {
	Path       string // absolute (canonicalized later, after git worktree add)
	Project    string // projectName with the RepoRoot-base default applied
	BranchSlug string // config.Slugify(branch), guaranteed non-empty
	Rel        string // the rendered (possibly relative) worktree_root path
}

// resolveTargetPath computes the target worktree path from opts without any side
// effects: it picks the worktree_root template, defaults the project name, slugifies
// the branch (rejecting an all-punctuation branch), rejects ".." path segments, and
// renders + joins + escape-checks the path. Failing here costs nothing to roll back.
func resolveTargetPath(opts CreateOptions) (targetPath, error) {
	tmpl := config.DefaultWorktreeRoot
	if opts.Cfg != nil && opts.Cfg.Project.WorktreeRoot != "" {
		tmpl = opts.Cfg.Project.WorktreeRoot
	}
	projectName := opts.ProjectName
	if projectName == "" {
		projectName = filepath.Base(opts.RepoRoot)
	}
	// Computed before any side effects so an unusable branch fails fast (before
	// `git worktree add`), with no worktree/port to roll back.
	branchSlug := config.Slugify(opts.Branch)
	if branchSlug == "" {
		return targetPath{}, fmt.Errorf("branch %q has no alphanumeric characters to form a branch_slug", opts.Branch)
	}
	// Reject branch names with ".." path segments. The branch is interpolated into
	// the on-disk worktree path via the {branch} template var, so a name like
	// "../../escape" would otherwise place the worktree outside the repo. (Such
	// names are also invalid as git refs.)
	if hasDotDotSegment(opts.Branch) {
		return targetPath{}, fmt.Errorf("branch %q contains a %q path segment", opts.Branch, "..")
	}
	rel := config.RenderPath(tmpl, map[string]string{
		"branch":      opts.Branch,
		"project":     projectName,
		"branch_slug": branchSlug,
	})
	worktreePath := rel
	if !filepath.IsAbs(worktreePath) {
		worktreePath = filepath.Join(opts.RepoRoot, rel)
	}
	// Defense in depth: for the common repo-relative worktree_root, ensure the
	// rendered path didn't escape the repository. Absolute templates are an explicit
	// opt-out (the user pointed worktree_root outside the repo on purpose).
	if !filepath.IsAbs(rel) && !isInsideRepo(opts.RepoRoot, worktreePath) {
		return targetPath{}, fmt.Errorf("worktree path %q escapes repository root %q", worktreePath, opts.RepoRoot)
	}
	return targetPath{Path: worktreePath, Project: projectName, BranchSlug: branchSlug, Rel: rel}, nil
}

// gitignoreEntriesFor builds the list of .gitignore entries to ensure on create.
// .worktrees/ is added only when worktrees land inside the repo (the default); the
// .nerve/* + .env.local entries are added whenever a config is present.
func gitignoreEntriesFor(repoRoot, worktreePath string, cfg *config.ProjectConfig) []string {
	entries := []string{}
	if isInsideRepo(repoRoot, worktreePath) {
		entries = append(entries, ".worktrees/")
	}
	if cfg != nil {
		entries = append(entries,
			".nerve/ports.json",
			".nerve/*.lock",
			".nerve/hooks/",
			".nerve/trash/",
			".env.local",
		)
	}
	return entries
}

// copyCloneFiles copies the project's clone_files into the worktree (one-time at
// create; refresh deliberately does not re-copy). No-op when none are configured.
func copyCloneFiles(repoRoot, worktreePath string, cfg *config.ProjectConfig, log io.Writer) error {
	if len(cfg.CloneFiles) == 0 {
		return nil
	}
	fmt.Fprintln(log, "copying clone_files:")
	cres, err := clone.Run(repoRoot, worktreePath, cfg.CloneFiles)
	if err != nil {
		return fmt.Errorf("clone: %w", err)
	}
	for _, p := range cres.Copied {
		fmt.Fprintf(log, "  copied: %s\n", p)
	}
	for _, p := range cres.Skipped {
		fmt.Fprintf(log, "  skipped (missing, not required): %s\n", p)
	}
	return nil
}

// buildHookEnv returns the environment passed to post_create hooks: the rendered
// env vars (ports + static vars) plus the well-known identity vars. It copies
// envVars rather than mutating the caller's map.
func buildHookEnv(envVars map[string]string, branch, project, worktreePath, branchSlug string) map[string]string {
	hookEnv := make(map[string]string, len(envVars)+4)
	for k, v := range envVars {
		hookEnv[k] = v
	}
	hookEnv["BRANCH"] = branch
	hookEnv["PROJECT"] = project
	hookEnv["WORKTREE_PATH"] = worktreePath
	hookEnv["BRANCH_SLUG"] = branchSlug
	return hookEnv
}

// runPostCreateHooks runs the project's post_create hooks with the allocated ports +
// identity vars in their environment, so setup scripts (migrations, installers) can
// read them. We expose the same KEY=port / static-var pairs written to .env.local,
// plus a few well-known identity vars. Each command is either foreground (default) or
// background (per-command `background:`, falling back to the deprecated project
// default). Foreground hooks run synchronously here, in order, so env-shapers like
// `direnv allow` take effect before the path is printed. Background hooks go to a
// detached child that runs them concurrently — Create (and the WorktreeCreate hook
// that prints the path) returns immediately; the child records progress under
// .nerve/hooks/<slug>/ for the TUI / `nerve list`.
//
// It returns only an error: a foreground-hook failure is propagated, and the caller's
// defer (holding res + committed) performs the rollback. This helper touches neither.
func runPostCreateHooks(opts CreateOptions, worktreePath, projectName, branchSlug string, envVars map[string]string, cfg *config.ProjectConfig, log io.Writer) error {
	if opts.SkipHooks || len(cfg.Hooks.PostCreate) == 0 {
		return nil
	}

	hookEnv := buildHookEnv(envVars, opts.Branch, projectName, worktreePath, branchSlug)

	fg, bg := cfg.Hooks.PostCreate.Partition(cfg.Project.BackgroundPostCreate)

	// Foreground hooks run synchronously, in declared order, before we report the
	// worktree path. A failure here still rolls back the worktree (caller's defer).
	if len(fg) > 0 {
		fmt.Fprintln(log, "running post_create hooks:")
		if err := RunHooks(worktreePath, fg, hookEnv, log); err != nil {
			return err
		}
	}

	// Background hooks can't roll back the worktree (Create has returned by the
	// time they run) — a failure surfaces as a "failed" status, not a removal.
	if len(bg) > 0 {
		if backgroundSupported() {
			// Mark "running" up front so read-side surfaces reflect it the instant
			// the path is printed, then spawn the detached runner with the hook env.
			_ = hookstatus.Write(opts.RepoRoot, branchSlug, hookstatus.Status{
				State:     hookstatus.StateRunning,
				StartedAt: time.Now(),
			})
			if err := spawnDetachedFn(hookEnv, "run-hooks",
				"--repo", opts.RepoRoot,
				"--worktree", worktreePath,
				"--branch", opts.Branch,
			); err != nil {
				// Couldn't detach — run them inline (still concurrently) rather than
				// silently skipping the project's bootstrap.
				fmt.Fprintf(log, "warning: could not background post_create hooks (%v); running synchronously\n", err)
				_ = hookstatus.Clear(opts.RepoRoot, branchSlug)
				if err := RunHooksParallel(worktreePath, bg, hookEnv, log); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(log, "post_create hooks running in background (see .nerve/hooks/%s/log)\n", branchSlug)
			}
		} else {
			// Platform without detached-process support: run them inline.
			if err := RunHooksParallel(worktreePath, bg, hookEnv, log); err != nil {
				return err
			}
		}
	}
	return nil
}

func isInsideRepo(repoRoot, target string) bool {
	repoAbs, err1 := filepath.Abs(repoRoot)
	tgtAbs, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(repoAbs, tgtAbs)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// hasDotDotSegment reports whether name contains a ".." path segment when split on
// "/". Used to reject branch names that would traverse out of the worktree root.
func hasDotDotSegment(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func discardIfNil(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
