package jsonstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doc is a minimal Document used to exercise the store.
type doc struct {
	Version int            `json:"version"`
	Entries map[string]int `json:"entries"`
}

func (d *doc) SchemaVersion() int     { return d.Version }
func (d *doc) SetSchemaVersion(v int) { d.Version = v }

func newStore(t *testing.T, current int) *Store[*doc] {
	t.Helper()
	dir := t.TempDir()
	return New(Config[*doc]{
		Path:     filepath.Join(dir, "store.json"),
		LockPath: filepath.Join(dir, "store.json.lock"),
		Current:  current,
		Label:    "test store",
		NewEmpty: func() *doc { return &doc{Entries: map[string]int{}} },
	})
}

func TestReadMissingFileReturnsEmpty(t *testing.T) {
	s := newStore(t, 3)
	d, err := s.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if d.Entries == nil {
		t.Fatalf("expected non-nil Entries map from NewEmpty")
	}
	if len(d.Entries) != 0 {
		t.Fatalf("expected empty store, got %d entries", len(d.Entries))
	}
}

func TestWithPersistsAtomicallyAndStampsVersion(t *testing.T) {
	s := newStore(t, 3)
	if err := s.With(func(d *doc) error {
		d.Entries["a"] = 1
		return nil
	}); err != nil {
		t.Fatalf("With: %v", err)
	}

	// The on-disk file should exist, be 0644, and carry the current version.
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("file mode = %o, want 0644", perm)
	}
	d, err := s.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if d.Version != 3 {
		t.Errorf("Version = %d, want 3 (current stamped on write)", d.Version)
	}
	if d.Entries["a"] != 1 {
		t.Errorf("Entries[a] = %d, want 1", d.Entries["a"])
	}

	// No stray temp files should be left in the directory.
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Path()), "*.tmp.*"))
	if len(matches) != 0 {
		t.Errorf("expected no leftover temp files, found %v", matches)
	}
}

func TestWithErrorLeavesFileUntouched(t *testing.T) {
	s := newStore(t, 1)
	if err := s.With(func(d *doc) error { d.Entries["keep"] = 7; return nil }); err != nil {
		t.Fatalf("seed: %v", err)
	}
	wantErr := "boom"
	if err := s.With(func(d *doc) error {
		d.Entries["wipe"] = 9
		return &stringErr{wantErr}
	}); err == nil || err.Error() != wantErr {
		t.Fatalf("expected error %q, got %v", wantErr, err)
	}
	d, _ := s.Read()
	if _, ok := d.Entries["wipe"]; ok {
		t.Errorf("failed callback should not have persisted its mutation")
	}
	if d.Entries["keep"] != 7 {
		t.Errorf("prior state lost after failed callback")
	}
}

func TestVersionZeroCoercedToCurrent(t *testing.T) {
	s := newStore(t, 2)
	// Write a document on disk with version 0 (older nerve / hand-edited).
	writeRaw(t, s.Path(), `{"version":0,"entries":{"x":1}}`)
	d, err := s.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if d.Version != 2 {
		t.Errorf("version 0 should coerce to current 2, got %d", d.Version)
	}
}

func TestOlderVersionAccepted(t *testing.T) {
	s := newStore(t, 5)
	writeRaw(t, s.Path(), `{"version":3,"entries":{"x":1}}`)
	d, err := s.Read()
	if err != nil {
		t.Fatalf("older version should be accepted, got: %v", err)
	}
	// Older versions are forward-compatible: the in-memory value keeps the stored
	// version until a write re-stamps it to current.
	if d.Version != 3 {
		t.Errorf("expected stored version 3 preserved on read, got %d", d.Version)
	}
}

func TestFutureVersionRejected(t *testing.T) {
	s := newStore(t, 2)
	writeRaw(t, s.Path(), `{"version":9,"entries":{"x":1}}`)
	_, err := s.Read()
	if err == nil {
		t.Fatalf("expected rejection of future version")
	}
	msg := err.Error()
	for _, want := range []string{"newer version of nerve", "v9 > v2", "upgrade nerve"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
	// With must reject too (so we never overwrite a newer file).
	if err := s.With(func(d *doc) error { return nil }); err == nil {
		t.Errorf("expected With to also reject a future-version file")
	}
}

func writeRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write raw: %v", err)
	}
}

type stringErr struct{ s string }

func (e *stringErr) Error() string { return e.s }
