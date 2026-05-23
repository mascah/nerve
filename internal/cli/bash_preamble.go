package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/envfile"
	"github.com/mascah/nerve/internal/envinject"
)

// bashPreambleInput is the subset of the PreToolUse hook stdin payload we need.
// Claude Code passes the session cwd at top level and the tool arguments under
// tool_input (for Bash, tool_input.command is the pending shell command).
type bashPreambleInput struct {
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// bashPreambleOutput is the PreToolUse response that rewrites the pending command.
// We deliberately omit permissionDecision: setting "allow" both historically suppressed
// updatedInput and would auto-approve every Bash command. Emitting only updatedInput
// rewrites the command and leaves the normal permission flow intact.
type bashPreambleOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName string             `json:"hookEventName"`
	UpdatedInput  bashUpdatedCommand `json:"updatedInput"`
}

type bashUpdatedCommand struct {
	Command string `json:"command"`
}

func newBashPreambleCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "bash-preamble",
		Short:  "Hook entry point: rewrite a Bash command to load the worktree env (PreToolUse:Bash)",
		Hidden: true,
		RunE:   runBashPreamble,
	}
}

func runBashPreamble(cmd *cobra.Command, _ []string) error {
	in := bashPreambleInput{}
	if err := json.NewDecoder(cmd.InOrStdin()).Decode(&in); err != nil && err != io.EOF {
		// Malformed payload: do nothing so the original command runs unchanged.
		return nil
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Gate: only rewrite inside a registered + configured worktree that has an
	// allocation. Any other case (main checkout, lightweight project, unregistered
	// repo, errors) is a silent no-op — matching `nerve env --inject`'s contract so
	// this hook never noises up or alters main-checkout commands.
	vars, cfg, _, err := envinject.ComputeWithConfig(cwd)
	if err != nil || len(vars) == 0 || cfg == nil {
		return nil
	}

	preamble := cfg.Project.BashPreamble
	if preamble == "" {
		preamble = strings.TrimRight(envfile.RenderShell(vars), "\n")
	}
	if preamble == "" {
		return nil
	}

	out := bashPreambleOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName: "PreToolUse",
			UpdatedInput:  bashUpdatedCommand{Command: preamble + "\n" + in.ToolInput.Command},
		},
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(&out)
}
