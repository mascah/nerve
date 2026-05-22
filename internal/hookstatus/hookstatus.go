// Package hookstatus is the shared contract for backgrounded post_create hook
// state. When a project sets background_post_create, nerve spawns a detached
// child (`nerve run-hooks`) that runs the hooks and records its progress here so
// read-side surfaces (the TUI Worktrees tab, `nerve list`) can show whether a
// worktree's bootstrap is still running, finished, or failed.
//
// Everything lives under the MAIN checkout's .nerve/hooks/<branch_slug>/, never
// inside the linked worktree — consistent with the rest of nerve's state, and so
// the status survives the worktree being torn down mid-run. Each worktree slug
// gets a directory with:
//
//	status.json  — the machine-readable Status below (atomic temp+rename writes)
//	log          — combined stdout/stderr of the hook commands
//
// The slug is always config.Slugify(branch); callers map a worktree to its status
// by slugifying the branch name, exactly as worktree.Create does on the write side.
package hookstatus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is the lifecycle phase of a backgrounded hook run.
type State string

const (
	// StateRunning means the detached hook process is still executing.
	StateRunning State = "running"
	// StateOK means every hook command exited zero.
	StateOK State = "ok"
	// StateFailed means a hook command exited non-zero (FailedCommand/ExitCode set).
	StateFailed State = "failed"
)

// Status is the persisted state of a backgrounded post_create hook run.
type Status struct {
	State State `json:"state"`
	// PID of the detached runner (informational; useful for `nerve doctor`).
	PID int `json:"pid,omitempty"`
	// StartedAt is when the runner began.
	StartedAt time.Time `json:"started_at"`
	// FinishedAt is when the runner reached a terminal state (zero while running).
	FinishedAt time.Time `json:"finished_at,omitempty"`
	// ExitCode of the failing command (only meaningful when State == StateFailed).
	ExitCode int `json:"exit_code,omitempty"`
	// FailedCommand is the shell command that failed (only set on StateFailed).
	FailedCommand string `json:"failed_command,omitempty"`
}

// Done reports whether the run has reached a terminal state.
func (s Status) Done() bool { return s.State == StateOK || s.State == StateFailed }

// Dir returns the per-worktree status directory under the main checkout.
func Dir(repoRoot, slug string) string {
	return filepath.Join(repoRoot, ".nerve", "hooks", slug)
}

// StatusPath returns the path to the status.json file for a worktree slug.
func StatusPath(repoRoot, slug string) string {
	return filepath.Join(Dir(repoRoot, slug), "status.json")
}

// LogPath returns the path to the combined hook log for a worktree slug.
func LogPath(repoRoot, slug string) string {
	return filepath.Join(Dir(repoRoot, slug), "log")
}

// Write persists s for the given worktree slug, creating the directory if needed.
// The status.json write is atomic (temp file + rename) so a reader never observes
// a half-written file.
func Write(repoRoot, slug string, s Status) error {
	dir := Dir(repoRoot, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "status-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, StatusPath(repoRoot, slug))
}

// Read loads the status for a worktree slug. found is false (with a nil error)
// when no status exists — the common case for synchronous projects and for
// worktrees created before backgrounding was enabled.
func Read(repoRoot, slug string) (status Status, found bool, err error) {
	raw, err := os.ReadFile(StatusPath(repoRoot, slug))
	if err != nil {
		if os.IsNotExist(err) {
			return Status{}, false, nil
		}
		return Status{}, false, err
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return Status{}, false, err
	}
	return status, true, nil
}

// Clear removes the status directory for a worktree slug. Best-effort: a missing
// directory is not an error. Called from teardown so a removed worktree doesn't
// leave stale status behind.
func Clear(repoRoot, slug string) error {
	err := os.RemoveAll(Dir(repoRoot, slug))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
