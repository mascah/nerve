package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
)

// gitRunCLI runs a git command in dir, failing the test on error.
func gitRunCLI(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// newRegisteredRepo creates a one-commit git repo and registers it in a throwaway
// global registry under name. It returns the repo's canonical path — the form
// gitutil.Discover reports — so cwd-based project inference matches the registry
// entry (macOS temp dirs are symlinks that git resolves). It does NOT chdir; the
// caller chdir's in to exercise inference.
func newRegisteredRepo(t *testing.T, name string) string {
	t.Helper()
	setXDGHome(t) // isolates ~/.config/nerve/projects.yaml
	repo := t.TempDir()
	gitRunCLI(t, repo, "init", "-q", "-b", "main")
	gitRunCLI(t, repo, "config", "user.email", "test@example.com")
	gitRunCLI(t, repo, "config", "user.name", "Test")
	gitRunCLI(t, repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunCLI(t, repo, "add", "seed.txt")
	gitRunCLI(t, repo, "commit", "-q", "-m", "seed")

	canon, err := gitutil.CanonicalPath(repo)
	if err != nil {
		t.Fatalf("canonicalize repo: %v", err)
	}
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if err := reg.AddProject(config.ProjectEntry{Name: name, Path: canon}); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := config.SaveGlobalRegistry(reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	return canon
}

// TestNewAndRemove_ProjectInferredFromCwd is the end-to-end check for the cwd-default
// ergonomic: from inside a registered project, `nerve new <branch>` and
// `nerve remove <branch>` work without naming the project.
func TestNewAndRemove_ProjectInferredFromCwd(t *testing.T) {
	repo := newRegisteredRepo(t, "demo")
	t.Chdir(repo)

	out, _, err := execCmd(t, NewRootCmd(), "new", "feat-x", "--minimal")
	if err != nil {
		t.Fatalf("new feat-x (cwd-inferred): %v", err)
	}
	if !strings.Contains(out, "worktree created") {
		t.Errorf("expected 'worktree created' in output, got: %q", out)
	}
	wtPath := filepath.Join(repo, ".worktrees", "feat-x")
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Fatalf("expected worktree at %s, stat err: %v", wtPath, statErr)
	}

	out, _, err = execCmd(t, NewRootCmd(), "remove", "feat-x", "--force")
	if err != nil {
		t.Fatalf("remove feat-x (cwd-inferred): %v", err)
	}
	if !strings.Contains(out, "removed worktree") {
		t.Errorf("expected 'removed worktree' in output, got: %q", out)
	}
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Errorf("worktree dir still exists after remove: %v", statErr)
	}
}

// TestNew_ExplicitProjectStillWorks confirms the legacy 2-arg form is unchanged and
// selects the project by name, independent of cwd.
func TestNew_ExplicitProjectStillWorks(t *testing.T) {
	repo := newRegisteredRepo(t, "demo")
	t.Chdir(t.TempDir()) // neutral cwd: prove the arg, not cwd, picks the project

	out, _, err := execCmd(t, NewRootCmd(), "new", "demo", "feat-y", "--minimal")
	if err != nil {
		t.Fatalf("new demo feat-y: %v", err)
	}
	if !strings.Contains(out, "worktree created") {
		t.Errorf("expected 'worktree created', got: %q", out)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".worktrees", "feat-y")); statErr != nil {
		t.Fatalf("expected worktree dir, stat err: %v", statErr)
	}
}

// TestNew_CwdNotRegistered_Errors checks the help-tuned error when the project can't
// be inferred from cwd and wasn't named.
func TestNew_CwdNotRegistered_Errors(t *testing.T) {
	setXDGHome(t)
	t.Chdir(t.TempDir()) // not a git repo, not registered

	_, _, err := execCmd(t, NewRootCmd(), "new", "feat-z", "--minimal")
	if err == nil {
		t.Fatal("expected error for unregistered cwd, got nil")
	}
	if !strings.Contains(err.Error(), "registered nerve project") {
		t.Errorf("error should mention 'registered nerve project': %v", err)
	}
}

// TestNew_RequiresBranchArg confirms the branch positional is still mandatory.
func TestNew_RequiresBranchArg(t *testing.T) {
	setXDGHome(t)
	_, _, err := execCmd(t, NewRootCmd(), "new")
	if err == nil {
		t.Fatal("expected error when no args given to `new`, got nil")
	}
}
