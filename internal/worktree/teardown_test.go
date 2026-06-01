package worktree

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/gitutil"
)

// gitRepoWithWorktree builds a temp repo with one commit and one linked worktree on
// branch "feature", returning the main checkout path and the worktree path.
func gitRepoWithWorktree(t *testing.T) (repo, wtPath string) {
	t.Helper()
	repo = t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
		}
	}
	run(repo, "init", "-q", "-b", "main")
	run(repo, "config", "user.email", "test@example.com")
	run(repo, "config", "user.name", "Test")
	run(repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "seed.txt")
	run(repo, "commit", "-q", "-m", "seed")

	wtPath = filepath.Join(repo, ".worktrees", "feature")
	run(repo, "worktree", "add", "-q", "-b", "feature", wtPath)
	return repo, wtPath
}

func worktreePaths(t *testing.T, repo string) []string {
	t.Helper()
	wts, err := gitutil.ListWorktrees(repo)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	var paths []string
	for _, w := range wts {
		paths = append(paths, w.Path)
	}
	return paths
}

// TestRemoveWorktreeDir_BackgroundRenamesAndPrunes verifies the fast path moves the
// worktree into .nerve/trash, reconciles git's metadata synchronously (prune), and
// hands the byte-delete to the detached spawner.
func TestRemoveWorktreeDir_BackgroundRenamesAndPrunes(t *testing.T) {
	repo, wtPath := gitRepoWithWorktree(t)

	var gotArgs []string
	orig := spawnDetachedFn
	spawnDetachedFn = func(_ map[string]string, args ...string) error { gotArgs = args; return nil }
	defer func() { spawnDetachedFn = orig }()

	if err := removeWorktreeDir(repo, wtPath, "feature", true, true, io.Discard); err != nil {
		t.Fatalf("removeWorktreeDir(background): %v", err)
	}

	// Original worktree dir is gone...
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present at %s (err=%v)", wtPath, err)
	}
	// ...moved into .nerve/trash/...
	trashDir := filepath.Join(repo, ".nerve", "trash")
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		t.Fatalf("read trash dir: %v", err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "feature-") {
		t.Fatalf("trash entries = %v; want one feature-<rand>", names(entries))
	}
	// ...git metadata reconciled (prune ran synchronously) so only main remains...
	if paths := worktreePaths(t, repo); len(paths) != 1 {
		t.Fatalf("worktrees after background remove = %v; want only the main checkout", paths)
	}
	// ...and the byte-delete was handed to the detached spawner.
	want := []string{"gc-trash", "--repo", repo}
	if strings.Join(gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("spawnDetached args = %v; want %v", gotArgs, want)
	}
}

// TestRemoveWorktreeDir_SyncPath verifies the default path uses git worktree remove
// (no trash dir is created) and fully deletes the worktree before returning.
func TestRemoveWorktreeDir_SyncPath(t *testing.T) {
	repo, wtPath := gitRepoWithWorktree(t)

	called := false
	orig := spawnDetachedFn
	spawnDetachedFn = func(_ map[string]string, _ ...string) error { called = true; return nil }
	defer func() { spawnDetachedFn = orig }()

	if err := removeWorktreeDir(repo, wtPath, "feature", true, false, io.Discard); err != nil {
		t.Fatalf("removeWorktreeDir(sync): %v", err)
	}
	if called {
		t.Fatalf("sync path must not spawn a detached delete")
	}
	if _, err := os.Stat(filepath.Join(repo, ".nerve", "trash")); !os.IsNotExist(err) {
		t.Fatalf("sync path must not create a trash dir")
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after sync remove: %v", err)
	}
	if paths := worktreePaths(t, repo); len(paths) != 1 {
		t.Fatalf("worktrees after sync remove = %v; want only the main checkout", paths)
	}
}

// TestChdirAwayIfInside moves the process out of a doomed worktree before deletion.
func TestChdirAwayIfInside(t *testing.T) {
	repo := t.TempDir()
	target := filepath.Join(repo, "wt")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	canonRepo, _ := gitutil.CanonicalPath(repo)
	canonTarget, _ := gitutil.CanonicalPath(target)

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	// Standing inside the target → should be moved to repoRoot.
	if err := os.Chdir(canonTarget); err != nil {
		t.Fatal(err)
	}
	chdirAwayIfInside(canonTarget, canonRepo)
	if now := mustCanonWd(t); now != canonRepo {
		t.Fatalf("cwd after guard = %q; want %q", now, canonRepo)
	}

	// Standing outside the target → cwd unchanged.
	if err := os.Chdir(canonRepo); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	canonOther, _ := gitutil.CanonicalPath(other)
	chdirAwayIfInside(canonOther, canonRepo) // target is unrelated dir
	if now := mustCanonWd(t); now != canonRepo {
		t.Fatalf("cwd should be unchanged; got %q want %q", now, canonRepo)
	}
}

func mustCanonWd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	c, _ := gitutil.CanonicalPath(wd)
	return c
}

func names(entries []os.DirEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
