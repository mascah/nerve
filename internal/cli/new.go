package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/ports"
	"github.com/mascah/nerve/internal/worktree"
)

func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new [<project>] <branch>",
		Short: "Create a worktree (project defaults to the one enclosing cwd)",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runNew,
	}
	cmd.Flags().String("from", "", "base ref for new branch (defaults to project default_base or current HEAD)")
	cmd.Flags().Bool("no-hooks", false, "skip post_create hooks")
	cmd.Flags().Bool("minimal", false, "lightweight mode: just git worktree, skip ports/clone/templates")
	cmd.Flags().Bool("print-cd", false, "print 'cd <path>' to stdout (eval-friendly)")
	return cmd
}

func runNew(cmd *cobra.Command, args []string) error {
	// Two forms: `new <branch>` (project inferred from cwd) and the explicit
	// `new <project> <branch>`. Disambiguated purely by arg count.
	var (
		branch string
		entry  *config.ProjectEntry
		err    error
	)
	switch len(args) {
	case 1:
		branch = args[0]
		entry, err = resolveCwdProject()
	default: // 2 (capped by RangeArgs)
		branch = args[1]
		entry, _, err = resolveProject(args[0])
	}
	if err != nil {
		return err
	}

	minimal, _ := cmd.Flags().GetBool("minimal")
	var cfg *config.ProjectConfig
	if !minimal {
		cfg, err = loadProjectConfigOrLightweight(entry.Path)
		if err != nil {
			return err
		}
	}

	from, _ := cmd.Flags().GetString("from")
	if from == "" {
		from = entry.DefaultBase
	}
	skipHooks, _ := cmd.Flags().GetBool("no-hooks")

	res, err := worktree.Create(worktree.CreateOptions{
		RepoRoot:    entry.Path,
		ProjectName: entry.Name,
		Branch:      branch,
		BaseRef:     from,
		Cfg:         cfg,
		SkipHooks:   skipHooks,
		Log:         cmd.ErrOrStderr(),
	})
	if err != nil {
		switch {
		case errors.Is(err, ports.ErrPoolExhausted):
			printErr(cmd, err.Error())
			fmt.Fprintln(cmd.ErrOrStderr(), "hint: try `nerve ports cleanup` to drop stale allocations")
			return exitCodeError{Code: ExitPoolExhausted, Err: err}
		}
		return err
	}

	// Summary table.
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\nworktree created\n")
	fmt.Fprintf(out, "  branch:  %s\n", res.Branch)
	fmt.Fprintf(out, "  path:    %s\n", res.Path)
	if res.Offset > 0 {
		fmt.Fprintf(out, "  offset:  %d\n", res.Offset)
		fmt.Fprintf(out, "  ports:\n")
		ids := make([]string, 0, len(res.PortByService))
		for id := range res.PortByService {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Fprintf(out, "    %-18s %d\n", id+":", res.PortByService[id])
		}
	}
	if printCd, _ := cmd.Flags().GetBool("print-cd"); printCd {
		fmt.Fprintf(out, "cd %s\n", res.Path)
	}
	return nil
}
