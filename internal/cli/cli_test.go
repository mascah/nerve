package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// setXDGHome redirects LoadGlobalRegistry / SaveGlobalRegistry to a temp dir
// so tests never touch the real ~/.config/nerve/projects.yaml.
func setXDGHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
}

// execCmd runs cmd with the given args and returns (stdout, stderr, error).
func execCmd(t *testing.T, cmd *cobra.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ----- Root command / subcommand registration --------------------------------

func TestNewRootCmd_Subcommands(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()

	want := []string{
		"version",
		"init",
		"project",
		"new",
		"remove",
		"list",
		"env",
		"ports",
		"hooks",
		"refresh",
		"doctor",
	}

	registered := map[string]bool{}
	for _, sub := range cmd.Commands() {
		registered[sub.Name()] = true
	}

	for _, name := range want {
		if !registered[name] {
			t.Errorf("expected subcommand %q to be registered", name)
		}
	}
}

func TestNewRootCmd_HiddenHookCommands(t *testing.T) {
	cmd := NewRootCmd()
	names := map[string]*cobra.Command{}
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = sub
	}

	// worktree-create and worktree-remove must exist but be hidden.
	for _, name := range []string{"worktree-create", "worktree-remove"} {
		sub, ok := names[name]
		if !ok {
			t.Errorf("expected hidden command %q to be registered", name)
			continue
		}
		if !sub.Hidden {
			t.Errorf("expected command %q to be hidden", name)
		}
	}
}

// ----- Help / usage ----------------------------------------------------------

func TestRootCmd_HelpNoError(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	out, _, err := execCmd(t, cmd, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	if !strings.Contains(out, "nerve") {
		t.Errorf("help output missing 'nerve': %q", out)
	}
}

func TestVersionCmd_HelpNoError(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	out, _, err := execCmd(t, cmd, "version", "--help")
	if err != nil {
		t.Fatalf("version --help returned error: %v", err)
	}
	if !strings.Contains(out, "version") {
		t.Errorf("expected 'version' in help output, got: %q", out)
	}
}

func TestProjectCmd_HelpNoError(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	out, _, err := execCmd(t, cmd, "project", "--help")
	if err != nil {
		t.Fatalf("project --help returned error: %v", err)
	}
	for _, sub := range []string{"add", "list", "remove"} {
		if !strings.Contains(out, sub) {
			t.Errorf("project help missing subcommand %q", sub)
		}
	}
}

// ----- version subcommand ----------------------------------------------------

func TestVersionCmd_PrintsVersion(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	out, _, err := execCmd(t, cmd, "version")
	if err != nil {
		t.Fatalf("version returned error: %v", err)
	}
	// version.Version is "dev" in tests (not overridden by ldflags).
	if strings.TrimSpace(out) == "" {
		t.Error("version output is empty")
	}
}

// ----- Flag parsing ----------------------------------------------------------

func TestNewCmd_Flags(t *testing.T) {
	setXDGHome(t)
	root := NewRootCmd()

	var newCmd *cobra.Command
	for _, sub := range root.Commands() {
		if sub.Name() == "new" {
			newCmd = sub
			break
		}
	}
	if newCmd == nil {
		t.Fatal("'new' command not found")
	}

	for _, flagName := range []string{"from", "no-hooks", "minimal", "print-cd"} {
		if newCmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected flag --%s on 'new' command", flagName)
		}
	}
}

func TestRemoveCmd_Flags(t *testing.T) {
	setXDGHome(t)
	root := NewRootCmd()

	var removeCmd *cobra.Command
	for _, sub := range root.Commands() {
		if sub.Name() == "remove" {
			removeCmd = sub
			break
		}
	}
	if removeCmd == nil {
		t.Fatal("'remove' command not found")
	}

	for _, flagName := range []string{"force", "keep-branch"} {
		if removeCmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected flag --%s on 'remove' command", flagName)
		}
	}
}

func TestEnvCmd_Flags(t *testing.T) {
	setXDGHome(t)
	root := NewRootCmd()

	var envCmd *cobra.Command
	for _, sub := range root.Commands() {
		if sub.Name() == "env" {
			envCmd = sub
			break
		}
	}
	if envCmd == nil {
		t.Fatal("'env' command not found")
	}

	for _, flagName := range []string{"inject", "shell", "json", "worktree"} {
		if envCmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected flag --%s on 'env' command", flagName)
		}
	}
}

func TestHooksCmd_Subcommands(t *testing.T) {
	setXDGHome(t)
	root := NewRootCmd()

	var hooksCmd *cobra.Command
	for _, sub := range root.Commands() {
		if sub.Name() == "hooks" {
			hooksCmd = sub
			break
		}
	}
	if hooksCmd == nil {
		t.Fatal("'hooks' command not found")
	}

	names := map[string]bool{}
	for _, sub := range hooksCmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"install", "uninstall", "show"} {
		if !names[want] {
			t.Errorf("expected hooks subcommand %q", want)
		}
	}
}

func TestPortsCmd_Subcommands(t *testing.T) {
	setXDGHome(t)
	root := NewRootCmd()

	var portsCmd *cobra.Command
	for _, sub := range root.Commands() {
		if sub.Name() == "ports" {
			portsCmd = sub
			break
		}
	}
	if portsCmd == nil {
		t.Fatal("'ports' command not found")
	}

	names := map[string]bool{}
	for _, sub := range portsCmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"list", "cleanup", "status"} {
		if !names[want] {
			t.Errorf("expected ports subcommand %q", want)
		}
	}
}

// ----- project list (hermetic — empty registry) -----------------------------

func TestProjectListCmd_EmptyRegistry(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	out, _, err := execCmd(t, cmd, "project", "list")
	if err != nil {
		t.Fatalf("project list returned error: %v", err)
	}
	if !strings.Contains(out, "no projects") {
		t.Errorf("expected 'no projects' in output, got: %q", out)
	}
}

func TestProjectListCmd_JSONFlag(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	out, _, err := execCmd(t, cmd, "project", "list", "--json")
	if err != nil {
		t.Fatalf("project list --json returned error: %v", err)
	}
	// Empty registry → JSON array (null or []).
	trimmed := strings.TrimSpace(out)
	if trimmed != "null" && trimmed != "[]" {
		t.Errorf("expected null or [] JSON for empty registry, got: %q", out)
	}
}

func TestProjectRemoveCmd_NotRegistered(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	_, _, err := execCmd(t, cmd, "project", "remove", "nonexistent")
	if err == nil {
		t.Fatal("expected error for removing unknown project, got nil")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error should mention 'not registered': %v", err)
	}
}

// ----- hooks show (no git context needed) ------------------------------------

func TestHooksShowCmd_PrintsJSON(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	out, _, err := execCmd(t, cmd, "hooks", "show")
	if err != nil {
		t.Fatalf("hooks show returned error: %v", err)
	}
	if !strings.Contains(out, "nerve-managed") {
		t.Errorf("hooks show output missing sentinel 'nerve-managed': %q", out)
	}
}

// ----- env command: no-op outside worktree -----------------------------------

func TestEnvCmd_NoopOutsideWorktree(t *testing.T) {
	setXDGHome(t)
	// Point --worktree at a temp dir that is NOT a git repo.
	dir := t.TempDir()
	cmd := NewRootCmd()
	// --inject in a non-worktree should silently succeed (no-op contract).
	_, _, err := execCmd(t, cmd, "env", "--inject", "--worktree", dir)
	if err != nil {
		t.Fatalf("env --inject outside worktree should be a silent no-op, got error: %v", err)
	}
}

// ----- persistent --verbose flag --------------------------------------------

func TestRootCmd_VerboseFlag(t *testing.T) {
	setXDGHome(t)
	cmd := NewRootCmd()
	if cmd.PersistentFlags().Lookup("verbose") == nil {
		t.Error("expected persistent flag --verbose on root command")
	}
}

// ----- exit codes -----------------------------------------------------------

func TestExitCodeError_ErrorAndUnwrap(t *testing.T) {
	inner := errString("inner error")
	e := exitCodeError{Code: ExitPoolExhausted, Err: inner}
	if e.Error() != "inner error" {
		t.Errorf("Error() = %q, want %q", e.Error(), "inner error")
	}
	if e.Unwrap() != inner {
		t.Errorf("Unwrap() != inner error")
	}
	if e.Code != ExitPoolExhausted {
		t.Errorf("Code = %d, want %d", e.Code, ExitPoolExhausted)
	}
}

// errString is a trivial error type for testing.
type errString string

func (e errString) Error() string { return string(e) }

// ----- printDirtyFiles -------------------------------------------------------

func TestPrintDirtyFiles_TruncatesAtMax(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&buf)

	files := make([]string, 30)
	for i := range files {
		files[i] = "file.go"
	}
	printDirtyFiles(cmd, files, 25)
	out := buf.String()
	if !strings.Contains(out, "5 more") {
		t.Errorf("expected '5 more' in truncated output, got: %q", out)
	}
}

func TestPrintDirtyFiles_NoTruncation(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&buf)

	files := []string{"a.go", "b.go", "c.go"}
	printDirtyFiles(cmd, files, 25)
	out := buf.String()
	if strings.Contains(out, "more") {
		t.Errorf("expected no truncation, got: %q", out)
	}
}
