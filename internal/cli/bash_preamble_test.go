package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/registry"
)

// runBashPreambleCmd executes `nerve bash-preamble` with the given stdin JSON and
// returns its stdout. Mirrors execCmd but injects stdin (the command reads the
// PreToolUse payload from cmd.InOrStdin()).
func runBashPreambleCmd(t *testing.T, stdin string) string {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs([]string{"bash-preamble"})
	if err := root.Execute(); err != nil {
		t.Fatalf("bash-preamble returned error: %v", err)
	}
	return out.String()
}

// setupPreambleRepo builds a temp git repo with one service, a worktree under
// .worktrees/feat, and a seeded registry allocation (offset 1 → DJANGO_PORT 8001).
// Returns (repoPath, worktreePath).
func setupPreambleRepo(t *testing.T, cfg *config.ProjectConfig) (string, string) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("relies on git on PATH + POSIX paths")
	}
	repoPath := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repoPath
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repoPath, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "seed")

	if err := config.SaveProjectConfig(repoPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	worktreePath := filepath.Join(repoPath, ".worktrees", "feat")
	git("worktree", "add", "-b", "feat", worktreePath)

	h := registry.Open(repoPath)
	if err := h.With(func(reg *registry.Registry) error {
		registry.ConfigurePool(reg, cfg)
		return reg.Claim(8001, registry.Allocation{
			WorktreePath:   worktreePath,
			Branch:         "feat",
			Offset:         1,
			PrimaryService: "django",
			CreatedByNerve: true,
		})
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	return repoPath, worktreePath
}

func djangoConfig() *config.ProjectConfig {
	return &config.ProjectConfig{
		Version:  config.CurrentConfigVersion,
		Project:  config.ProjectSettings{PortOffset: 0, PoolSize: 10},
		Services: []config.Service{{ID: "django", BasePort: 8000, EnvKey: "DJANGO_PORT", Primary: true}},
	}
}

// decodeUpdatedCommand pulls hookSpecificOutput.updatedInput.command out of the
// command's JSON stdout, or "" if stdout was empty (the no-op case).
func decodeUpdatedCommand(t *testing.T, stdout string) string {
	t.Helper()
	if strings.TrimSpace(stdout) == "" {
		return ""
	}
	var doc struct {
		HookSpecificOutput struct {
			UpdatedInput struct {
				Command string `json:"command"`
			} `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode output JSON %q: %v", stdout, err)
	}
	return doc.HookSpecificOutput.UpdatedInput.Command
}

func TestBashPreamble_DefaultExportsPortVars(t *testing.T) {
	setXDGHome(t)
	_, worktreePath := setupPreambleRepo(t, djangoConfig())

	stdin := `{"cwd": "` + worktreePath + `", "tool_input": {"command": "echo $DJANGO_PORT"}}`
	got := decodeUpdatedCommand(t, runBashPreambleCmd(t, stdin))

	want := "export DJANGO_PORT=8001\necho $DJANGO_PORT"
	if got != want {
		t.Errorf("updatedInput.command =\n%q\nwant\n%q", got, want)
	}
}

func TestBashPreamble_UsesConfiguredPreamble(t *testing.T) {
	setXDGHome(t)
	cfg := djangoConfig()
	cfg.Project.BashPreamble = `eval "$(direnv export bash 2>/dev/null)"`
	_, worktreePath := setupPreambleRepo(t, cfg)

	stdin := `{"cwd": "` + worktreePath + `", "tool_input": {"command": "ls"}}`
	got := decodeUpdatedCommand(t, runBashPreambleCmd(t, stdin))

	want := `eval "$(direnv export bash 2>/dev/null)"` + "\nls"
	if got != want {
		t.Errorf("updatedInput.command =\n%q\nwant\n%q", got, want)
	}
	// The nerve port exports must NOT be present when a custom preamble is set.
	if strings.Contains(got, "DJANGO_PORT") {
		t.Errorf("configured preamble should replace nerve exports, got:\n%q", got)
	}
}

func TestBashPreamble_NoopInMainCheckout(t *testing.T) {
	setXDGHome(t)
	repoPath, _ := setupPreambleRepo(t, djangoConfig())

	stdin := `{"cwd": "` + repoPath + `", "tool_input": {"command": "echo hi"}}`
	out := runBashPreambleCmd(t, stdin)
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty stdout (no-op) in main checkout, got:\n%q", out)
	}
}

func TestBashPreamble_NoopOutsideGitRepo(t *testing.T) {
	setXDGHome(t)
	dir := t.TempDir() // not a git repo
	stdin := `{"cwd": "` + dir + `", "tool_input": {"command": "echo hi"}}`
	out := runBashPreambleCmd(t, stdin)
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty stdout (no-op) outside a git repo, got:\n%q", out)
	}
}
