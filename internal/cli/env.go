package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/envfile"
	"github.com/mascah/nerve/internal/envinject"
)

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Print or inject per-worktree env vars",
		Long: `Print the env vars (port overrides) that should be set inside the current worktree.

With --inject, append the vars to $CLAUDE_ENV_FILE so Claude Code's Bash tool sees them.
The --shell form emits 'export KEY=VALUE' lines suitable for ` + "`eval`" + `.
The --json form emits a JSON object.

If cwd isn't inside a configured worktree, all forms are silent no-ops (exit 0).
Pass --verbose with --inject to print the no-op reason to stderr.`,
		RunE: runEnv,
	}
	cmd.Flags().Bool("inject", false, "append to $CLAUDE_ENV_FILE (use from Claude Code hooks)")
	cmd.Flags().Bool("shell", false, "print 'export KEY=VALUE' lines")
	cmd.Flags().Bool("json", false, "JSON output")
	cmd.Flags().Bool("verbose", false, "with --inject, log no-op reasons to stderr")
	cmd.Flags().String("worktree", "", "explicit worktree path (defaults to cwd)")
	return cmd
}

func runEnv(cmd *cobra.Command, _ []string) error {
	dir, _ := cmd.Flags().GetString("worktree")
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	inject, _ := cmd.Flags().GetBool("inject")
	verbose, _ := cmd.Flags().GetBool("verbose")
	vars, reason, err := envinject.ComputeVerbose(dir)
	if err != nil {
		// Hook contexts should never fail loudly — log to stderr and exit 0.
		if inject {
			fmt.Fprintln(cmd.ErrOrStderr(), "nerve env --inject:", err)
			return nil
		}
		return err
	}

	if inject {
		wrote, err := envfile.AppendToClaudeEnv(vars)
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "nerve env --inject:", err)
			return nil
		}
		if verbose {
			switch {
			case reason != "":
				fmt.Fprintln(cmd.ErrOrStderr(), "nerve env --inject: no-op:", reason)
			case !wrote:
				fmt.Fprintln(cmd.ErrOrStderr(), "nerve env --inject: no-op: CLAUDE_ENV_FILE not set")
			default:
				fmt.Fprintf(cmd.ErrOrStderr(), "nerve env --inject: wrote %d var(s) to %s\n", len(vars), os.Getenv("CLAUDE_ENV_FILE"))
			}
		}
		return nil
	}

	if asShell, _ := cmd.Flags().GetBool("shell"); asShell {
		fmt.Fprint(cmd.OutOrStdout(), envfile.RenderShell(vars))
		return nil
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		raw, err := json.MarshalIndent(vars, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(raw))
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), envfile.Render(vars))
	return nil
}
