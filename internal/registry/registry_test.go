package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFindByWorktreePath_ResolvesSymlinks(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("symlink test relies on POSIX symlinks")
	}

	root := t.TempDir()

	// realDir is the canonical worktree path; linkDir is a symlink that resolves to it.
	realDir := filepath.Join(root, "real", "worktree")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	linkParent := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "real"), linkParent); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	linkedWorktree := filepath.Join(linkParent, "worktree")

	// Stand up a registry on disk that stores the symlink-unresolved form (mirrors
	// the on-the-wire shape produced by older nerve versions before the fix).
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".nerve"), 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	h := Open(repoRoot)
	if err := h.With(func(reg *Registry) error {
		return reg.Claim(8001, Allocation{
			WorktreePath: linkedWorktree, // unresolved path
			Branch:       "feat",
			Offset:       1,
		})
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	// Look up using the symlink-resolved path; FindByWorktreePath must canonicalize
	// both sides and find a match.
	port, alloc, found, err := h.FindByWorktreePath(realDir)
	if err != nil {
		t.Fatalf("FindByWorktreePath(real): %v", err)
	}
	if !found {
		t.Fatalf("expected to find allocation via resolved path %q (stored as %q)", realDir, linkedWorktree)
	}
	if port != "8001" || alloc.Offset != 1 {
		t.Fatalf("unexpected allocation: port=%q offset=%d", port, alloc.Offset)
	}

	// And lookup using the symlinked form still works (the canonicalization is
	// symmetric).
	if _, _, found2, err := h.FindByWorktreePath(linkedWorktree); err != nil || !found2 {
		t.Fatalf("FindByWorktreePath(link) err=%v found=%v", err, found2)
	}
}

func TestReleaseByWorktreePath_ResolvesSymlinks(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("symlink test relies on POSIX symlinks")
	}

	root := t.TempDir()
	realDir := filepath.Join(root, "real", "worktree")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	linked := filepath.Join(root, "link", "worktree")

	reg := &Registry{Allocations: map[string]Allocation{}}
	if err := reg.Claim(8002, Allocation{WorktreePath: realDir, Offset: 2}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	port, ok := reg.ReleaseByWorktreePath(linked)
	if !ok || port != "8002" {
		t.Fatalf("ReleaseByWorktreePath(link) port=%q ok=%v", port, ok)
	}
	if len(reg.Allocations) != 0 {
		t.Fatalf("expected allocations to be empty, got %d", len(reg.Allocations))
	}
}
