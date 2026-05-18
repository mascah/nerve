package cli

import (
	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/version"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "nerve",
		Short:         "Worktree manager with Claude Code integration",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
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
