package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestDirtyError_UnwrapsToErrDirty guarantees existing callers that check
// errors.Is(err, ErrDirty) keep working when Remove returns a *DirtyError.
func TestDirtyError_UnwrapsToErrDirty(t *testing.T) {
	de := &DirtyError{Files: []string{"a", "b"}}
	if !errors.Is(de, ErrDirty) {
		t.Fatalf("errors.Is(*DirtyError, ErrDirty) = false; want true")
	}
	if de.Error() != ErrDirty.Error() {
		t.Fatalf("DirtyError.Error() = %q; want %q", de.Error(), ErrDirty.Error())
	}
	var got *DirtyError
	if !errors.As(de, &got) {
		t.Fatalf("errors.As to *DirtyError failed")
	}
	if !reflect.DeepEqual(got.Files, []string{"a", "b"}) {
		t.Fatalf("DirtyError.Files = %v; want [a b]", got.Files)
	}
}

// TestRemove_DirtyReturnsDirtyError drives Remove against a real dirty worktree
// and confirms the returned error is a *DirtyError carrying the file list.
func TestRemove_DirtyReturnsDirtyError(t *testing.T) {
	repo := t.TempDir()

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
		}
	}

	// Main checkout with one commit.
	run(repo, "init", "-q", "-b", "main")
	run(repo, "config", "user.email", "test@example.com")
	run(repo, "config", "user.name", "Test")
	run(repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "seed.txt")
	run(repo, "commit", "-q", "-m", "seed")

	// Linked worktree.
	wtPath := filepath.Join(repo, ".worktrees", "feature")
	run(repo, "worktree", "add", "-b", "feature", wtPath)

	// Dirty the worktree: one untracked, one modified.
	if err := os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "seed.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Remove(RemoveOptions{
		RepoRoot:     repo,
		WorktreePath: wtPath,
		Branch:       "feature",
	})
	if err == nil {
		t.Fatalf("expected Remove to fail on dirty worktree, got nil")
	}
	if !errors.Is(err, ErrDirty) {
		t.Fatalf("errors.Is(err, ErrDirty) = false; err = %v", err)
	}
	var de *DirtyError
	if !errors.As(err, &de) {
		t.Fatalf("errors.As(err, *DirtyError) = false; err = %v", err)
	}

	got := append([]string{}, de.Files...)
	sort.Strings(got)
	want := []string{"new.txt", "seed.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DirtyError.Files = %v; want %v", got, want)
	}
}
