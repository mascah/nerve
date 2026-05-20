package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/hooks"
	"github.com/mascah/nerve/internal/worktree"
)

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "hooks", Short: "Manage Claude Code hook installation"}

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Write nerve hooks into .claude/settings.local.json (structural merge)",
		RunE:  runHooksInstall,
	}
	installCmd.Flags().Bool("user", false, "write to ~/.claude/settings.json (applies to every repo)")
	installCmd.Flags().Bool("project", false, "write to <repo>/.claude/settings.local.json (default; user-local, not committed)")
	installCmd.Flags().Bool("shared", false, "write to <repo>/.claude/settings.json instead of settings.local.json (commits hooks so every collaborator must have nerve installed)")
	installCmd.Flags().Bool("dry-run", false, "print the merged file without writing")

	uninstallCmd := &cobra.Command{Use: "uninstall", Short: "Remove nerve hooks", RunE: runHooksUninstall}
	uninstallCmd.Flags().Bool("user", false, "operate on ~/.claude/settings.json")
	uninstallCmd.Flags().Bool("project", false, "operate on <repo>/.claude/settings.local.json (default)")
	uninstallCmd.Flags().Bool("shared", false, "operate on <repo>/.claude/settings.json")

	showCmd := &cobra.Command{Use: "show", Short: "Print the hook config nerve would write", RunE: runHooksShow}

	cmd.AddCommand(installCmd, uninstallCmd, showCmd)
	return cmd
}

func runHooksInstall(cmd *cobra.Command, _ []string) error {
	target, scope, err := resolveHookTargetFile(cmd)
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

	// For the default project scope, make sure settings.local.json is gitignored
	// so nerve-installed hooks don't sneak into commits. Also warn the user if a
	// prior install wrote to the committed settings.json so they can clean it up.
	if scope == hookScopeProjectLocal {
		repoRoot := filepath.Dir(filepath.Dir(target)) // .claude/settings.local.json → repo
		added, ierr := worktree.EnsureGitignore(repoRoot, []string{".claude/settings.local.json"})
		if ierr == nil && len(added) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  added .claude/settings.local.json to .gitignore\n")
		}
		sharedPath := filepath.Join(repoRoot, ".claude", "settings.json")
		if fileExists(sharedPath) && hasNerveEntries(sharedPath) {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: %s also contains nerve hooks from an older install — run `nerve hooks uninstall --shared` to remove them (your collaborators may not have nerve installed)\n",
				sharedPath)
		}
	}
	return nil
}

func hasNerveEntries(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytesContainsNerveSentinel(raw)
}

func bytesContainsNerveSentinel(b []byte) bool {
	const sentinel = "# nerve-managed"
	for i := 0; i+len(sentinel) <= len(b); i++ {
		if string(b[i:i+len(sentinel)]) == sentinel {
			return true
		}
	}
	return false
}

func runHooksUninstall(cmd *cobra.Command, _ []string) error {
	target, _, err := resolveHookTargetFile(cmd)
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

type hookScope int

const (
	hookScopeProjectLocal  hookScope = iota // .claude/settings.local.json (default)
	hookScopeProjectShared                  // .claude/settings.json (committed)
	hookScopeUser                           // ~/.claude/settings.json
)

func resolveHookTargetFile(cmd *cobra.Command) (string, hookScope, error) {
	userScope, _ := cmd.Flags().GetBool("user")
	projectScope, _ := cmd.Flags().GetBool("project")
	sharedScope, _ := cmd.Flags().GetBool("shared")

	flagsSet := 0
	for _, b := range []bool{userScope, projectScope, sharedScope} {
		if b {
			flagsSet++
		}
	}
	if flagsSet > 1 {
		return "", 0, fmt.Errorf("--user, --project, and --shared are mutually exclusive")
	}
	if userScope {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", 0, err
		}
		return filepath.Join(home, ".claude", "settings.json"), hookScopeUser, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", 0, err
	}
	info, err := gitutil.Discover(cwd)
	if err != nil {
		return "", 0, fmt.Errorf("not inside a git repo (run from a project, or pass --user)")
	}
	if sharedScope {
		return filepath.Join(info.MainCheckout, ".claude", "settings.json"), hookScopeProjectShared, nil
	}
	// Default: project-local, so nerve hooks don't get committed and break
	// collaborators who don't have nerve installed.
	return filepath.Join(info.MainCheckout, ".claude", "settings.local.json"), hookScopeProjectLocal, nil
}
