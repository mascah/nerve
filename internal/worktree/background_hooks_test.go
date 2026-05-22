package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mascah/nerve/internal/config"
)

// TestRunHooksParallel_RunsAllAndReportsFirstFailure checks that every command runs
// (even when one fails) and the earliest-declared failure is the one returned.
func TestRunHooksParallel_RunsAllAndReportsFirstFailure(t *testing.T) {
	dir := t.TempDir()
	cmds := []string{
		"touch a.txt",
		"exit 3", // fails — should be the reported HookError
		"touch c.txt",
	}
	err := RunHooksParallel(dir, cmds, nil, nil)
	if err == nil {
		t.Fatal("expected a HookError from the failing command, got nil")
	}
	var he *HookError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HookError, got %T: %v", err, err)
	}
	if he.Command != "exit 3" || he.ExitCode != 3 {
		t.Errorf("HookError = {%q, %d}, want {\"exit 3\", 3}", he.Command, he.ExitCode)
	}
	// Both non-failing commands still ran to completion (independence).
	for _, f := range []string{"a.txt", "c.txt"} {
		if _, statErr := os.Stat(filepath.Join(dir, f)); statErr != nil {
			t.Errorf("expected %s to exist (all commands run despite a sibling failure): %v", f, statErr)
		}
	}
}

// TestCreate_ForegroundInlineBackgroundDetached is the core regression test for the
// per-command background model (the direnv-allow fix): foreground hooks run
// synchronously inside Create, while background hooks are handed off to the detached
// runner — never run inline.
func TestCreate_ForegroundInlineBackgroundDetached(t *testing.T) {
	repo := initRepo(t)

	cfg := configWithService()
	cfg.Hooks.PostCreate = config.HookCommands{
		config.Hook("touch fg_ran.txt"),           // foreground → inline
		config.BackgroundHook("touch bg_ran.txt"), // background → detached
	}

	// Stub the detached spawn so the background command is captured, not executed.
	var spawnArgs []string
	var spawnEnv map[string]string
	orig := spawnDetachedFn
	spawnDetachedFn = func(env map[string]string, args ...string) error {
		spawnEnv = env
		spawnArgs = args
		return nil
	}
	defer func() { spawnDetachedFn = orig }()

	res, err := Create(CreateOptions{RepoRoot: repo, Branch: "feat", Cfg: cfg})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Foreground hook ran inline → its file exists in the worktree.
	if _, statErr := os.Stat(filepath.Join(res.Path, "fg_ran.txt")); statErr != nil {
		t.Errorf("foreground hook should have run inline: %v", statErr)
	}
	// Background hook was deferred to the (stubbed) detached child → NOT run inline.
	if _, statErr := os.Stat(filepath.Join(res.Path, "bg_ran.txt")); !os.IsNotExist(statErr) {
		t.Errorf("background hook must not run inline (should be detached); stat err: %v", statErr)
	}
	// The detached runner was invoked with run-hooks for this worktree.
	if !contains(spawnArgs, "run-hooks") {
		t.Errorf("expected detached spawn of run-hooks, got args: %v", spawnArgs)
	}
	// The hook env (ports + identity vars) was carried into the detached spawn.
	if spawnEnv["DJANGO_PORT"] == "" || spawnEnv["BRANCH"] != "feat" {
		t.Errorf("expected hook env carried into spawn (DJANGO_PORT, BRANCH=feat), got: %v", spawnEnv)
	}
}

// TestCreate_AllForegroundNeverSpawns confirms a config with only foreground hooks
// (the default) never reaches the detached path.
func TestCreate_AllForegroundNeverSpawns(t *testing.T) {
	repo := initRepo(t)
	cfg := configWithService("touch only_fg.txt")

	spawned := false
	orig := spawnDetachedFn
	spawnDetachedFn = func(map[string]string, ...string) error { spawned = true; return nil }
	defer func() { spawnDetachedFn = orig }()

	res, err := Create(CreateOptions{RepoRoot: repo, Branch: "feat", Cfg: cfg})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if spawned {
		t.Error("foreground-only config must not spawn a detached runner")
	}
	if _, statErr := os.Stat(filepath.Join(res.Path, "only_fg.txt")); statErr != nil {
		t.Errorf("foreground hook should have run: %v", statErr)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
