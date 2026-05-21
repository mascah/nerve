package registry

import (
	"fmt"
	"strconv"
	"time"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/jsonstore"
)

// Open returns a Handle bound to the per-project port registry. The handle does NOT
// acquire the lock; callers use h.With() to run mutating logic under flock.
func Open(repoRoot string) *Handle {
	store := jsonstore.New(jsonstore.Config[*Registry]{
		Path:     config.PortRegistryPath(repoRoot),
		LockPath: config.PortLockPath(repoRoot),
		Current:  CurrentVersion,
		Label:    "port registry",
		NewEmpty: newEmpty,
	})
	return &Handle{repoRoot: repoRoot, store: store}
}

// Handle is the entry point for all registry operations. Methods on Handle that read
// without writing do their own lightweight locking; mutating logic should go through With.
type Handle struct {
	repoRoot string
	store    *jsonstore.Store[*Registry]
}

// Read returns the current registry contents without holding the lock for any duration
// beyond the call. Suitable for `nerve list`, `nerve ports list`, etc.
func (h *Handle) Read() (*Registry, error) {
	reg, err := h.store.Read()
	if err != nil {
		return nil, err
	}
	normalize(reg)
	return reg, nil
}

// With runs fn under an exclusive flock. fn receives a mutable Registry pointer; if
// it returns nil, the registry is persisted atomically. If fn returns an error, the
// registry is left untouched and the error is propagated. fn must not retain the
// pointer beyond its return.
func (h *Handle) With(fn func(*Registry) error) error {
	return h.store.With(func(reg *Registry) error {
		normalize(reg)
		return fn(reg)
	})
}

// FindByWorktreePath looks up an allocation by absolute worktree path. Returns the
// port (registry key) and the allocation. Returns ("", false) if not found.
//
// Both the lookup path and each stored allocation path are canonicalized
// (symlink-resolved) before comparison so callers can pass either the
// symlink-resolved or unresolved form. This matters on macOS where /tmp is a
// symlink to /private/tmp and `git rev-parse --show-toplevel` returns the
// resolved form while older registry entries may not be.
func (h *Handle) FindByWorktreePath(path string) (string, Allocation, bool, error) {
	reg, err := h.Read()
	if err != nil {
		return "", Allocation{}, false, err
	}
	target, err := gitutil.CanonicalPath(path)
	if err != nil {
		return "", Allocation{}, false, err
	}
	for port, a := range reg.Allocations {
		stored, err := gitutil.CanonicalPath(a.WorktreePath)
		if err != nil {
			continue
		}
		if stored == target {
			return port, a, true, nil
		}
	}
	return "", Allocation{}, false, nil
}

// normalize ensures the registry's allocation map is non-nil so callbacks and
// Claim can write into it (a JSON document with a missing or null "allocations"
// would otherwise leave it nil).
func normalize(reg *Registry) {
	if reg.Allocations == nil {
		reg.Allocations = make(map[string]Allocation)
	}
}

func newEmpty() *Registry {
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
		canon, err := gitutil.CanonicalPath(wt.Path)
		if err == nil {
			alive[canon] = true
		}
	}
	dropped := 0
	for port, a := range reg.Allocations {
		canon, err := gitutil.CanonicalPath(a.WorktreePath)
		if err != nil {
			canon = a.WorktreePath
		}
		if !alive[canon] {
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
	if r.Allocations == nil {
		r.Allocations = make(map[string]Allocation)
	}
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
//
// Comparison is canonical (symlink-resolved) on both sides so the call works
// whether the registry holds the resolved form or not.
func (r *Registry) ReleaseByWorktreePath(worktreePath string) (string, bool) {
	target, err := gitutil.CanonicalPath(worktreePath)
	if err != nil {
		return "", false
	}
	for port, a := range r.Allocations {
		stored, err := gitutil.CanonicalPath(a.WorktreePath)
		if err != nil {
			continue
		}
		if stored == target {
			delete(r.Allocations, port)
			return port, true
		}
	}
	return "", false
}
