package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	t.Setenv("NERVE_TEST_VAR", "expanded")

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/abs/path", "/abs/path"},
		{"relative/path", "relative/path"},
		{"~", home},
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"~not-a-home", "~not-a-home"},
		{"$NERVE_TEST_VAR/x", "expanded/x"},
		{"${NERVE_TEST_VAR}/x", "expanded/x"},
		{"~/$NERVE_TEST_VAR", filepath.Join(home, "expanded")},
	}
	for _, c := range cases {
		got, err := ExpandPath(c.in)
		if err != nil {
			t.Errorf("ExpandPath(%q) returned error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
