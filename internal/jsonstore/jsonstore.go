// Package jsonstore is the shared lockable-JSON-document machinery used by
// internal/registry (per-project port registry) and internal/leases (user-wide
// port leases). Both stores want the same behavior: acquire a sibling flock
// (exclusive for mutations, shared for reads), MkdirAll the parent, read JSON
// (a missing file is an empty document), run a callback, then write atomically
// (temp file in the SAME directory + os.Rename, chmod 0644, defer-remove temp).
//
// The schema-version policy is enforced here so it stays identical across every
// nerve JSON store: a stored version <= current is accepted (0 coerces to
// current; older versions are forward-compatible), and a stored version >
// current is rejected with a clear "written by a newer version of nerve" error
// telling the user to upgrade.
package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/mascah/nerve/internal/atomicfile"
)

// Document is the on-disk shape a Store persists. The version accessors let the
// store enforce a single schema-version policy across all nerve JSON stores.
// Implementations should use a pointer receiver so the store can populate a
// freshly-allocated, decoded value.
type Document interface {
	// SchemaVersion returns the document's currently-recorded schema version.
	SchemaVersion() int
	// SetSchemaVersion records v as the document's schema version. The store
	// calls this to coerce a zero version up to current and to stamp the
	// current version before writing.
	SetSchemaVersion(v int)
}

// Store mediates locked, atomic, versioned access to a single JSON document of
// type T. T must be a pointer type whose pointee implements Document (so the
// store can both decode into it and mutate its version in place).
//
// A Store does NOT acquire its lock at construction; callers use With (mutating,
// exclusive) or Read (read-only, shared) per operation.
type Store[T Document] struct {
	path     string
	lockPath string
	current  int
	label    string
	newEmpty func() T
}

// Config configures a Store.
type Config[T Document] struct {
	// Path is the on-disk JSON file. Its parent directory is created on demand.
	Path string
	// LockPath is the sibling flock file guarding Path.
	LockPath string
	// Current is the schema version this build of nerve writes. Stored versions
	// greater than Current are rejected; a stored version of 0 coerces to it.
	Current int
	// Label names the kind of file for error messages (e.g. "port registry").
	Label string
	// NewEmpty returns a fresh, ready-to-use empty document, used when the file
	// does not exist yet. It must initialize any maps the callback relies on.
	NewEmpty func() T
}

// New returns a Store bound to cfg.
func New[T Document](cfg Config[T]) *Store[T] {
	return &Store[T]{
		path:     cfg.Path,
		lockPath: cfg.LockPath,
		current:  cfg.Current,
		label:    cfg.Label,
		newEmpty: cfg.NewEmpty,
	}
}

// Path returns the on-disk path the store reads/writes (mostly for diagnostics).
func (s *Store[T]) Path() string { return s.path }

// LockPath returns the sibling flock path (mostly for diagnostics).
func (s *Store[T]) LockPath() string { return s.lockPath }

// Read returns the current document under a shared flock held only for the
// duration of the read. A missing file yields a fresh empty document. The
// schema-version policy is enforced.
func (s *Store[T]) Read() (T, error) {
	var zero T
	if err := s.ensureDir(); err != nil {
		return zero, err
	}
	lk := flock.New(s.lockPath)
	if err := lk.RLock(); err != nil {
		return zero, fmt.Errorf("acquire shared lock %s: %w", s.lockPath, err)
	}
	defer lk.Unlock()
	return s.readUnlocked()
}

// With runs fn under an exclusive flock with a mutable document. If fn returns
// nil, the document is stamped with the current schema version and persisted
// atomically. If fn returns an error, the file is left untouched and the error
// is propagated. fn must not retain the document beyond its return.
func (s *Store[T]) With(fn func(T) error) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	lk := flock.New(s.lockPath)
	if err := lk.Lock(); err != nil {
		return fmt.Errorf("acquire lock %s: %w", s.lockPath, err)
	}
	defer lk.Unlock()

	doc, err := s.readUnlocked()
	if err != nil {
		return err
	}
	if err := fn(doc); err != nil {
		return err
	}
	return s.writeAtomic(doc)
}

// readUnlocked reads and parses the file (assumes the caller holds the lock).
// A missing file returns a fresh empty document. The schema-version policy is
// applied to the decoded version.
func (s *Store[T]) readUnlocked() (T, error) {
	var zero T
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.newEmpty(), nil
		}
		return zero, fmt.Errorf("read %s: %w", s.path, err)
	}
	doc := s.newEmpty()
	if err := json.Unmarshal(raw, doc); err != nil {
		return zero, fmt.Errorf("parse %s: %w", s.path, err)
	}
	if err := s.applyVersionPolicy(doc); err != nil {
		return zero, err
	}
	return doc, nil
}

// applyVersionPolicy enforces the shared schema-version rule: coerce 0 ->
// current, accept anything <= current, reject anything > current.
func (s *Store[T]) applyVersionPolicy(doc T) error {
	v := doc.SchemaVersion()
	if v == 0 {
		doc.SetSchemaVersion(s.current)
		return nil
	}
	if v > s.current {
		return fmt.Errorf("%s %s was written by a newer version of nerve (v%d > v%d); upgrade nerve", s.label, s.path, v, s.current)
	}
	return nil
}

// writeAtomic stamps the current schema version and writes doc to path via a
// temp file in the same directory followed by os.Rename.
func (s *Store[T]) writeAtomic(doc T) error {
	doc.SetSchemaVersion(s.current)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o644)
}

func (s *Store[T]) ensureDir() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(s.path), err)
	}
	return nil
}
