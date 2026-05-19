package ports

import (
	"testing"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/registry"
)

func testConfig() *config.ProjectConfig {
	cfg := config.Defaults()
	cfg.Services = []config.Service{
		{ID: "django", BasePort: 8000, EnvKey: "DJANGO_PORT", Primary: true},
		{ID: "postgres", BasePort: 5432, EnvKey: "POSTGRES_PORT"},
	}
	return &cfg
}

func freshRegistry() *registry.Registry {
	return &registry.Registry{
		Version:     registry.CurrentVersion,
		Allocations: map[string]registry.Allocation{},
	}
}

func TestAllocateAssignsFirstFreeOffset(t *testing.T) {
	cfg := testConfig()
	reg := freshRegistry()
	probe := func(int) bool { return true }
	res, err := Allocate(reg, cfg, "/tmp/wt", "feat", probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Offset != 1 {
		t.Errorf("first allocation should get offset 1, got %d", res.Offset)
	}
	if res.PrimaryPort != 8001 {
		t.Errorf("primary port = %d, want 8001", res.PrimaryPort)
	}
	if res.PortByService["postgres"] != 5433 {
		t.Errorf("postgres port = %d, want 5433", res.PortByService["postgres"])
	}
}

func TestAllocateSkipsTakenOffsets(t *testing.T) {
	cfg := testConfig()
	reg := freshRegistry()
	registry.ConfigurePool(reg, cfg)
	// Pre-claim offset 1.
	if err := reg.Claim(8001, registry.Allocation{WorktreePath: "/tmp/existing", Branch: "x", Offset: 1, PrimaryService: "django"}); err != nil {
		t.Fatal(err)
	}
	probe := func(int) bool { return true }
	res, err := Allocate(reg, cfg, "/tmp/wt2", "feat2", probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Offset != 2 {
		t.Errorf("expected offset 2, got %d", res.Offset)
	}
}

func TestAllocateSkipsBoundPorts(t *testing.T) {
	cfg := testConfig()
	reg := freshRegistry()
	// Probe says ports for offset 1 are busy (postgres squatter); offset 2 is free.
	probe := func(port int) bool {
		return !(port == 5433)
	}
	res, err := Allocate(reg, cfg, "/tmp/wt", "feat", probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Offset != 2 {
		t.Errorf("expected offset 2 (skipping squatter at 5433), got %d", res.Offset)
	}
}

func TestAllocateExhausted(t *testing.T) {
	cfg := testConfig()
	cfg.Project.PoolSize = 2
	reg := freshRegistry()
	for i := 1; i <= 2; i++ {
		if err := reg.Claim(8000+i, registry.Allocation{WorktreePath: "/tmp/x", Branch: "x", Offset: i}); err != nil {
			t.Fatal(err)
		}
	}
	probe := func(int) bool { return true }
	_, err := Allocate(reg, cfg, "/tmp/wt", "feat", probe, nil)
	if err != ErrPoolExhausted {
		t.Errorf("expected ErrPoolExhausted, got %v", err)
	}
}

func TestAllocateNoServices(t *testing.T) {
	cfg := config.Defaults()
	reg := freshRegistry()
	_, err := Allocate(reg, &cfg, "/tmp/wt", "feat", func(int) bool { return true }, nil)
	if err != ErrNoServices {
		t.Errorf("expected ErrNoServices, got %v", err)
	}
}

// fakeChecker reports a fixed set of ports as leased.
type fakeChecker map[int]bool

func (f fakeChecker) IsLeased(port int) (bool, string) { return f[port], "other" }

func TestAllocateSkipsLeasedPorts(t *testing.T) {
	cfg := testConfig()
	reg := freshRegistry()
	// Bind probe says everything is free; only the leases store says 5433 is taken.
	probe := func(int) bool { return true }
	checker := fakeChecker{5433: true}
	res, err := Allocate(reg, cfg, "/tmp/wt", "feat", probe, checker)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if res.Offset != 2 {
		t.Errorf("expected offset 2 (offset 1's postgres=5433 leased to another project), got %d", res.Offset)
	}
}

func TestPortsForOffset(t *testing.T) {
	cfg := testConfig()
	cfg.Project.PortOffset = 20
	got := PortsFor(cfg, 3)
	if got["django"] != 8023 {
		t.Errorf("django = %d, want 8023 (base 8000 + project_offset 20 + offset 3)", got["django"])
	}
	if got["postgres"] != 5455 {
		t.Errorf("postgres = %d, want 5455", got["postgres"])
	}
}
