// Package gitutil wraps the subset of `git` we need: locating the main checkout
// from any directory (worktree-aware), listing worktrees, and removing them.
package gitutil

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepo is returned when the given directory isn't inside a git work tree.
var ErrNotRepo = errors.New("not a git repository")

// RepoInfo describes the git context of a directory.
type RepoInfo struct {
	// MainCheckout is the path of the primary working tree (not a linked worktree).
	// This is where .nerve/ and .nerve/ports.json live.
	MainCheckout string
	// CurrentWorktree is the working tree containing the queried path. Equals
	// MainCheckout when the queried path is in the primary checkout.
	CurrentWorktree string
	// CommonGitDir is the path to the shared .git directory (the main repo's .git).
	CommonGitDir string
	// IsWorktree is true when CurrentWorktree != MainCheckout.
	IsWorktree bool
}

// Discover runs `git rev-parse` in dir to fill in a RepoInfo. Returns ErrNotRepo if
// dir is not inside any git work tree.
func Discover(dir string) (*RepoInfo, error) {
	out, err := runGit(dir, "rev-parse",
		"--path-format=absolute",
		"--show-toplevel",
		"--git-common-dir")
	if err != nil {
		if isNotRepoErr(err) {
			return nil, ErrNotRepo
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected git rev-parse output: %q", out)
	}
	topLevel := strings.TrimSpace(lines[0])
	commonDir := strings.TrimSpace(lines[1])

	mainCheckout := filepath.Dir(commonDir)
	info := &RepoInfo{
		MainCheckout:    mainCheckout,
		CurrentWorktree: topLevel,
		CommonGitDir:    commonDir,
		IsWorktree:      topLevel != mainCheckout,
	}
	return info, nil
}

// Worktree describes one entry from `git worktree list --porcelain`.
type Worktree struct {
	Path   string
	Branch string // empty if detached
	Head   string // commit sha
}

// ListWorktrees returns all worktrees registered against the repo containing dir.
func ListWorktrees(dir string) ([]Worktree, error) {
	out, err := runGit(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var (
		all []Worktree
		cur Worktree
	)
	flush := func() {
		if cur.Path != "" {
			all = append(all, cur)
		}
		cur = Worktree{}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "":
			flush()
		}
	}
	flush()
	return all, nil
}

// AddWorktree creates a new worktree at path. If baseRef is non-empty, the new branch
// is created from that ref; otherwise it's created from the repo's current HEAD.
// If the branch already exists, AddWorktree attaches to it instead of creating it.
func AddWorktree(repoDir, path, branch, baseRef string) error {
	branchExists, err := BranchExists(repoDir, branch)
	if err != nil {
		return err
	}
	args := []string{"worktree", "add"}
	if branchExists {
		args = append(args, path, branch)
	} else {
		args = append(args, "-b", branch, path)
		if baseRef != "" {
			args = append(args, baseRef)
		}
	}
	_, err = runGit(repoDir, args...)
	return err
}

// RemoveWorktree runs `git worktree remove`. If force is true, the --force flag is added.
func RemoveWorktree(repoDir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := runGit(repoDir, args...)
	return err
}

// DeleteBranch removes a local branch. force=true uses -D, otherwise -d.
func DeleteBranch(repoDir, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := runGit(repoDir, "branch", flag, branch)
	return err
}

// BranchExists reports whether the given local branch exists in repoDir.
func BranchExists(repoDir, branch string) (bool, error) {
	_, err := runGit(repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return false, nil
	}
	return false, err
}

// IsDirty reports whether the worktree at path has uncommitted changes or untracked files.
func IsDirty(path string) (bool, error) {
	out, err := runGit(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// HasUnpushedCommits reports whether the current branch in path has commits not yet
// pushed to its tracked upstream. Returns false if no upstream is configured.
func HasUnpushedCommits(path string) (bool, error) {
	_, err := runGit(path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		// No upstream — treat as no unpushed commits (caller may decide).
		return false, nil
	}
	out, err := runGit(path, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "0", nil
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return outBuf.String(), &gitError{
			args:   args,
			stderr: strings.TrimSpace(errBuf.String()),
			err:    err,
		}
	}
	return outBuf.String(), nil
}

type gitError struct {
	args   []string
	stderr string
	err    error
}

func (e *gitError) Error() string {
	if e.stderr != "" {
		return fmt.Sprintf("git %s: %s", strings.Join(e.args, " "), e.stderr)
	}
	return fmt.Sprintf("git %s: %v", strings.Join(e.args, " "), e.err)
}

func (e *gitError) Unwrap() error { return e.err }

func isNotRepoErr(err error) bool {
	var ge *gitError
	if errors.As(err, &ge) {
		return strings.Contains(ge.stderr, "not a git repository")
	}
	return false
}
