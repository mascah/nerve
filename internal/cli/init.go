package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/worktree"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold .nerve/config.yaml in current repo",
		RunE:  runInit,
	}
	cmd.Flags().Bool("force", false, "overwrite existing .nerve/config.yaml")
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	info, err := gitutil.Discover(cwd)
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}
	root := info.MainCheckout

	cfgPath := config.ProjectConfigPath(root)
	force, _ := cmd.Flags().GetBool("force")
	if _, err := os.Stat(cfgPath); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", cfgPath)
	}

	cfg := config.Defaults()
	if err := config.SaveProjectConfig(root, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", cfgPath)

	added, err := worktree.EnsureGitignore(root, []string{".worktrees/", ".nerve/ports.json", ".nerve/*.lock"})
	if err != nil {
		return fmt.Errorf("gitignore: %w", err)
	}
	if len(added) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "appended %d entries to .gitignore: %v\n", len(added), added)
	}

	entry, _, _ := resolveProjectByCwd(cwd)
	if entry == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "next: `nerve project add %s` then edit %s to declare services/clone_files\n", root, cfgPath)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "registered as %q. next: edit %s to declare services/clone_files, then `nerve hooks install --project`\n", entry.Name, cfgPath)
	}
	return nil
}
