package tui

import (
	"sort"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/ports"
	"github.com/mascah/nerve/internal/registry"
)

// portCell is one service's resolved port for a given offset, plus a liveness flag.
type portCell struct {
	ServiceID string
	Port      int
	// Listening is true when something is currently bound to Port (a running dev
	// server or an external squatter) — i.e. the bind probe reported the port NOT free.
	Listening bool
}

// portsRow is one row in the Ports tab: a single offset in the pool, who holds it
// (Branch == "" means nerve has not allocated it), and every service's resolved port
// with its liveness marker.
type portsRow struct {
	Offset int
	// Branch is the worktree that holds this offset per the registry, or "" when free.
	Branch string
	Ports  []portCell
}

// portsProbeFunc reports whether a port is currently FREE/bindable (true) or in use
// (false). It mirrors ports.ProbeFunc so tests can inject a hermetic probe instead of
// touching real sockets. Production builds the rows with ports.ProbeBind.
type portsProbeFunc func(port int) bool

// buildPortsRows builds one row per offset in [1, pool_size], joining the registry's
// offset→branch map with offset-arithmetic port computation and a per-port liveness
// probe. It is pure given (reg, cfg, probe) and does no I/O of its own beyond calling
// probe — the probe is the only injection point that touches the network.
//
// Returns nil when cfg has no primary service (lightweight project) — the caller
// renders a placeholder rather than an empty grid, and never probes.
func buildPortsRows(reg *registry.Registry, cfg *config.ProjectConfig, probe portsProbeFunc) []portsRow {
	if cfg == nil || cfg.PrimaryService() == nil {
		return nil
	}
	if probe == nil {
		probe = func(port int) bool { return ports.ProbeBind(port) }
	}

	// Map offset → branch from the registry allocations. A missing/empty registry
	// simply yields all-free rows.
	branchByOffset := map[int]string{}
	if reg != nil {
		for _, a := range reg.Allocations {
			branchByOffset[a.Offset] = a.Branch
		}
	}

	rows := make([]portsRow, 0, cfg.Project.PoolSize)
	for offset := 1; offset <= cfg.Project.PoolSize; offset++ {
		portByID := ports.PortsFor(cfg, offset)
		cells := make([]portCell, 0, len(cfg.Services))
		for i := range cfg.Services {
			svc := &cfg.Services[i]
			port := portByID[svc.ID]
			cells = append(cells, portCell{
				ServiceID: svc.ID,
				Port:      port,
				// probe reports free; Listening is the inverse.
				Listening: !probe(port),
			})
		}
		rows = append(rows, portsRow{
			Offset: offset,
			Branch: branchByOffset[offset],
			Ports:  cells,
		})
	}
	return rows
}

// loadPortsRows reads the per-project registry and builds the Ports-tab rows with a live
// bind probe. It is the synchronous body invoked from a tea.Cmd goroutine (never directly
// from Update/View) — it reads the flock'd registry and probes pool_size×services ports.
//
// cfg may be nil / have no services (lightweight project); in that case it returns nil
// without touching the registry or probing, and the caller renders a placeholder.
func loadPortsRows(repoRoot string, cfg *config.ProjectConfig) ([]portsRow, error) {
	if cfg == nil || cfg.PrimaryService() == nil {
		return nil, nil
	}
	// Registry read is best-effort: a missing/corrupt registry yields all-free rows
	// rather than an error, since the offset arithmetic + live probe are still useful.
	var reg *registry.Registry
	if r, err := registry.Open(repoRoot).Read(); err == nil {
		reg = r
	}
	return buildPortsRows(reg, cfg, nil), nil
}

// serviceIDsInOrder returns the service IDs in config order — used to render the Ports
// grid header so the per-row cells line up under the right column.
func serviceIDsInOrder(cfg *config.ProjectConfig) []string {
	if cfg == nil {
		return nil
	}
	ids := make([]string, 0, len(cfg.Services))
	for i := range cfg.Services {
		ids = append(ids, cfg.Services[i].ID)
	}
	return ids
}

// sortedBranchOffsets is a small helper for tests/debugging: the offsets that are
// allocated, sorted ascending. Not used in rendering but handy for assertions.
func sortedBranchOffsets(rows []portsRow) []int {
	var out []int
	for _, r := range rows {
		if r.Branch != "" {
			out = append(out, r.Offset)
		}
	}
	sort.Ints(out)
	return out
}
