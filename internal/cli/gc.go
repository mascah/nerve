package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/gitutil"
)

// newGCCmd registers the human-facing `nerve gc`, which clears the .nerve/trash
// staging dir left by the background_remove fast path. Normally the detached
// gc-trash worker empties it promptly; this is for sweeping up after an interrupted
// delete (which `nerve doctor` will have flagged). Defaults to the project
// containing cwd; pass a project name to target another.
func newGCCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc [<project>]",
		Short: "Clear leftover worktree bytes from .nerve/trash",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runGC,
	}
}

func runGC(cmd *cobra.Command, args []string) error {
	var repoRoot string
	if len(args) == 1 {
		entry, _, err := resolveProject(args[0])
		if err != nil {
			return err
		}
		repoRoot = entry.Path
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		info, err := gitutil.Discover(cwd)
		if err != nil {
			return fmt.Errorf("cwd is not inside a git worktree (or pass a project name)")
		}
		repoRoot = info.MainCheckout
	}

	count, bytes := trashStats(repoRoot)
	if count == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), ".nerve/trash is empty — nothing to clear")
		return nil
	}
	removed, err := clearTrash(repoRoot)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "cleared %d item(s) (%s) from .nerve/trash\n", removed, humanBytes(bytes))
	return nil
}
