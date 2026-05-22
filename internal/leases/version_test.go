package leases

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedLeasesFile writes raw JSON to the sandbox store's on-disk path, creating
// the parent directory (the store only does so lazily on read/write).
func seedLeasesFile(t *testing.T, s *Store, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(s.Path(), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestVersionZeroCoercedToCurrent(t *testing.T) {
	s := sandboxStore(t)
	seedLeasesFile(t, s, `{"version":0,"leases":{"8001":{"worktree_path":"/x"}}}`)
	got, err := s.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(got))
	}
	// A subsequent write must stamp the current version on disk.
	if err := s.With(func(m map[int]Lease) error { return nil }); err != nil {
		t.Fatalf("With: %v", err)
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if !strings.Contains(string(raw), `"version": 1`) {
		t.Errorf("expected current version 1 stamped on disk, got: %s", raw)
	}
}

func TestOlderVersionAccepted(t *testing.T) {
	s := sandboxStore(t)
	// CurrentVersion is 1, so there is no older positive version to seed; assert
	// the current version is accepted as the baseline forward-compatible case.
	seedLeasesFile(t, s, `{"version":1,"leases":{"8001":{"worktree_path":"/x"}}}`)
	got, err := s.Read()
	if err != nil {
		t.Fatalf("current/older version should be accepted, got: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(got))
	}
}

func TestFutureVersionRejected(t *testing.T) {
	s := sandboxStore(t)
	seedLeasesFile(t, s, `{"version":7,"leases":{"8001":{"worktree_path":"/x"}}}`)
	_, err := s.Read()
	if err == nil {
		t.Fatalf("expected rejection of future leases version")
	}
	msg := err.Error()
	for _, want := range []string{"newer version of nerve", "v7 > v1", "upgrade nerve"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
	if err := s.With(func(m map[int]Lease) error { return nil }); err == nil {
		t.Errorf("expected With to also reject a future-version file")
	}
}
