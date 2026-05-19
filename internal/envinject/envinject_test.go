package envinject

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/registry"
)

// TestCompute_SymlinkedWorktree exercises the bug where the registry stored a
// fully-resolved worktree path but the lookup arrived via a symlinked path (the
// shape `git rev-parse --show-toplevel` returns on macOS, where /tmp resolves to
// /private/tmp). Before the canonicalize-on-both-sides fix this returned (nil, nil).
func TestCompute_SymlinkedWorktree(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("symlink test relies on POSIX symlinks")
	}

	// Realdir is where the main checkout (and the linked worktree) physically live.
	// We then create `linkRoot -> realDir/repo` so the worktree can be referenced
	// via two different absolute paths that resolve to the same inode.
	realDir := t.TempDir()
	repoPath := filepath.Join(realDir, "repo")
	if err := os.Mkdir(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	gitRun(t, repoPath, "init", "-q", "-b", "main")
	gitRun(t, repoPath, "config", "user.email", "test@example.com")
	gitRun(t, repoPath, "config", "user.name", "Test")
	gitRun(t, repoPath, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repoPath, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-q", "-m", "seed")

	// Write a minimal .nerve/config.yaml with one service so envinject takes the
	// configured path.
	cfg := &config.ProjectConfig{
		Version: config.CurrentConfigVersion,
		Project: config.ProjectSettings{PortOffset: 0, PoolSize: 10},
		Services: []config.Service{{
			ID:       "django",
			BasePort: 8000,
			EnvKey:   "DJANGO_PORT",
			Primary:  true,
		}},
	}
	if err := config.SaveProjectConfig(repoPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Create the worktree under repo/.worktrees/feat. Use the resolved repoPath
	// here so git stores the canonical path.
	worktreePath := filepath.Join(repoPath, ".worktrees", "feat")
	gitRun(t, repoPath, "worktree", "add", "-b", "feat", worktreePath)

	// Seed the registry by hand with the canonical worktree path (mirrors what
	// `nerve new` writes after the canonicalize-in-create change).
	h := registry.Open(repoPath)
	if err := h.With(func(reg *registry.Registry) error {
		registry.ConfigurePool(reg, cfg)
		return reg.Claim(8001, registry.Allocation{
			WorktreePath:   worktreePath,
			Branch:         "feat",
			Offset:         1,
			PrimaryService: "django",
			CreatedByNerve: true,
		})
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	// Now construct a *symlinked* alias to repo/ so the path we hand to Compute
	// is byte-different from the stored one but resolves to the same inode.
	linkRoot := filepath.Join(realDir, "link")
	if err := os.Symlink(repoPath, linkRoot); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	linkedWorktree := filepath.Join(linkRoot, ".worktrees", "feat")

	vars, err := Compute(linkedWorktree)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if vars["DJANGO_PORT"] != "8001" {
		t.Fatalf("expected DJANGO_PORT=8001, got %q (full map: %v)", vars["DJANGO_PORT"], vars)
	}
}

func TestComputeVerbose_NoAllocationReason(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("relies on git on PATH")
	}
	repoPath := t.TempDir()
	gitRun(t, repoPath, "init", "-q", "-b", "main")
	gitRun(t, repoPath, "config", "user.email", "test@example.com")
	gitRun(t, repoPath, "config", "user.name", "Test")
	gitRun(t, repoPath, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repoPath, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoPath, "add", ".")
	gitRun(t, repoPath, "commit", "-q", "-m", "seed")

	cfg := &config.ProjectConfig{
		Version: config.CurrentConfigVersion,
		Project: config.ProjectSettings{PoolSize: 10},
		Services: []config.Service{{
			ID: "django", BasePort: 8000, EnvKey: "DJANGO_PORT", Primary: true,
		}},
	}
	if err := config.SaveProjectConfig(repoPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	worktreePath := filepath.Join(repoPath, ".worktrees", "feat")
	gitRun(t, repoPath, "worktree", "add", "-b", "feat", worktreePath)

	vars, reason, err := ComputeVerbose(worktreePath)
	if err != nil {
		t.Fatalf("ComputeVerbose: %v", err)
	}
	if len(vars) != 0 {
		t.Fatalf("expected empty vars when no allocation, got %v", vars)
	}
	if !strings.Contains(reason, "no allocation for worktree") {
		t.Fatalf("expected no-allocation reason, got %q", reason)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
