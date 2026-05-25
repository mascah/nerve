package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
)

// newUnregisteredRepo creates a one-commit git repo under a throwaway XDG home but
// does NOT register it — so `nerve init` can exercise auto-registration. Returns the
// canonical repo path (the form gitutil.Discover reports).
func newUnregisteredRepo(t *testing.T) string {
	t.Helper()
	setXDGHome(t) // isolates ~/.config/nerve/projects.yaml
	repo := t.TempDir()
	gitRunCLI(t, repo, "init", "-q", "-b", "main")
	gitRunCLI(t, repo, "config", "user.email", "test@example.com")
	gitRunCLI(t, repo, "config", "user.name", "Test")
	gitRunCLI(t, repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunCLI(t, repo, "add", "seed.txt")
	gitRunCLI(t, repo, "commit", "-q", "-m", "seed")

	canon, err := gitutil.CanonicalPath(repo)
	if err != nil {
		t.Fatalf("canonicalize repo: %v", err)
	}
	return canon
}

// TestInit_ScaffoldsAndRegisters is the headline check: `nerve init` in an
// unregistered repo writes the scaffold config AND registers the project.
func TestInit_ScaffoldsAndRegisters(t *testing.T) {
	repo := newUnregisteredRepo(t)
	t.Chdir(repo)

	out, _, err := execCmd(t, NewRootCmd(), "init")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(out, "wrote") {
		t.Errorf("expected 'wrote' in output, got: %q", out)
	}
	if !strings.Contains(out, "registered as") {
		t.Errorf("expected 'registered as' in output, got: %q", out)
	}

	// Config on disk is the verbatim scaffold.
	cfgPath := config.ProjectConfigPath(repo)
	got, rerr := os.ReadFile(cfgPath)
	if rerr != nil {
		t.Fatalf("read config: %v", rerr)
	}
	if string(got) != string(config.Scaffold()) {
		t.Errorf("written config is not the verbatim scaffold")
	}

	// Project is registered under the repo's dir name.
	reg, lerr := config.LoadGlobalRegistry()
	if lerr != nil {
		t.Fatalf("load registry: %v", lerr)
	}
	if len(reg.Projects) != 1 {
		t.Fatalf("expected exactly 1 registered project, got %d: %+v", len(reg.Projects), reg.Projects)
	}
	if reg.Projects[0].Name != filepath.Base(repo) {
		t.Errorf("registered name = %q, want %q", reg.Projects[0].Name, filepath.Base(repo))
	}
	if entry := reg.FindProjectByPath(repo); entry == nil {
		t.Errorf("repo path not found in registry: %s", repo)
	}
}

// TestInit_ForceIsIdempotentForRegistration re-runs init --force and confirms the
// registry still has exactly one entry (no duplicate, no error).
func TestInit_ForceIsIdempotentForRegistration(t *testing.T) {
	repo := newUnregisteredRepo(t)
	t.Chdir(repo)

	if _, _, err := execCmd(t, NewRootCmd(), "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}
	out, _, err := execCmd(t, NewRootCmd(), "init", "--force")
	if err != nil {
		t.Fatalf("init --force: %v", err)
	}
	if !strings.Contains(out, "already registered") {
		t.Errorf("expected 'already registered' on re-init, got: %q", out)
	}

	reg, lerr := config.LoadGlobalRegistry()
	if lerr != nil {
		t.Fatalf("load registry: %v", lerr)
	}
	if len(reg.Projects) != 1 {
		t.Fatalf("expected exactly 1 registered project after re-init, got %d: %+v", len(reg.Projects), reg.Projects)
	}
}

// TestInit_NoRegisterSkipsRegistration confirms --no-register writes the config but
// leaves the registry empty and prints the old `nerve project add` hint.
func TestInit_NoRegisterSkipsRegistration(t *testing.T) {
	repo := newUnregisteredRepo(t)
	t.Chdir(repo)

	out, _, err := execCmd(t, NewRootCmd(), "init", "--no-register")
	if err != nil {
		t.Fatalf("init --no-register: %v", err)
	}
	if !strings.Contains(out, "nerve project add") {
		t.Errorf("expected 'nerve project add' hint with --no-register, got: %q", out)
	}

	if _, statErr := os.Stat(config.ProjectConfigPath(repo)); statErr != nil {
		t.Fatalf("config should still be written with --no-register: %v", statErr)
	}
	reg, lerr := config.LoadGlobalRegistry()
	if lerr != nil {
		t.Fatalf("load registry: %v", lerr)
	}
	if len(reg.Projects) != 0 {
		t.Errorf("expected empty registry with --no-register, got %d: %+v", len(reg.Projects), reg.Projects)
	}
}

// TestInit_NameCollisionWarnsButSucceeds confirms that when a DIFFERENT path already
// owns the chosen name, init still writes the scaffold and succeeds (exit 0) with a
// clear warning rather than failing.
func TestInit_NameCollisionWarnsButSucceeds(t *testing.T) {
	repo := newUnregisteredRepo(t) // sets up the throwaway XDG home
	t.Chdir(repo)

	// Pre-register a DIFFERENT path under the name init would auto-pick.
	name := filepath.Base(repo)
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if err := reg.AddProject(config.ProjectEntry{Name: name, Path: filepath.Join(t.TempDir(), "elsewhere")}); err != nil {
		t.Fatalf("seed colliding project: %v", err)
	}
	if err := config.SaveGlobalRegistry(reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	out, _, err := execCmd(t, NewRootCmd(), "init")
	if err != nil {
		t.Fatalf("init with name collision should succeed, got: %v", err)
	}
	if !strings.Contains(out, "auto-registration skipped") {
		t.Errorf("expected 'auto-registration skipped' warning, got: %q", out)
	}
	if _, statErr := os.Stat(config.ProjectConfigPath(repo)); statErr != nil {
		t.Fatalf("scaffold should still be written on name collision: %v", statErr)
	}

	// The colliding entry's path is unchanged (we didn't overwrite it).
	reg2, lerr := config.LoadGlobalRegistry()
	if lerr != nil {
		t.Fatalf("reload registry: %v", lerr)
	}
	if entry := reg2.FindProjectByPath(repo); entry != nil {
		t.Errorf("repo should NOT be registered after name collision, but found: %+v", entry)
	}
}
