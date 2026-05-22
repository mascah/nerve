package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/leases"
	"github.com/mascah/nerve/internal/registry"
)

// gitRun runs a git command in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// initRepo creates a git repo with a single commit and returns its path. It also
// points XDG_CONFIG_HOME at a throwaway dir so the user-wide leases store stays
// isolated from the developer's real ~/.config/nerve.
func initRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test")
	gitRun(t, repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "seed.txt")
	gitRun(t, repo, "commit", "-q", "-m", "seed")
	return repo
}

// configWithService returns a minimal configured ProjectConfig with one primary
// service and the given post_create hook commands.
func configWithService(postCreate ...string) *config.ProjectConfig {
	return &config.ProjectConfig{
		Version: 1,
		Project: config.ProjectSettings{
			WorktreeRoot: config.DefaultWorktreeRoot,
			PoolSize:     config.DefaultPoolSize,
		},
		Services: []config.Service{
			{ID: "django", BasePort: 8000, EnvKey: "DJANGO_PORT", Primary: true},
		},
		Hooks: config.LifecycleHooks{PostCreate: postCreate},
	}
}

// TestCreate_RollbackOnPostCreateHookFailure is the regression test for the leak
// bug: a post_create hook failing must leave no git worktree, no registry
// allocation, and no global lease behind.
func TestCreate_RollbackOnPostCreateHookFailure(t *testing.T) {
	repo := initRepo(t)
	cfg := configWithService("exit 1")

	_, err := Create(CreateOptions{RepoRoot: repo, Branch: "feat", Cfg: cfg})
	if err == nil {
		t.Fatalf("expected Create to fail when a post_create hook exits non-zero")
	}

	wtPath := filepath.Join(repo, ".worktrees", "feat")
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir %s still exists after rollback (stat err: %v)", wtPath, statErr)
	}

	reg, err := registry.Open(repo).Read()
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if len(reg.Allocations) != 0 {
		t.Fatalf("registry still holds %d allocation(s) after rollback: %+v", len(reg.Allocations), reg.Allocations)
	}

	store, err := leases.Open()
	if err != nil {
		t.Fatalf("open leases: %v", err)
	}
	cur, err := store.Read()
	if err != nil {
		t.Fatalf("read leases: %v", err)
	}
	if len(cur) != 0 {
		t.Fatalf("leases store still holds %d lease(s) after rollback: %+v", len(cur), cur)
	}
}

// TestCreate_PostCreateHookReceivesAllocatedPort verifies the allocated port is
// exported into the hook environment (previously hooks only saw os.Environ()).
func TestCreate_PostCreateHookReceivesAllocatedPort(t *testing.T) {
	repo := initRepo(t)
	// The hook runs with cwd == worktree path; record the injected port to a file.
	cfg := configWithService(`printf '%s' "$DJANGO_PORT" > hook_port.txt`)

	res, err := Create(CreateOptions{RepoRoot: repo, Branch: "feat", Cfg: cfg})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(res.Path, "hook_port.txt"))
	if err != nil {
		t.Fatalf("read hook output: %v", err)
	}
	want := strconv.Itoa(res.PortByService["django"])
	if string(got) != want {
		t.Fatalf("hook saw DJANGO_PORT=%q; want allocated port %q", got, want)
	}
	if res.PrimaryPort == 0 || res.PortByService["django"] != res.PrimaryPort {
		t.Fatalf("unexpected allocation: primary=%d byService=%v", res.PrimaryPort, res.PortByService)
	}
}

// TestCreate_RejectsBranchPathTraversal ensures a branch name with a ".." segment
// is rejected before any worktree is created (path-traversal guard).
func TestCreate_RejectsBranchPathTraversal(t *testing.T) {
	repo := initRepo(t)

	_, err := Create(CreateOptions{RepoRoot: repo, Branch: "../../escape", Cfg: nil})
	if err == nil {
		t.Fatalf("expected Create to reject a branch with a .. path segment")
	}
	if !strings.Contains(err.Error(), "path segment") {
		t.Fatalf("error %q does not mention the rejected path segment", err)
	}
	escaped := filepath.Join(filepath.Dir(repo), "escape")
	if _, statErr := os.Stat(escaped); !os.IsNotExist(statErr) {
		t.Fatalf("a worktree escaped to %s", escaped)
	}
}

func TestHasDotDotSegment(t *testing.T) {
	cases := map[string]bool{
		"feature/foo":  false,
		"fix-bug":      false,
		"../escape":    true,
		"a/../../b":    true,
		"..":           true,
		"foo..bar":     false, // ".." only counts as a whole segment, not a substring
		"release/v1.2": false,
	}
	for in, want := range cases {
		if got := hasDotDotSegment(in); got != want {
			t.Errorf("hasDotDotSegment(%q) = %v; want %v", in, got, want)
		}
	}
}
