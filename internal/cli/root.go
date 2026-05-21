package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/tui"
	"github.com/mascah/nerve/internal/version"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "nerve",
		Short:         "Worktree manager with Claude Code integration",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// With no subcommand, launch the interactive project-setup TUI. Pass cwd so
		// the TUI can show the current worktree's ports when launched inside one.
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) > 0 {
				return c.Help()
			}
			cwd, _ := os.Getwd()
			return tui.Run(cwd)
		},
	}
	cmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")

	cmd.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newProjectCmd(),
		newNewCmd(),
		newRemoveCmd(),
		newListCmd(),
		newEnvCmd(),
		newPortsCmd(),
		newHooksCmd(),
		newWorktreeCreateCmd(),
		newWorktreeRemoveCmd(),
		newRunHooksCmd(),
		newGCTrashCmd(),
		newGCCmd(),
		newRefreshCmd(),
		newDoctorCmd(),
	)
	return cmd
}
