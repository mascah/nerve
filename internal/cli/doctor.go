package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/registry"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose config, registry, and hook installation",
		RunE:  runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return err
	}
	if len(reg.Projects) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no projects registered (start with `nerve project add <path>`)")
		return nil
	}

	issues := 0
	for _, p := range reg.Projects {
		fmt.Fprintf(cmd.OutOrStdout(), "\n# %s (%s)\n", p.Name, p.Path)
		// Repo health.
		info, err := gitutil.Discover(p.Path)
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✗ not a git repository\n")
			issues++
			continue
		}
		if info.MainCheckout != p.Path {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✗ registry path %s != main checkout %s\n", p.Path, info.MainCheckout)
			issues++
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✓ git OK (%s)\n", info.MainCheckout)
		}

		// Project config.
		cfg, err := loadProjectConfigOrLightweight(p.Path)
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✗ config: %v\n", err)
			issues++
			continue
		}
		if cfg == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  • lightweight (no .nerve/config.yaml)\n")
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  ✓ config OK (%d services, %d clone_files, %d templates)\n", len(cfg.Services), len(cfg.CloneFiles), len(cfg.Templates))

		// Port registry vs git worktrees consistency.
		regSnap, err := registry.Open(p.Path).Read()
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✗ port registry: %v\n", err)
			issues++
			continue
		}
		wts, err := gitutil.ListWorktrees(p.Path)
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✗ list worktrees: %v\n", err)
			issues++
			continue
		}
		alive := make(map[string]bool, len(wts))
		for _, w := range wts {
			alive[w.Path] = true
		}
		stale := 0
		for _, a := range regSnap.Allocations {
			if !alive[a.WorktreePath] {
				stale++
			}
		}
		if stale > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  ! %d stale port allocations — run `nerve ports cleanup --project %s`\n", stale, p.Name)
			issues++
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✓ port registry consistent (%d allocations)\n", len(regSnap.Allocations))
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	if issues > 0 {
		return exitCodeError{Code: ExitDoctorIssues, Err: fmt.Errorf("%d issue(s) found", issues)}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "all checks passed")
	return nil
}
