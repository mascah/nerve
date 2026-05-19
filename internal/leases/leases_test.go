package leases

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

// sandboxStore creates an isolated Store under a temp XDG_CONFIG_HOME so tests
// never touch the real ~/.config/nerve/ports.json.
func sandboxStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir) // belt-and-suspenders for the home-dir fallback
	s, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestReserveReadReleaseRoundtrip(t *testing.T) {
	s := sandboxStore(t)
	wt := filepath.Join(t.TempDir(), "wt-a")
	ports := map[string]int{"django": 8001, "postgres": 5433}
	if err := s.Reserve(ports, Lease{Project: "demo", WorktreePath: wt, Branch: "feat"}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	got, err := s.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 leases, got %d", len(got))
	}
	if got[8001].Branch != "feat" {
		t.Errorf("lease[8001].Branch = %q, want feat", got[8001].Branch)
	}

	released, err := s.Release(wt)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if len(released) != 2 {
		t.Fatalf("expected 2 released ports, got %d (%v)", len(released), released)
	}
	got, _ = s.Read()
	if len(got) != 0 {
		t.Fatalf("expected empty after Release, got %d entries", len(got))
	}
}

func TestReserveConflictsOnDifferentWorktree(t *testing.T) {
	s := sandboxStore(t)
	wtA := filepath.Join(t.TempDir(), "wt-a")
	wtB := filepath.Join(t.TempDir(), "wt-b")
	if err := s.Reserve(map[string]int{"django": 8001}, Lease{Project: "alpha", WorktreePath: wtA}); err != nil {
		t.Fatalf("Reserve A: %v", err)
	}
	err := s.Reserve(map[string]int{"django": 8001}, Lease{Project: "beta", WorktreePath: wtB})
	if err == nil {
		t.Fatalf("expected conflict, got nil")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConflictError, got %T (%v)", err, err)
	}
	if ce.Port != 8001 || ce.ByProject != "alpha" {
		t.Errorf("ConflictError = %+v, want port=8001 byProject=alpha", ce)
	}
}

func TestReserveIdempotentSameWorktree(t *testing.T) {
	s := sandboxStore(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := s.Reserve(map[string]int{"django": 8001, "pg": 5433}, Lease{Project: "demo", WorktreePath: wt}); err != nil {
		t.Fatalf("Reserve initial: %v", err)
	}
	// Re-reserving the same ports for the same worktree must succeed.
	if err := s.Reserve(map[string]int{"django": 8001, "pg": 5433}, Lease{Project: "demo", WorktreePath: wt, Branch: "x"}); err != nil {
		t.Fatalf("Reserve idempotent: %v", err)
	}
	got, _ := s.Read()
	if len(got) != 2 {
		t.Fatalf("expected 2 leases after idempotent re-reserve, got %d", len(got))
	}
	if got[8001].Branch != "x" {
		t.Errorf("expected re-reserve to update branch, got %q", got[8001].Branch)
	}
}

func TestPruneDropsOrphans(t *testing.T) {
	s := sandboxStore(t)
	wtAlive := filepath.Join(t.TempDir(), "alive")
	wtDead := filepath.Join(t.TempDir(), "dead")
	if err := s.Reserve(map[string]int{"a": 8001}, Lease{Project: "p", WorktreePath: wtAlive}); err != nil {
		t.Fatalf("Reserve alive: %v", err)
	}
	if err := s.Reserve(map[string]int{"b": 8002}, Lease{Project: "p", WorktreePath: wtDead}); err != nil {
		t.Fatalf("Reserve dead: %v", err)
	}

	dropped, err := s.Prune([]string{wtAlive})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(dropped) != 1 || dropped[0] != 8002 {
		t.Fatalf("expected to drop port 8002, got %v", dropped)
	}
	got, _ := s.Read()
	if _, ok := got[8001]; !ok {
		t.Errorf("alive entry was pruned")
	}
	if _, ok := got[8002]; ok {
		t.Errorf("dead entry survived")
	}
}

func TestConcurrentReserveExactlyOneWins(t *testing.T) {
	s := sandboxStore(t)
	wtA := filepath.Join(t.TempDir(), "wt-a")
	wtB := filepath.Join(t.TempDir(), "wt-b")
	port := 8001

	// Coordinate two goroutines so they both try to reserve the same port.
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		results <- s.Reserve(map[string]int{"x": port}, Lease{Project: "alpha", WorktreePath: wtA})
	}()
	go func() {
		defer wg.Done()
		<-start
		results <- s.Reserve(map[string]int{"x": port}, Lease{Project: "beta", WorktreePath: wtB})
	}()
	close(start)
	wg.Wait()
	close(results)

	wins, losses := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else {
			losses++
			var ce *ConflictError
			if !errors.As(err, &ce) {
				t.Errorf("loser got unexpected error type: %T (%v)", err, err)
			}
		}
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("expected exactly 1 winner and 1 loser, got wins=%d losses=%d", wins, losses)
	}
	got, _ := s.Read()
	if len(got) != 1 {
		t.Fatalf("expected exactly one lease after race, got %d", len(got))
	}
}

func TestCheckerIgnoresSelf(t *testing.T) {
	s := sandboxStore(t)
	wt := filepath.Join(t.TempDir(), "wt-self")
	if err := s.Reserve(map[string]int{"x": 8001}, Lease{Project: "p", WorktreePath: wt}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// A Checker scoped to a DIFFERENT worktree sees the lease as taken.
	otherWt := filepath.Join(t.TempDir(), "wt-other")
	chk, err := NewChecker(s, otherWt)
	if err != nil {
		t.Fatalf("NewChecker(other): %v", err)
	}
	taken, _ := chk.IsLeased(8001)
	if !taken {
		t.Errorf("expected port 8001 to be reported leased for unrelated worktree")
	}

	// A Checker scoped to the same worktree does NOT see its own lease as taken
	// (so idempotent re-allocation isn't artificially blocked).
	self, err := NewChecker(s, wt)
	if err != nil {
		t.Fatalf("NewChecker(self): %v", err)
	}
	taken, _ = self.IsLeased(8001)
	if taken {
		t.Errorf("Checker reported self-lease as taken")
	}
}
