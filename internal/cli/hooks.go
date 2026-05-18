package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/hooks"
)

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "hooks", Short: "Manage Claude Code hook installation"}

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Write nerve hooks into .claude/settings.json (structural merge)",
		RunE:  runHooksInstall,
	}
	installCmd.Flags().Bool("user", false, "write to ~/.claude/settings.json (applies to every repo)")
	installCmd.Flags().Bool("project", false, "write to <repo>/.claude/settings.json (default)")
	installCmd.Flags().Bool("dry-run", false, "print the merged file without writing")

	uninstallCmd := &cobra.Command{Use: "uninstall", Short: "Remove nerve hooks", RunE: runHooksUninstall}
	uninstallCmd.Flags().Bool("user", false, "operate on ~/.claude/settings.json")
	uninstallCmd.Flags().Bool("project", false, "operate on <repo>/.claude/settings.json")

	showCmd := &cobra.Command{Use: "show", Short: "Print the hook config nerve would write", RunE: runHooksShow}

	cmd.AddCommand(installCmd, uninstallCmd, showCmd)
	return cmd
}

func runHooksInstall(cmd *cobra.Command, _ []string) error {
	target, err := resolveHookTargetFile(cmd)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	merged, err := hooks.Install(target)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintln(cmd.OutOrStdout(), merged)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, []byte(merged), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote nerve hooks to %s\n", target)
	return nil
}

func runHooksUninstall(cmd *cobra.Command, _ []string) error {
	target, err := resolveHookTargetFile(cmd)
	if err != nil {
		return err
	}
	merged, changed, err := hooks.Uninstall(target)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(cmd.OutOrStdout(), "%s contains no nerve hooks\n", target)
		return nil
	}
	if err := os.WriteFile(target, []byte(merged), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed nerve hooks from %s\n", target)
	return nil
}

func runHooksShow(cmd *cobra.Command, _ []string) error {
	out := hooks.Snippet()
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(raw))
	return nil
}

func resolveHookTargetFile(cmd *cobra.Command) (string, error) {
	userScope, _ := cmd.Flags().GetBool("user")
	projectScope, _ := cmd.Flags().GetBool("project")

	if userScope && projectScope {
		return "", fmt.Errorf("--user and --project are mutually exclusive")
	}
	if userScope {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	// Default to project scope.
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	info, err := gitutil.Discover(cwd)
	if err != nil {
		return "", fmt.Errorf("not inside a git repo (run from a project, or pass --user)")
	}
	return filepath.Join(info.MainCheckout, ".claude", "settings.json"), nil
}
