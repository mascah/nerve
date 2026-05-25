package tui

import (
	"testing"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/registry"
)

// portsTestCfg builds a two-service config with a known port_offset and pool_size so the
// offset arithmetic (base_port + port_offset + offset) is easy to assert by hand.
func portsTestCfg() *config.ProjectConfig {
	return &config.ProjectConfig{
		Project: config.ProjectSettings{
			PortOffset: 0,
			PoolSize:   3,
		},
		Services: []config.Service{
			{ID: "django", BasePort: 8000, EnvKey: "DJANGO_PORT", Primary: true},
			{ID: "vite", BasePort: 5170, EnvKey: "VITE_PORT"},
		},
	}
}

// fakeProbe returns a probe func reporting free=true for every port EXCEPT those in the
// inUse set, which it reports as in-use (free=false). Mirrors the ports.ProbeFunc
// hermetic-injection convention so the test never touches a real socket.
func fakeProbe(inUse map[int]bool) portsProbeFunc {
	return func(port int) bool { return !inUse[port] }
}

func TestBuildPortsRows_OffsetArithmeticAndBranchMapping(t *testing.T) {
	cfg := portsTestCfg()
	// Offset 2's primary port is 8000+0+2 = 8002.
	reg := &registry.Registry{
		Allocations: map[string]registry.Allocation{
			"8002": {Offset: 2, Branch: "feature-x", PrimaryService: "django"},
		},
	}

	rows := buildPortsRows(reg, cfg, fakeProbe(nil))
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (pool_size=3), got %d", len(rows))
	}

	// Rows are emitted in offset order 1..pool_size.
	for i, want := range []int{1, 2, 3} {
		if rows[i].Offset != want {
			t.Errorf("row %d: offset = %d, want %d", i, rows[i].Offset, want)
		}
	}

	// Offset 1 and 3 are free; offset 2 is held by feature-x.
	if rows[0].Branch != "" {
		t.Errorf("offset 1 should be free, got branch %q", rows[0].Branch)
	}
	if rows[1].Branch != "feature-x" {
		t.Errorf("offset 2 branch = %q, want feature-x", rows[1].Branch)
	}
	if rows[2].Branch != "" {
		t.Errorf("offset 3 should be free, got branch %q", rows[2].Branch)
	}

	// Per-service ports must follow base_port + port_offset + offset.
	// Offset 1: django 8001, vite 5171. Offset 3: django 8003, vite 5173.
	wantPorts := map[int]map[string]int{
		1: {"django": 8001, "vite": 5171},
		2: {"django": 8002, "vite": 5172},
		3: {"django": 8003, "vite": 5173},
	}
	for _, row := range rows {
		want := wantPorts[row.Offset]
		if len(row.Ports) != 2 {
			t.Fatalf("offset %d: expected 2 port cells, got %d", row.Offset, len(row.Ports))
		}
		for _, cell := range row.Ports {
			if cell.Port != want[cell.ServiceID] {
				t.Errorf("offset %d service %q: port = %d, want %d",
					row.Offset, cell.ServiceID, cell.Port, want[cell.ServiceID])
			}
		}
	}
}

func TestBuildPortsRows_ListeningFlagWiring(t *testing.T) {
	cfg := portsTestCfg()
	reg := &registry.Registry{Allocations: map[string]registry.Allocation{}}

	// Mark django on offset 1 (8001) and vite on offset 3 (5173) as in-use.
	inUse := map[int]bool{8001: true, 5173: true}
	rows := buildPortsRows(reg, cfg, fakeProbe(inUse))

	check := func(offset int, svc string, wantListening bool) {
		t.Helper()
		for _, row := range rows {
			if row.Offset != offset {
				continue
			}
			for _, cell := range row.Ports {
				if cell.ServiceID == svc {
					if cell.Listening != wantListening {
						t.Errorf("offset %d service %q: Listening = %v, want %v",
							offset, svc, cell.Listening, wantListening)
					}
					return
				}
			}
		}
		t.Fatalf("offset %d service %q not found", offset, svc)
	}

	check(1, "django", true)  // 8001 in use
	check(1, "vite", false)   // 5171 free
	check(3, "vite", true)    // 5173 in use
	check(3, "django", false) // 8003 free
}

func TestBuildPortsRows_LightweightReturnsNil(t *testing.T) {
	// No services → lightweight project. Must return nil and never call the probe.
	cfg := &config.ProjectConfig{Project: config.ProjectSettings{PoolSize: 5}}
	probed := false
	probe := func(int) bool { probed = true; return true }

	rows := buildPortsRows(nil, cfg, probe)
	if rows != nil {
		t.Errorf("expected nil rows for lightweight project, got %d rows", len(rows))
	}
	if probed {
		t.Error("probe must not be called for a lightweight project")
	}

	// Also nil cfg.
	if rows := buildPortsRows(nil, nil, probe); rows != nil {
		t.Errorf("expected nil rows for nil cfg, got %d rows", len(rows))
	}
}

func TestBuildPortsRows_NilRegistryAllFree(t *testing.T) {
	cfg := portsTestCfg()
	rows := buildPortsRows(nil, cfg, fakeProbe(nil))
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	for _, row := range rows {
		if row.Branch != "" {
			t.Errorf("offset %d should be free with a nil registry, got %q", row.Offset, row.Branch)
		}
	}
}

func TestSortedBranchOffsets(t *testing.T) {
	cfg := portsTestCfg()
	reg := &registry.Registry{
		Allocations: map[string]registry.Allocation{
			"8003": {Offset: 3, Branch: "c"},
			"8001": {Offset: 1, Branch: "a"},
		},
	}
	rows := buildPortsRows(reg, cfg, fakeProbe(nil))
	got := sortedBranchOffsets(rows)
	want := []int{1, 3}
	if len(got) != len(want) {
		t.Fatalf("sortedBranchOffsets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedBranchOffsets = %v, want %v", got, want)
		}
	}
}
