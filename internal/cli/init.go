package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/worktree"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold .nerve/config.yaml in current repo and register it",
		RunE:  runInit,
	}
	cmd.Flags().Bool("force", false, "overwrite existing .nerve/config.yaml")
	cmd.Flags().String("name", "", "logical project name for registration (defaults to repo dir name)")
	cmd.Flags().String("default-base", "", "default base branch recorded in the registry (e.g. main)")
	cmd.Flags().Bool("no-register", false, "scaffold only; do not register the project in the global registry")
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

	// Write the local artifact (the scaffold) first, before any global side effect.
	if err := os.MkdirAll(filepath.Join(root, config.NerveDir), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Join(root, config.NerveDir), err)
	}
	if err := os.WriteFile(cfgPath, config.Scaffold(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cfgPath, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", cfgPath)

	added, err := worktree.EnsureGitignore(root, []string{".worktrees/", ".nerve/ports.json", ".nerve/*.lock"})
	if err != nil {
		return fmt.Errorf("gitignore: %w", err)
	}
	if len(added) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "appended %d entries to .gitignore: %v\n", len(added), added)
	}

	noRegister, _ := cmd.Flags().GetBool("no-register")
	if noRegister {
		fmt.Fprintf(cmd.OutOrStdout(), "next: `nerve project add %s` then edit %s to declare services/clone_files\n", root, cfgPath)
		return nil
	}

	// Auto-register the main checkout in the global registry (issue #28).
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return err
	}
	if existing := reg.FindProjectByPath(root); existing != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "already registered as %q. next: edit %s to declare services/clone_files, then `nerve hooks install --project`\n", existing.Name, cfgPath)
		return nil
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		name = filepath.Base(root)
	}
	defaultBase, _ := cmd.Flags().GetString("default-base")
	if err := reg.AddProject(config.ProjectEntry{Name: name, Path: root, DefaultBase: defaultBase}); err != nil {
		// A DIFFERENT path already owns this name. The scaffold (init's main job) is
		// already written, so don't fail — warn with a clear remedy and succeed.
		fmt.Fprintf(cmd.OutOrStdout(), "auto-registration skipped: a different project is already registered as %q; run `nerve project add . --name <unique>`\n", name)
		return nil
	}
	if err := config.SaveGlobalRegistry(reg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "registered as %q. next: edit %s to declare services/clone_files, then `nerve hooks install --project`\n", name, cfgPath)
	return nil
}
