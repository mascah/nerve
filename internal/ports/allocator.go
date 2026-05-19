// Package ports allocates per-worktree port offsets within a project's pool.
//
// The allocation algorithm is offset arithmetic: for some offset N in [1, pool_size],
// each service's allocated port is service.base_port + project.port_offset + N. This
// preserves predictable URLs ("django for worktree 3 is always 8003"). A short
// socket-bind probe rejects offsets where any service's port is already in use by an
// external squatter.
package ports

import (
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/registry"
)

// ErrPoolExhausted is returned when every offset in the pool is taken (or every
// candidate offset has at least one externally-bound port).
var ErrPoolExhausted = errors.New("port pool exhausted")

// ErrNoServices is returned when allocation is attempted on a config with no services.
// Lightweight projects should not call Allocate at all.
var ErrNoServices = errors.New("project has no services configured")

// ProbeFunc reports whether a port on localhost is currently free (i.e. can be bound).
// Pluggable to keep tests hermetic; production code uses ProbeBind.
type ProbeFunc func(port int) bool

// LeaseChecker is a read-only view of the cross-project leases store. The
// allocator calls IsLeased(port) for every service's candidate port and skips
// the offset if any port is leased to a different worktree. A nil LeaseChecker
// disables cross-project lease checking (the per-project registry and the bind
// probe still apply). The authoritative race-safe gate is the leases store
// Reserve call under the worktree create flow; this pre-check just avoids
// wasted offsets.
type LeaseChecker interface {
	IsLeased(port int) (taken bool, owner string)
}

// Result describes a successful allocation.
type Result struct {
	Offset int
	// PortByService maps service ID to the allocated port for that service.
	PortByService map[string]int
	// PrimaryPort is the registry key — i.e. the primary service's allocated port.
	PrimaryPort int
}

// Allocate finds an unused offset in cfg's pool, probes every service's resulting port
// to ensure no external squatter, and records the allocation in reg. The caller must
// have reg under exclusive lock (via registry.Handle.With).
//
// probe may be nil; in that case ProbeBind is used. checker may be nil; in that
// case cross-project lease checking is skipped.
func Allocate(reg *registry.Registry, cfg *config.ProjectConfig, worktreePath, branch string, probe ProbeFunc, checker LeaseChecker) (*Result, error) {
	primary := cfg.PrimaryService()
	if primary == nil {
		return nil, ErrNoServices
	}
	if probe == nil {
		probe = ProbeBind
	}
	if reg.Pool.Size() == 0 {
		registry.ConfigurePool(reg, cfg)
	}
	taken := registry.AllocatedOffsets(reg)

	for offset := 1; offset <= cfg.Project.PoolSize; offset++ {
		if taken[offset] {
			continue
		}
		// Probe every service before claiming.
		portByService := make(map[string]int, len(cfg.Services))
		allFree := true
		for i := range cfg.Services {
			svc := &cfg.Services[i]
			candidate := svc.BasePort + cfg.Project.PortOffset + offset
			if !probe(candidate) {
				allFree = false
				break
			}
			if checker != nil {
				if leased, _ := checker.IsLeased(candidate); leased {
					allFree = false
					break
				}
			}
			portByService[svc.ID] = candidate
		}
		if !allFree {
			continue
		}
		primaryPort := primary.BasePort + cfg.Project.PortOffset + offset
		alloc := registry.Allocation{
			WorktreePath:   worktreePath,
			Branch:         branch,
			Offset:         offset,
			PrimaryService: primary.ID,
			CreatedByNerve: true,
			CreatedAt:      time.Now().UTC(),
		}
		if err := reg.Claim(primaryPort, alloc); err != nil {
			return nil, err
		}
		return &Result{
			Offset:        offset,
			PortByService: portByService,
			PrimaryPort:   primaryPort,
		}, nil
	}
	return nil, ErrPoolExhausted
}

// PortsFor computes every service's port for a given offset without touching the
// registry. Useful for `nerve env` lookups.
func PortsFor(cfg *config.ProjectConfig, offset int) map[string]int {
	out := make(map[string]int, len(cfg.Services))
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		out[svc.ID] = svc.BasePort + cfg.Project.PortOffset + offset
	}
	return out
}

// ProbeBind opens a brief TCP listener on 127.0.0.1:port to determine whether the
// port is free. Returns true if the listener bound successfully (port was free).
func ProbeBind(port int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}
