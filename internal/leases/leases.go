// Package leases owns the user-wide port leases store at
// $XDG_CONFIG_HOME/nerve/ports.json (default: ~/.config/nerve/ports.json).
//
// Each per-project port registry (internal/registry) decides offsets within its
// own pool, but two projects with overlapping port pools (e.g. project A at base
// 8000 and project B at base 8005) would otherwise be free to register the same
// TCP port. This store records every port held by any active nerve worktree
// across all projects so allocation can reject cross-project collisions even
// when no worktree is currently listening.
//
// Lock ordering: callers that hold both the per-project registry lock AND the
// leases lock MUST acquire the per-project registry first, then the leases
// store. Reversing the order can deadlock concurrent `nerve new` invocations
// on different projects.
package leases

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gofrs/flock"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
)

// CurrentVersion is the schema version written to ports.json.
const CurrentVersion = 1

// Lease records who is holding a port. The map key (a port number) sits one
// level above this struct in the on-disk file.
type Lease struct {
	Project      string    `json:"project,omitempty"`
	ProjectPath  string    `json:"project_path,omitempty"`
	WorktreePath string    `json:"worktree_path"`
	Branch       string    `json:"branch,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// file is the on-disk shape. Port keys are stringified for JSON friendliness.
type file struct {
	Version int              `json:"version"`
	Leases  map[string]Lease `json:"leases"`
}

// ConflictError is returned by Reserve when a requested port is already leased
// to a different worktree path.
type ConflictError struct {
	Port        int
	ByProject   string
	ByWorktree  string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("port %d already leased by project %q worktree %q", e.Port, e.ByProject, e.ByWorktree)
}

// Store is the entry point for leases operations. Methods that mutate run
// under an exclusive flock on a sibling lock file; reads use a shared lock.
type Store struct {
	path     string
	lockPath string
}

// Open returns a Store bound to the user-wide leases path. The file (and parent
// directory) need not exist yet; reads from a missing file return an empty map.
func Open() (*Store, error) {
	p, err := config.LeasesPath()
	if err != nil {
		return nil, err
	}
	lk, err := config.LeasesLockPath()
	if err != nil {
		return nil, err
	}
	return &Store{path: p, lockPath: lk}, nil
}

// Path returns the on-disk path the store reads/writes (mostly for diagnostics).
func (s *Store) Path() string { return s.path }

// LockPath returns the sibling flock path (mostly for diagnostics).
func (s *Store) LockPath() string { return s.lockPath }

// Read returns a snapshot of all current leases keyed by port number. Holds a
// shared lock just long enough to read the file. Suitable as a pre-check before
// the allocator picks an offset; the authoritative race-safe gate is Reserve.
func (s *Store) Read() (map[int]Lease, error) {
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	lk := flock.New(s.lockPath)
	if err := lk.RLock(); err != nil {
		return nil, fmt.Errorf("acquire shared lock %s: %w", s.lockPath, err)
	}
	defer lk.Unlock()
	return s.readUnlocked()
}

// With runs fn under an exclusive flock with a mutable view of the leases map.
// If fn returns nil, the resulting map is persisted atomically. If fn returns
// an error, the file is left untouched and the error is propagated.
//
// fn must not retain the map beyond its return.
func (s *Store) With(fn func(map[int]Lease) error) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	lk := flock.New(s.lockPath)
	if err := lk.Lock(); err != nil {
		return fmt.Errorf("acquire lock %s: %w", s.lockPath, err)
	}
	defer lk.Unlock()
	cur, err := s.readUnlocked()
	if err != nil {
		return err
	}
	if err := fn(cur); err != nil {
		return err
	}
	return writeAtomic(s.path, cur)
}

// Reserve atomically claims the given ports for lease. All ports must either be
// unclaimed or already held by the SAME canonical worktree path (idempotent
// re-run). On conflict, returns a *ConflictError naming the first conflicting
// port; no partial state is written.
//
// canonicalForCmp is the worktree path used to determine "same worktree"; if
// empty, lease.WorktreePath is used. The caller is responsible for passing
// canonicalized (symlink-resolved) paths; callers under the worktree create
// flow already canonicalize before calling Reserve.
func (s *Store) Reserve(ports map[string]int, lease Lease) error {
	target, err := gitutil.CanonicalPath(lease.WorktreePath)
	if err != nil {
		target = lease.WorktreePath
	}
	return s.With(func(cur map[int]Lease) error {
		// Validate first so we don't write a partial reservation.
		for _, p := range ports {
			existing, ok := cur[p]
			if !ok {
				continue
			}
			storedCanon, err := gitutil.CanonicalPath(existing.WorktreePath)
			if err != nil {
				storedCanon = existing.WorktreePath
			}
			if storedCanon != target {
				return &ConflictError{
					Port:       p,
					ByProject:  existing.Project,
					ByWorktree: existing.WorktreePath,
				}
			}
		}
		if lease.CreatedAt.IsZero() {
			lease.CreatedAt = time.Now().UTC()
		}
		// Use the canonical form on disk so future canonical comparisons are stable.
		lease.WorktreePath = target
		for _, p := range ports {
			cur[p] = lease
		}
		return nil
	})
}

// Release drops every lease whose canonical WorktreePath matches the canonical
// form of worktreePath. Returns the released ports in no particular order.
// Errors only on I/O failure; "no entries matched" is not an error.
func (s *Store) Release(worktreePath string) ([]int, error) {
	target, err := gitutil.CanonicalPath(worktreePath)
	if err != nil {
		target = worktreePath
	}
	var released []int
	err = s.With(func(cur map[int]Lease) error {
		for port, l := range cur {
			storedCanon, err := gitutil.CanonicalPath(l.WorktreePath)
			if err != nil {
				storedCanon = l.WorktreePath
			}
			if storedCanon == target {
				released = append(released, port)
				delete(cur, port)
			}
		}
		return nil
	})
	return released, err
}

// Prune removes every lease whose canonical WorktreePath is not present in
// activeWorktrees. Returns the dropped ports. The activeWorktrees slice is
// canonicalized before comparison so callers don't have to.
func (s *Store) Prune(activeWorktrees []string) ([]int, error) {
	alive := make(map[string]bool, len(activeWorktrees))
	for _, w := range activeWorktrees {
		canon, err := gitutil.CanonicalPath(w)
		if err != nil {
			canon = w
		}
		alive[canon] = true
	}
	var dropped []int
	err := s.With(func(cur map[int]Lease) error {
		for port, l := range cur {
			canon, err := gitutil.CanonicalPath(l.WorktreePath)
			if err != nil {
				canon = l.WorktreePath
			}
			if !alive[canon] {
				dropped = append(dropped, port)
				delete(cur, port)
			}
		}
		return nil
	})
	return dropped, err
}

// readUnlocked reads and parses the on-disk file. Returns an empty map (NOT an
// error) when the file is missing — leases are best-effort additive.
func (s *Store) readUnlocked() (map[int]Lease, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[int]Lease{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	out := make(map[int]Lease, len(f.Leases))
	for k, v := range f.Leases {
		p, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		out[p] = v
	}
	return out, nil
}

func (s *Store) ensureDir() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(s.path), err)
	}
	return nil
}

// writeAtomic serializes the leases map and writes to path via temp+rename.
func writeAtomic(path string, m map[int]Lease) error {
	out := file{Version: CurrentVersion, Leases: make(map[string]Lease, len(m))}
	for p, l := range m {
		out.Leases[strconv.Itoa(p)] = l
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Checker is a read-only view of the leases store used by the ports allocator
// to skip offsets whose ports are already leased to a different worktree. A
// Checker is a snapshot — callers should re-snapshot if they need fresh data.
type Checker struct {
	// owners maps port -> canonical worktree path of the current lease holder.
	owners map[int]string
	// self is the canonical worktree path that should be treated as "us" (so an
	// idempotent re-run doesn't report a self-collision). Empty disables self
	// matching.
	self string
}

// IsLeased reports whether port is leased to a DIFFERENT worktree than the
// Checker's self. Returns the holder's canonical worktree path as owner when
// taken=true.
func (c *Checker) IsLeased(port int) (taken bool, owner string) {
	if c == nil || c.owners == nil {
		return false, ""
	}
	holder, ok := c.owners[port]
	if !ok {
		return false, ""
	}
	if c.self != "" && holder == c.self {
		return false, ""
	}
	return true, holder
}

// NewChecker snapshots the current leases for read-only allocator pre-checks.
// selfWorktreePath is the canonical worktree path the caller is allocating for
// (so self-leases don't appear taken). Pass empty to treat every lease as
// belonging to "someone else".
func NewChecker(s *Store, selfWorktreePath string) (*Checker, error) {
	cur, err := s.Read()
	if err != nil {
		return nil, err
	}
	self, _ := gitutil.CanonicalPath(selfWorktreePath)
	owners := make(map[int]string, len(cur))
	for port, l := range cur {
		canon, err := gitutil.CanonicalPath(l.WorktreePath)
		if err != nil {
			canon = l.WorktreePath
		}
		owners[port] = canon
	}
	return &Checker{owners: owners, self: self}, nil
}
