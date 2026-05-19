package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage the global project registry",
	}

	addCmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Register a project",
		Args:  cobra.ExactArgs(1),
		RunE:  runProjectAdd,
	}
	addCmd.Flags().String("name", "", "logical project name (defaults to repo dir name)")
	addCmd.Flags().String("default-base", "", "default base branch (e.g. main)")

	listCmd := &cobra.Command{Use: "list", Short: "List registered projects", RunE: runProjectList}
	listCmd.Flags().Bool("json", false, "JSON output")

	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Deregister a project",
		Args:  cobra.ExactArgs(1),
		RunE:  runProjectRemove,
	}

	cmd.AddCommand(addCmd, listCmd, removeCmd)
	return cmd
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	rawPath := args[0]
	expanded, err := gitutil.ExpandPath(rawPath)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return err
	}
	info, err := gitutil.Discover(abs)
	if err != nil {
		return fmt.Errorf("%s is not inside a git repo: %w", rawPath, err)
	}
	// We always register the main checkout, never a linked worktree.
	root := info.MainCheckout

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		name = filepath.Base(root)
	}
	defaultBase, _ := cmd.Flags().GetString("default-base")

	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return err
	}
	if err := reg.AddProject(config.ProjectEntry{
		Name:        name,
		Path:        root,
		DefaultBase: defaultBase,
	}); err != nil {
		return err
	}
	if err := config.SaveGlobalRegistry(reg); err != nil {
		return err
	}

	cfgPath := config.ProjectConfigPath(root)
	if fileExists(cfgPath) {
		fmt.Fprintf(cmd.OutOrStdout(), "registered %s -> %s (configured)\n", name, root)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "registered %s -> %s (lightweight; run `nerve init` inside the repo to configure)\n", name, root)
	}
	return nil
}

func runProjectList(cmd *cobra.Command, _ []string) error {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return err
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		out, err := json.MarshalIndent(reg.Projects, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	if len(reg.Projects) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no projects registered")
		return nil
	}
	for _, p := range reg.Projects {
		mode := "lightweight"
		if fileExists(config.ProjectConfigPath(p.Path)) {
			mode = "configured"
		}
		base := p.DefaultBase
		if base == "" {
			base = "-"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-20s  %-10s  %-10s  %s\n", p.Name, mode, base, p.Path)
	}
	return nil
}

func runProjectRemove(cmd *cobra.Command, args []string) error {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return err
	}
	if !reg.RemoveProject(args[0]) {
		return fmt.Errorf("project %q not registered", args[0])
	}
	if err := config.SaveGlobalRegistry(reg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed project %q (repo on disk untouched)\n", args[0])
	return nil
}
