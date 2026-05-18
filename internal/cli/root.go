package cli

import (
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
		// With no subcommand, launch the interactive project-setup TUI.
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) > 0 {
				return c.Help()
			}
			return tui.Run()
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
		newRefreshCmd(),
		newDoctorCmd(),
	)
	return cmd
}
