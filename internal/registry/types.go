// Package registry owns the per-project port allocation registry stored at
// <repo>/.nerve/ports.json. All mutating access is mediated by a flock on the
// sibling ports.json.lock so concurrent `nerve new` calls are safe.
package registry

import "time"

// Registry is the persisted state of which ports are allocated to which worktrees.
type Registry struct {
	Version     int                   `json:"version"`
	Project     string                `json:"project,omitempty"`
	Pool        Pool                  `json:"pool"`
	Allocations map[string]Allocation `json:"allocations"`
}

// SchemaVersion implements jsonstore.Document.
func (r *Registry) SchemaVersion() int { return r.Version }

// SetSchemaVersion implements jsonstore.Document.
func (r *Registry) SetSchemaVersion(v int) { r.Version = v }

// Pool is the half-open port range [Start, End). Pool.End - Pool.Start is the pool size.
type Pool struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Allocation describes one currently-claimed port and the worktree using it.
// The map key in Registry.Allocations is the primary-service port as a string
// (e.g. "8003"); Offset is what was added to base_port to get it.
type Allocation struct {
	WorktreePath   string    `json:"worktree_path"`
	Branch         string    `json:"branch"`
	Offset         int       `json:"offset"`
	PrimaryService string    `json:"primary_service,omitempty"`
	CreatedByNerve bool      `json:"created_by_nerve"`
	CreatedAt      time.Time `json:"created_at"`
}

// CurrentVersion is the schema version nerve writes.
const CurrentVersion = 2

// Size returns the (half-open) pool capacity.
func (p Pool) Size() int {
	if p.End <= p.Start {
		return 0
	}
	return p.End - p.Start
}
