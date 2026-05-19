package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// makeTree builds the requested files/directories under root. A path ending
// in "/" is created as a directory; otherwise it's a regular file with the
// trailing segment as both name and minimal content.
func makeTree(t *testing.T, root string, entries []string) {
	t.Helper()
	for _, e := range entries {
		full := filepath.Join(root, filepath.FromSlash(e))
		if strings.HasSuffix(e, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListPathSuggestions_TopLevelPrefix(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, []string{
		".env",
		".env.example",
		".envrc",
		".npmrc",
		"README.md",
		"src/",
	})

	got := listPathSuggestions(root, ".env", 10)
	want := []string{".env", ".env.example", ".envrc"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prefix .env: got %v want %v", got, want)
	}
}

func TestListPathSuggestions_CaseInsensitive(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, []string{"README.md", "readme.txt", "Makefile"})

	got := listPathSuggestions(root, "read", 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 readme-prefixed matches, got %v", got)
	}
	for _, g := range got {
		lower := strings.ToLower(g)
		if !strings.HasPrefix(lower, "read") {
			t.Errorf("entry %q does not start with read (case-insensitive)", g)
		}
	}
}

func TestListPathSuggestions_ExcludesSkipDirs(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, []string{
		".git/HEAD",
		".git/refs/heads/main",
		"node_modules/foo/index.js",
		".worktrees/feature/.env",
		".nerve/config.yaml",
		"src/app.go",
	})

	// Top-level scan: the skip dirs are themselves matched only as directory
	// entries (and we don't descend into them). The contents inside them must
	// not appear.
	all := listPathSuggestions(root, "", 100)
	for _, e := range all {
		switch {
		case strings.HasPrefix(e, ".git/"):
			t.Errorf("unexpected .git child in suggestions: %q", e)
		case strings.HasPrefix(e, "node_modules/"):
			t.Errorf("unexpected node_modules child in suggestions: %q", e)
		case strings.HasPrefix(e, ".worktrees/"):
			t.Errorf("unexpected .worktrees child in suggestions: %q", e)
		case strings.HasPrefix(e, ".nerve/"):
			t.Errorf("unexpected .nerve child in suggestions: %q", e)
		}
	}

	// Even when the user explicitly types into one of these skip dirs we
	// refuse to surface contents (we never descend into them at all).
	got := listPathSuggestions(root, "node_modules/", 10)
	if len(got) != 0 {
		t.Errorf("expected zero matches inside skip dir, got %v", got)
	}
}

func TestListPathSuggestions_DescendIntoSubdir(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, []string{
		"config/settings.py",
		"config/settings.local.py",
		"config/urls.py",
		"src/app.go",
	})

	got := listPathSuggestions(root, "config/sett", 10)
	want := []string{"config/settings.local.py", "config/settings.py"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("descend into config/: got %v want %v", got, want)
	}
}

func TestListPathSuggestions_RespectsMax(t *testing.T) {
	root := t.TempDir()
	entries := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		entries = append(entries, "file_"+string(rune('a'+i)))
	}
	makeTree(t, root, entries)

	got := listPathSuggestions(root, "file_", 5)
	if len(got) != 5 {
		t.Errorf("expected exactly 5 results when max=5, got %d (%v)", len(got), got)
	}
}

func TestListPathSuggestions_DirSuffix(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, []string{"docs/", "docs/index.md", "doctor.txt"})

	got := listPathSuggestions(root, "doc", 10)
	var sawDir bool
	for _, g := range got {
		if g == "docs/" {
			sawDir = true
		}
	}
	if !sawDir {
		t.Errorf("expected docs/ in suggestions (got %v)", got)
	}
}
