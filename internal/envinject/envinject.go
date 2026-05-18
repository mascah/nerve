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
	"path/filepath"
	"strconv"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/registry"
)

// Compute returns the env vars that should be set for the worktree containing dir.
// Returns (nil, nil) for any of the silent no-op cases listed in the package doc.
func Compute(dir string) (map[string]string, error) {
	info, err := gitutil.Discover(dir)
	if err != nil {
		// Not a git repo — silently produce no vars.
		return nil, nil
	}
	cfg, err := config.LoadProjectConfig(info.MainCheckout)
	if err != nil {
		if err == config.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	if !cfg.IsConfigured() {
		return nil, nil
	}
	if !info.IsWorktree {
		// Sitting in the main checkout: don't inject — the user is likely on the
		// default ports, and we don't want to surprise them.
		return nil, nil
	}

	handle := registry.Open(info.MainCheckout)
	wtAbs, err := filepath.Abs(info.CurrentWorktree)
	if err != nil {
		return nil, err
	}
	_, alloc, found, err := handle.FindByWorktreePath(wtAbs)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	vars := make(map[string]string, len(cfg.Services))
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		port := svc.BasePort + cfg.Project.PortOffset + alloc.Offset
		vars[svc.EnvKey] = strconv.Itoa(port)
	}
	return vars, nil
}
