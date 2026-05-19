package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/worktree"
)

// hookInput is the JSON shape Claude Code passes on stdin to WorktreeCreate hooks.
// Per the docs: {"name": "..."} plus an optional "cwd" with the directory from which
// Claude Code was invoked.
type hookInput struct {
	Name string `json:"name"`
	Cwd  string `json:"cwd"`
}

func newWorktreeCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "worktree-create",
		Short:  "Hook entry point: read {name} from stdin, create worktree, print absolute path",
		Hidden: true,
		RunE:   runWorktreeCreate,
	}
}

func runWorktreeCreate(cmd *cobra.Command, _ []string) error {
	in := hookInput{}
	dec := json.NewDecoder(os.Stdin)
	if err := dec.Decode(&in); err != nil && err != io.EOF {
		fmt.Fprintln(cmd.ErrOrStderr(), "nerve worktree-create: stdin parse:", err)
	}
	if in.Cwd == "" {
		in.Cwd, _ = os.Getwd()
	}
	if in.Name == "" {
		in.Name = "wt-" + randomSlug()
	}

	entry, _, err := resolveProjectByCwd(in.Cwd)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "nerve worktree-create: lookup project:", err)
		return err
	}
	if entry == nil {
		// Unregistered repo: defer to Claude Code's default git worktree behavior.
		fmt.Fprintln(cmd.ErrOrStderr(), "nerve: project not registered for", in.Cwd, "— deferring to Claude Code default")
		return fmt.Errorf("project not registered for %s", in.Cwd)
	}

	cfg, err := loadProjectConfigOrLightweight(entry.Path)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "nerve worktree-create: load config:", err)
		return err
	}

	res, err := worktree.Create(worktree.CreateOptions{
		RepoRoot:    entry.Path,
		ProjectName: entry.Name,
		Branch:      in.Name,
		BaseRef:     entry.DefaultBase,
		Cfg:         cfg,
		Log:         cmd.ErrOrStderr(),
	})
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "nerve worktree-create:", err)
		return err
	}

	// Claude Code consumes stdout as the worktree path.
	fmt.Fprintln(cmd.OutOrStdout(), res.Path)
	return nil
}

func newWorktreeRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "worktree-remove",
		Short:  "Hook entry point: read JSON from stdin, run cleanup",
		Hidden: true,
		RunE:   runWorktreeRemove,
	}
	cmd.Flags().Bool("from-hook", false, "read JSON payload from stdin (vs. cwd-based)")
	return cmd
}

type removeHookInput struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func runWorktreeRemove(cmd *cobra.Command, _ []string) error {
	in := removeHookInput{}
	fromHook, _ := cmd.Flags().GetBool("from-hook")
	if fromHook {
		_ = json.NewDecoder(os.Stdin).Decode(&in)
	}

	wtPath := in.Path
	if wtPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		wtPath = cwd
	}
	if canon, err := gitutil.CanonicalPath(wtPath); err == nil {
		wtPath = canon
	}

	info, err := gitutil.Discover(wtPath)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "nerve worktree-remove: discover:", err)
		return err
	}

	var branch string
	worktrees, _ := gitutil.ListWorktrees(info.MainCheckout)
	for _, w := range worktrees {
		if canon, err := gitutil.CanonicalPath(w.Path); err == nil && canon == wtPath {
			branch = w.Branch
			break
		}
	}

	entry, _, _ := resolveProjectByCwd(info.MainCheckout)
	projectPath := info.MainCheckout
	var cfg *config.ProjectConfig
	if entry != nil {
		projectPath = entry.Path
		cfg, _ = loadProjectConfigOrLightweight(entry.Path)
	} else {
		cfg, _ = loadProjectConfigOrLightweight(info.MainCheckout)
	}

	_, err = worktree.Remove(worktree.RemoveOptions{
		RepoRoot:     projectPath,
		WorktreePath: wtPath,
		Branch:       branch,
		Cfg:          cfg,
		// fromHook implies the user clicked "remove" in Claude Code's exit
		// dialog — they explicitly consented. Untracked .env.local /
		// .claude/settings.local.json would otherwise trip the dirty check
		// and silently leak the worktree.
		Force: fromHook,
		Log:   cmd.ErrOrStderr(),
	})
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "nerve worktree-remove:", err)
		return err
	}
	return nil
}

func randomSlug() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
