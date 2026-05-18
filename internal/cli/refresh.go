package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/envfile"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/registry"
)

func newRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Re-render templates and .env.local in cwd worktree",
		RunE:  runRefresh,
	}
	cmd.Flags().Bool("force", false, "overwrite existing files (default true; reserved for future merge tweaks)")
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
	_, alloc, found, err := handle.FindByWorktreePath(info.CurrentWorktree)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("current worktree %q has no port allocation (run `nerve new` to re-create or `nerve doctor`)", info.CurrentWorktree)
	}

	// Recompute env vars and rewrite .env.local.
	envVars := make(map[string]string, len(cfg.Services))
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		envVars[svc.EnvKey] = strconv.Itoa(svc.BasePort + cfg.Project.PortOffset + alloc.Offset)
	}
	envPath := filepath.Join(info.CurrentWorktree, ".env.local")
	if err := envfile.WriteFile(envPath, envVars); err != nil {
		return fmt.Errorf("write .env.local: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "rewrote %s\n", envPath)
	return nil
}
