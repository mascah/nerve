package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
)

// Exit codes (referenced from documentation / plan).
const (
	ExitOK            = 0
	ExitUsage         = 1
	ExitGit           = 2
	ExitPoolExhausted = 3
	ExitCloneFailed   = 4
	ExitDirty         = 5
	ExitUnpushed      = 6
	ExitNotInWorktree = 7
	ExitDoctorIssues  = 8
)

// resolveProject looks up name in the global registry. Returns the resolved entry or
// an error if not found.
func resolveProject(name string) (*config.ProjectEntry, *config.GlobalRegistry, error) {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return nil, nil, err
	}
	entry := reg.FindProject(name)
	if entry == nil {
		return nil, reg, fmt.Errorf("project %q not registered (run `nerve project add <path>`)", name)
	}
	return entry, reg, nil
}

// resolveProjectByCwd finds the project entry whose Path encloses cwd. Returns nil
// when cwd isn't inside any registered project (caller decides how to handle).
func resolveProjectByCwd(cwd string) (*config.ProjectEntry, *config.GlobalRegistry, error) {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return nil, nil, err
	}
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, reg, err
	}
	// Discover the main checkout for cwd, then look up by exact path.
	info, err := gitutil.Discover(cwdAbs)
	if err != nil {
		return nil, reg, nil
	}
	entry := reg.FindProjectByPath(info.MainCheckout)
	return entry, reg, nil
}

// printErr is a helper for commands that want non-default exit codes.
func printErr(cmd *cobra.Command, msg string) {
	fmt.Fprintln(cmd.ErrOrStderr(), "nerve:", msg)
}

// progressWriter returns the stderr stream when verbose is set, else io.Discard.
// All commands accept the persistent --verbose / -v flag from root.
func progressWriter(cmd *cobra.Command) io.Writer {
	v, _ := cmd.Flags().GetBool("verbose")
	if !v {
		// Quiet — most users get a tidy summary; verbose gets a play-by-play.
		return io.Discard
	}
	return cmd.ErrOrStderr()
}

// loadProjectConfigOrLightweight loads the per-project config or returns (nil, nil)
// if the project is in lightweight mode (no .nerve/config.yaml present).
func loadProjectConfigOrLightweight(repoRoot string) (*config.ProjectConfig, error) {
	cfg, err := config.LoadProjectConfig(repoRoot)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, config.ErrNotFound) {
		return nil, nil
	}
	return nil, err
}

// fileExists is a tiny convenience used by a couple of commands.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// exitCodeError lets RunE bubble up a custom exit code through cobra's Execute()
// so main.go can call os.Exit with the right code. The Unwrap method ensures
// errors.Is/As works on the wrapped cause.
type exitCodeError struct {
	Code int
	Err  error
}

func (e exitCodeError) Error() string { return e.Err.Error() }
func (e exitCodeError) Unwrap() error { return e.Err }

// ExitCode extracts the numeric exit code from err. If err wraps an
// exitCodeError, its Code is returned; otherwise ExitUsage (1) is returned.
// Returns ExitOK (0) for a nil error.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ece exitCodeError
	if errors.As(err, &ece) {
		return ece.Code
	}
	return ExitUsage
}
