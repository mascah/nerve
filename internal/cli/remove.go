package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/worktree"
)

func newRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove [<project>] [<branch>]",
		Short: "Remove a worktree (defaults to cwd)",
		Args:  cobra.MaximumNArgs(2),
		RunE:  runRemove,
	}
	cmd.Flags().Bool("force", false, "skip dirty + unpushed checks and force git worktree remove")
	cmd.Flags().Bool("keep-branch", false, "do not delete the branch")
	return cmd
}

func runRemove(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	keepBranch, _ := cmd.Flags().GetBool("keep-branch")

	repoRoot, wtPath, branch, projEntry, err := resolveRemoveTarget(args)
	if err != nil {
		return err
	}

	var cfg *config.ProjectConfig
	if projEntry != nil {
		cfg, err = loadProjectConfigOrLightweight(projEntry.Path)
		if err != nil {
			return err
		}
	}

	res, err := worktree.Remove(worktree.RemoveOptions{
		RepoRoot:     repoRoot,
		WorktreePath: wtPath,
		Branch:       branch,
		Cfg:          cfg,
		Force:        force,
		KeepBranch:   keepBranch,
		Log:          cmd.ErrOrStderr(),
	})
	if err != nil {
		switch {
		case errors.Is(err, worktree.ErrDirty):
			printErr(cmd, "worktree has uncommitted changes — use --force to override")
			var de *worktree.DirtyError
			if errors.As(err, &de) && len(de.Files) > 0 {
				printDirtyFiles(cmd, de.Files, 25)
			}
			return exitCodeError{Code: ExitDirty, Err: err}
		case errors.Is(err, worktree.ErrUnpushed):
			printErr(cmd, "worktree has unpushed commits — use --force to override")
			return exitCodeError{Code: ExitUnpushed, Err: err}
		}
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "removed worktree %s\n", wtPath)
	if res.ReleasedPort != "" {
		fmt.Fprintf(out, "  released port %s\n", res.ReleasedPort)
	}
	if res.BranchDeleted {
		fmt.Fprintf(out, "  deleted branch %s\n", branch)
	}
	return nil
}

// resolveRemoveTarget figures out which worktree to remove from the given args.
// Supports: (no args) → cwd; (project) → ambiguous (returns error); (project, branch).
func resolveRemoveTarget(args []string) (repoRoot, wtPath, branch string, entry *config.ProjectEntry, err error) {
	switch len(args) {
	case 0:
		// Default: the worktree containing cwd.
		cwd, err := os.Getwd()
		if err != nil {
			return "", "", "", nil, err
		}
		info, err := gitutil.Discover(cwd)
		if err != nil {
			return "", "", "", nil, fmt.Errorf("cwd is not inside a git worktree")
		}
		if !info.IsWorktree {
			return "", "", "", nil, fmt.Errorf("refusing to remove the main checkout (cd into a linked worktree first)")
		}
		entry, _, _ := resolveProjectByCwd(cwd)
		// Identify the branch (best-effort) by checking which worktree we're in.
		worktrees, _ := gitutil.ListWorktrees(info.MainCheckout)
		wtAbs, _ := filepath.Abs(info.CurrentWorktree)
		for _, w := range worktrees {
			wAbs, _ := filepath.Abs(w.Path)
			if wAbs == wtAbs {
				branch = w.Branch
				break
			}
		}
		return info.MainCheckout, info.CurrentWorktree, branch, entry, nil

	case 2:
		entry, _, err := resolveProject(args[0])
		if err != nil {
			return "", "", "", nil, err
		}
		branch = args[1]
		// Find the worktree path by listing git worktrees for repoRoot and matching branch.
		worktrees, err := gitutil.ListWorktrees(entry.Path)
		if err != nil {
			return "", "", "", nil, err
		}
		for _, w := range worktrees {
			if w.Branch == branch {
				return entry.Path, w.Path, branch, entry, nil
			}
		}
		return "", "", "", nil, fmt.Errorf("no worktree for branch %q in project %q", branch, entry.Name)

	default:
		return "", "", "", nil, fmt.Errorf("provide either no args (use cwd) or <project> <branch>")
	}
}

// printDirtyFiles writes a preview of dirty files to cmd.ErrOrStderr, truncated at
// max entries. The output includes a header line with the shown / total counts and,
// if truncated, a trailing "... N more" line.
func printDirtyFiles(cmd *cobra.Command, files []string, max int) {
	total := len(files)
	shown := total
	if shown > max {
		shown = max
	}
	w := cmd.ErrOrStderr()
	fmt.Fprintln(w)
	fmt.Fprintf(w, "dirty files (showing %d of %d):\n", shown, total)
	for i := 0; i < shown; i++ {
		fmt.Fprintf(w, "  %s\n", files[i])
	}
	if total > shown {
		fmt.Fprintf(w, "  ... %d more\n", total-shown)
	}
}
