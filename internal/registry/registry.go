package registry

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

// Open returns a Handle bound to the per-project port registry. The handle does NOT
// acquire the lock; callers use h.With() to run mutating logic under flock.
func Open(repoRoot string) *Handle {
	return &Handle{
		repoRoot: repoRoot,
		path:     config.PortRegistryPath(repoRoot),
		lockPath: config.PortLockPath(repoRoot),
	}
}

// Handle is the entry point for all registry operations. Methods on Handle that read
// without writing do their own lightweight locking; mutating logic should go through With.
type Handle struct {
	repoRoot string
	path     string
	lockPath string
}

// Read returns the current registry contents without holding the lock for any duration
// beyond the call. Suitable for `nerve list`, `nerve ports list`, etc.
func (h *Handle) Read() (*Registry, error) {
	lk := flock.New(h.lockPath)
	if err := acquireShared(lk); err != nil {
		return nil, err
	}
	defer lk.Unlock()
	return h.readUnlocked()
}

// With runs fn under an exclusive flock. fn receives a mutable Registry pointer; if
// it returns nil, the registry is persisted atomically. If fn returns an error, the
// registry is left untouched and the error is propagated. fn must not retain the
// pointer beyond its return.
func (h *Handle) With(fn func(*Registry) error) error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(h.path), err)
	}
	lk := flock.New(h.lockPath)
	if err := lk.Lock(); err != nil {
		return fmt.Errorf("acquire lock %s: %w", h.lockPath, err)
	}
	defer lk.Unlock()

	reg, err := h.readUnlocked()
	if err != nil {
		return err
	}
	if err := fn(reg); err != nil {
		return err
	}
	return writeAtomic(h.path, reg)
}

// FindByWorktreePath looks up an allocation by absolute worktree path. Returns the
// port (registry key) and the allocation. Returns ("", false) if not found.
func (h *Handle) FindByWorktreePath(path string) (string, Allocation, bool, error) {
	reg, err := h.Read()
	if err != nil {
		return "", Allocation{}, false, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", Allocation{}, false, err
	}
	for port, a := range reg.Allocations {
		if a.WorktreePath == abs {
			return port, a, true, nil
		}
	}
	return "", Allocation{}, false, nil
}

func (h *Handle) readUnlocked() (*Registry, error) {
	raw, err := os.ReadFile(h.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newEmpty(h.repoRoot), nil
		}
		return nil, fmt.Errorf("read %s: %w", h.path, err)
	}
	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", h.path, err)
	}
	if reg.Allocations == nil {
		reg.Allocations = make(map[string]Allocation)
	}
	if reg.Version == 0 {
		reg.Version = CurrentVersion
	}
	return &reg, nil
}

func newEmpty(repoRoot string) *Registry {
	_ = repoRoot
	return &Registry{
		Version:     CurrentVersion,
		Allocations: make(map[string]Allocation),
	}
}

// ConfigurePool sets the registry pool from a ProjectConfig. Idempotent; safe to call
// each time a registry is initialized.
func ConfigurePool(reg *Registry, cfg *config.ProjectConfig) {
	primary := cfg.PrimaryService()
	if primary == nil {
		// Lightweight project — no services, no pool. Leave Pool zero so allocator no-ops.
		return
	}
	start := primary.BasePort + cfg.Project.PortOffset + 1
	reg.Pool = Pool{Start: start, End: start + cfg.Project.PoolSize}
}

// CleanStale removes allocations whose WorktreePath is no longer a valid git worktree.
// The set of registered worktree paths for repoRoot is consulted first to avoid shelling
// out per allocation. Returns the number of entries dropped.
func CleanStale(reg *Registry, repoRoot string) (int, error) {
	if len(reg.Allocations) == 0 {
		return 0, nil
	}
	worktrees, err := gitutil.ListWorktrees(repoRoot)
	if err != nil {
		return 0, fmt.Errorf("list worktrees: %w", err)
	}
	alive := make(map[string]bool, len(worktrees))
	for _, wt := range worktrees {
		abs, err := filepath.Abs(wt.Path)
		if err == nil {
			alive[abs] = true
		}
	}
	dropped := 0
	for port, a := range reg.Allocations {
		if !alive[a.WorktreePath] {
			delete(reg.Allocations, port)
			dropped++
		}
	}
	return dropped, nil
}

// AllocatedOffsets returns the set of currently-claimed offsets, derived from
// allocation entries' Offset field.
func AllocatedOffsets(reg *Registry) map[int]bool {
	out := make(map[int]bool, len(reg.Allocations))
	for _, a := range reg.Allocations {
		out[a.Offset] = true
	}
	return out
}

// Claim inserts a new allocation. Returns an error if the port key already exists.
func (r *Registry) Claim(port int, a Allocation) error {
	key := strconv.Itoa(port)
	if _, ok := r.Allocations[key]; ok {
		return fmt.Errorf("port %d already allocated", port)
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	r.Allocations[key] = a
	return nil
}

// ReleaseByWorktreePath drops the allocation associated with worktreePath. Returns the
// dropped port (registry key) and true, or "" and false if not found.
func (r *Registry) ReleaseByWorktreePath(worktreePath string) (string, bool) {
	abs, err := filepath.Abs(worktreePath)
	if err != nil {
		return "", false
	}
	for port, a := range r.Allocations {
		if a.WorktreePath == abs {
			delete(r.Allocations, port)
			return port, true
		}
	}
	return "", false
}

// writeAtomic marshals reg as pretty JSON and writes to path via temp+rename.
func writeAtomic(path string, reg *Registry) error {
	data, err := json.MarshalIndent(reg, "", "  ")
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

func acquireShared(lk *flock.Flock) error {
	if err := lk.RLock(); err != nil {
		return fmt.Errorf("acquire shared lock: %w", err)
	}
	return nil
}
