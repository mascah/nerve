package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// initRepoForWorktrees creates a fresh git repo at dir with deterministic
// identity and a single committed file, mirroring the helper in
// internal/gitutil/gitutil_test.go.
func initRepoForWorktrees(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
}

func TestLoadWorktreeRows_SkipsMainAndSortsByBranch(t *testing.T) {
	repo := t.TempDir()
	initRepoForWorktrees(t, repo)

	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// Two linked worktrees on new branches.
	wtRoot := t.TempDir()
	wtA := filepath.Join(wtRoot, "feature-a")
	wtB := filepath.Join(wtRoot, "feature-b")
	gitRun("worktree", "add", "-b", "feature-b", wtB)
	gitRun("worktree", "add", "-b", "feature-a", wtA)

	rows, err := loadWorktreeRows(repo, nil)
	if err != nil {
		t.Fatalf("loadWorktreeRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (main excluded), got %d: %+v", len(rows), rows)
	}

	// Must be sorted by branch.
	branches := []string{rows[0].Branch, rows[1].Branch}
	wantBranches := []string{"feature-a", "feature-b"}
	if !equalStringSlices(branches, wantBranches) {
		t.Fatalf("branches not sorted; got %v want %v", branches, wantBranches)
	}

	// State for both untouched worktrees should be "clean".
	for _, r := range rows {
		if r.State != "clean" {
			t.Errorf("worktree %q state = %q, want clean", r.Branch, r.State)
		}
		if r.Path == "" {
			t.Errorf("worktree %q has empty path", r.Branch)
		}
		// No registry/cfg → PrimaryPort should be zero.
		if r.PrimaryPort != 0 {
			t.Errorf("worktree %q PrimaryPort = %d, want 0 (no cfg)", r.Branch, r.PrimaryPort)
		}
	}
}

func TestLoadWorktreeRows_DirtyState(t *testing.T) {
	repo := t.TempDir()
	initRepoForWorktrees(t, repo)

	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	wtRoot := t.TempDir()
	wt := filepath.Join(wtRoot, "dirty-branch")
	gitRun("worktree", "add", "-b", "dirty-branch", wt)

	// Make the worktree dirty with one untracked file and one modified file.
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "seed.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, err := loadWorktreeRows(repo, nil)
	if err != nil {
		t.Fatalf("loadWorktreeRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 worktree row, got %d", len(rows))
	}
	if !strings.HasPrefix(rows[0].State, "dirty") {
		t.Errorf("expected dirty state, got %q", rows[0].State)
	}
	if !strings.Contains(rows[0].State, "2 files") {
		t.Errorf("expected dirty count 2 files, got %q", rows[0].State)
	}
}

func TestLoadWorktreeRows_EmptyWhenOnlyMain(t *testing.T) {
	repo := t.TempDir()
	initRepoForWorktrees(t, repo)

	rows, err := loadWorktreeRows(repo, nil)
	if err != nil {
		t.Fatalf("loadWorktreeRows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows (only main checkout exists), got %d: %+v", len(rows), rows)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string{}, a...)
	bb := append([]string{}, b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	// Also check the *exact* order matches (since loadWorktreeRows guarantees sort).
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
