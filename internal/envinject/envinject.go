// Package envinject computes the env vars that should be active inside a worktree and
// either appends them to $CLAUDE_ENV_FILE (the Claude Code hook integration path) or
// returns them for the CLI to print in shell/JSON form.
//
// Discovery rule: walk up from the given directory using `git rev-parse` to find the
// main checkout, load .nerve/config.yaml (if present), look up the worktree path in
// the port registry, and compute env from the recorded offset + service base ports.
//
// Silent no-op cases (return empty map, no error):
//   - Not a git repo
//   - No .nerve/config.yaml in the main checkout (lightweight project)
//   - Current path is the main checkout itself (no per-worktree overrides)
//   - The current worktree path isn't in the registry
package envinject

import (
	"strconv"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/registry"
)

// Compute returns the env vars that should be set for the worktree containing dir.
// Returns (nil, nil) for any of the silent no-op cases listed in the package doc.
func Compute(dir string) (map[string]string, error) {
	vars, _, err := ComputeVerbose(dir)
	return vars, err
}

// ComputeVerbose is like Compute but also returns a short, human-readable reason
// when no vars are produced (empty string when vars are returned, or when an error
// is returned). Intended for `nerve env --inject --verbose` diagnostics; callers
// that want the silent-no-op contract should keep using Compute.
func ComputeVerbose(dir string) (map[string]string, string, error) {
	info, err := gitutil.Discover(dir)
	if err != nil {
		return nil, "not a git repo", nil
	}
	cfg, err := config.LoadProjectConfig(info.MainCheckout)
	if err != nil {
		if err == config.ErrNotFound {
			return nil, "no .nerve/config.yaml in main checkout " + info.MainCheckout, nil
		}
		return nil, "", err
	}
	if !cfg.IsConfigured() {
		return nil, "project has no services configured", nil
	}
	if !info.IsWorktree {
		return nil, "cwd is the main checkout", nil
	}

	handle := registry.Open(info.MainCheckout)
	wt, err := gitutil.CanonicalPath(info.CurrentWorktree)
	if err != nil {
		return nil, "", err
	}
	_, alloc, found, err := handle.FindByWorktreePath(wt)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, "no allocation for worktree " + wt, nil
	}

	vars := make(map[string]string, len(cfg.Services))
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		port := svc.BasePort + cfg.Project.PortOffset + alloc.Offset
		vars[svc.EnvKey] = strconv.Itoa(port)
	}
	return vars, "", nil
}
