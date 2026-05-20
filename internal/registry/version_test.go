package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/config"
)

// seedRegistryFile writes raw JSON to <repoRoot>/.nerve/ports.json.
func seedRegistryFile(t *testing.T, repoRoot, content string) {
	t.Helper()
	path := config.PortRegistryPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestVersionZeroCoercedToCurrent(t *testing.T) {
	repoRoot := t.TempDir()
	seedRegistryFile(t, repoRoot, `{"version":0,"allocations":{}}`)
	reg, err := Open(repoRoot).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if reg.Version != CurrentVersion {
		t.Errorf("version 0 should coerce to %d, got %d", CurrentVersion, reg.Version)
	}
	if reg.Allocations == nil {
		t.Errorf("expected non-nil Allocations after read")
	}
}

func TestOlderVersionAccepted(t *testing.T) {
	repoRoot := t.TempDir()
	// CurrentVersion is 2; v1 is an older, forward-compatible registry.
	seedRegistryFile(t, repoRoot, `{"version":1,"allocations":{}}`)
	reg, err := Open(repoRoot).Read()
	if err != nil {
		t.Fatalf("older version should be accepted, got: %v", err)
	}
	if reg.Version != 1 {
		t.Errorf("expected stored version 1 preserved on read, got %d", reg.Version)
	}
}

func TestFutureVersionRejected(t *testing.T) {
	repoRoot := t.TempDir()
	future := CurrentVersion + 1
	seedRegistryFile(t, repoRoot, `{"version":99,"allocations":{}}`)
	_, err := Open(repoRoot).Read()
	if err == nil {
		t.Fatalf("expected rejection of future registry version")
	}
	msg := err.Error()
	for _, want := range []string{"newer version of nerve", "upgrade nerve"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
	_ = future
}
