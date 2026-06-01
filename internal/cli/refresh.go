package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/registry"
	"github.com/mascah/nerve/internal/worktree"
)

func newRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Re-render templates and .env.local in cwd worktree",
		RunE:  runRefresh,
	}
	return cmd
}

func runRefresh(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	info, err := gitutil.Discover(cwd)
	if err != nil {
		return fmt.Errorf("cwd is not inside a git worktree")
	}
	if !info.IsWorktree {
		return fmt.Errorf("refresh only operates inside linked worktrees, not the main checkout")
	}
	cfg, err := config.LoadProjectConfig(info.MainCheckout)
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return fmt.Errorf("project has no .nerve/config.yaml — nothing to refresh")
		}
		return err
	}
	handle := registry.Open(info.MainCheckout)
	project, alloc, found, err := handle.FindByWorktreePath(info.CurrentWorktree)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("current worktree %q has no port allocation (run `nerve new` to re-create or `nerve doctor`)", info.CurrentWorktree)
	}
	if project == "" {
		project = filepath.Base(info.MainCheckout)
	}

	// Recompute per-service ports from the worktree's stable offset, then re-render
	// templates + .env.local (ports + static vars) through the same RenderEnv path
	// `nerve new` uses, so refresh and create can't drift (vars and templates were
	// previously dropped here — see docs/TESTING.md §5).
	portByService := make(map[string]int, len(cfg.Services))
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		portByService[svc.ID] = svc.BasePort + cfg.Project.PortOffset + alloc.Offset
	}
	if _, err := worktree.RenderEnv(info.MainCheckout, info.CurrentWorktree, alloc.Branch, project, config.Slugify(alloc.Branch), portByService, cfg, cmd.OutOrStdout()); err != nil {
		return err
	}
	return nil
}
