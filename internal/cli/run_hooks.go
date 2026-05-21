package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/hookstatus"
	"github.com/mascah/nerve/internal/worktree"
)

// newRunHooksCmd registers the hidden `run-hooks` entry point. It is not for humans:
// worktree.Create spawns it as a detached child when a project sets
// background_post_create, so the WorktreeCreate hook can print the worktree path and
// let `claude --worktree` boot without waiting on slow installs. It runs the
// project's post_create hooks in the given worktree and records progress + a terminal
// status under .nerve/hooks/<branch_slug>/ for the TUI / `nerve list` to surface.
func newRunHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "run-hooks",
		Short:  "Hook entry point: run a worktree's post_create hooks (backgrounded)",
		Hidden: true,
		RunE:   runRunHooks,
	}
	cmd.Flags().String("repo", "", "main checkout path")
	cmd.Flags().String("worktree", "", "worktree path the hooks run in")
	cmd.Flags().String("branch", "", "branch name (used to derive the status slug)")
	return cmd
}

func runRunHooks(cmd *cobra.Command, _ []string) error {
	repo, _ := cmd.Flags().GetString("repo")
	wt, _ := cmd.Flags().GetString("worktree")
	branch, _ := cmd.Flags().GetString("branch")
	if repo == "" || wt == "" || branch == "" {
		return fmt.Errorf("run-hooks: --repo, --worktree and --branch are required")
	}
	slug := config.Slugify(branch)
	if slug == "" {
		return fmt.Errorf("run-hooks: branch %q has no slug", branch)
	}

	cfg, err := loadProjectConfigOrLightweight(repo)
	if err != nil {
		return err
	}

	start := time.Now()
	pid := os.Getpid()

	// Mark running (also MkdirAll's the status dir before we open the log file).
	if err := hookstatus.Write(repo, slug, hookstatus.Status{
		State:     hookstatus.StateRunning,
		PID:       pid,
		StartedAt: start,
	}); err != nil {
		return err
	}

	if cfg == nil || len(cfg.Hooks.PostCreate) == 0 {
		// Nothing to run (e.g. config changed since spawn) — record success.
		return hookstatus.Write(repo, slug, hookstatus.Status{
			State:      hookstatus.StateOK,
			PID:        pid,
			StartedAt:  start,
			FinishedAt: time.Now(),
		})
	}

	logf, err := os.Create(hookstatus.LogPath(repo, slug))
	if err != nil {
		return err
	}
	defer logf.Close()

	runErr := worktree.RunHooks(wt, cfg.Hooks.PostCreate, logf)

	st := hookstatus.Status{PID: pid, StartedAt: start, FinishedAt: time.Now()}
	if runErr != nil {
		st.State = hookstatus.StateFailed
		var he *worktree.HookError
		if errors.As(runErr, &he) {
			st.FailedCommand = he.Command
			st.ExitCode = he.ExitCode
		}
		fmt.Fprintf(logf, "\nhook run failed: %v\n", runErr)
	} else {
		st.State = hookstatus.StateOK
	}
	_ = hookstatus.Write(repo, slug, st)
	return runErr
}
