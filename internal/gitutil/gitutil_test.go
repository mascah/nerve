package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// initRepo creates a fresh repo at dir with deterministic identity and a single
// committed file so we have a HEAD to diff against.
func initRepo(t *testing.T, dir string) {
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
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "to_delete.txt"), []byte("bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "to_rename.txt"), []byte("name\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
}

func TestDirtyFiles_Clean(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	files, err := DirtyFiles(dir)
	if err != nil {
		t.Fatalf("DirtyFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected clean repo to have no dirty files, got %v", files)
	}

	dirty, err := IsDirty(dir)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Fatalf("expected IsDirty false on clean repo, got true")
	}
}

func TestDirtyFiles_AllStates(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// 1) Untracked file.
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 2) Tracked + modified.
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 3) Tracked + deleted.
	if err := os.Remove(filepath.Join(dir, "to_delete.txt")); err != nil {
		t.Fatal(err)
	}
	// 4) Rename via `git mv` (staged R entry).
	run("mv", "to_rename.txt", "renamed.txt")

	got, err := DirtyFiles(dir)
	if err != nil {
		t.Fatalf("DirtyFiles: %v", err)
	}

	// Order is git-defined; assert as a set + spot-check renamed → destination only.
	want := []string{
		"untracked.txt",
		"tracked.txt",
		"to_delete.txt",
		"renamed.txt",
	}
	gotSorted := append([]string{}, got...)
	wantSorted := append([]string{}, want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if !reflect.DeepEqual(gotSorted, wantSorted) {
		t.Fatalf("DirtyFiles set mismatch:\n got: %v\nwant: %v", gotSorted, wantSorted)
	}

	// Rename must show only the destination (no "old -> new" leak, no "to_rename.txt").
	for _, f := range got {
		if strings.Contains(f, " -> ") {
			t.Fatalf("DirtyFiles leaked arrow syntax: %q", f)
		}
		if f == "to_rename.txt" {
			t.Fatalf("DirtyFiles returned rename source, expected destination only: %q", f)
		}
	}

	dirty, err := IsDirty(dir)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if !dirty {
		t.Fatalf("expected IsDirty true on dirty repo")
	}
}
