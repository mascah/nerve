package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSorted(t *testing.T) {
	out := Render(map[string]string{
		"BBB": "2",
		"AAA": "1",
		"CCC": "3",
	})
	want := "AAA=1\nBBB=2\nCCC=3\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestRenderQuotesWhenNeeded(t *testing.T) {
	out := Render(map[string]string{
		"PLAIN":  "8001",
		"PATH":   "/usr/local/bin",
		"SPACED": "hello world",
		"EMPTY":  "",
		"QUOTE":  `say "hi"`,
	})
	for _, want := range []string{
		"PLAIN=8001",
		"PATH=/usr/local/bin",
		`SPACED="hello world"`,
		`EMPTY=""`,
		`QUOTE="say \"hi\""`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, out)
		}
	}
}

func TestRenderShellPrefix(t *testing.T) {
	out := RenderShell(map[string]string{"PORT": "8001"})
	if out != "export PORT=8001\n" {
		t.Errorf("got %q", out)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")
	if err := WriteFile(path, map[string]string{"K": "v"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "K=v\n" {
		t.Errorf("got %q", string(raw))
	}
}

// TestWriteFileNoLeftoverTemp verifies that WriteFile writes the expected
// contents and leaves no leftover temp files in the target directory.
func TestWriteFileNoLeftoverTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")

	vars := map[string]string{"DJANGO_PORT": "8001", "VITE_PORT": "5173"}
	if err := WriteFile(path, vars); err != nil {
		t.Fatal(err)
	}

	// Verify the written contents.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "DJANGO_PORT=8001\nVITE_PORT=5173\n"
	if string(raw) != want {
		t.Errorf("contents: got %q, want %q", string(raw), want)
	}

	// Verify no leftover temp files remain in the target directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".env.local" {
			t.Errorf("unexpected leftover file in target dir: %s", e.Name())
		}
	}
}

// TestWriteFileTempInTargetDir verifies that the temp file is created in the
// target's directory (same filesystem), which avoids EXDEV errors on cross-device
// renames when $TMPDIR is on a different filesystem than the target path.
func TestWriteFileTempInTargetDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")

	if err := WriteFile(path, map[string]string{"PORT": "9000"}); err != nil {
		t.Fatal(err)
	}

	// Confirm the file exists at the target path with correct permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("permissions: got %04o, want 0644", perm)
	}

	// Confirm contents are correct.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "PORT=9000\n" {
		t.Errorf("contents: got %q", string(raw))
	}
}

func TestAppendToClaudeEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "claude.env")
	t.Setenv("CLAUDE_ENV_FILE", envPath)

	ok, err := AppendToClaudeEnv(map[string]string{"FOO": "1"})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	ok, err = AppendToClaudeEnv(map[string]string{"BAR": "2"})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	raw, _ := os.ReadFile(envPath)
	if string(raw) != "export FOO=1\nexport BAR=2\n" {
		t.Errorf("got %q", string(raw))
	}
}

func TestAppendToClaudeEnvUnset(t *testing.T) {
	t.Setenv("CLAUDE_ENV_FILE", "")
	ok, err := AppendToClaudeEnv(map[string]string{"FOO": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("ok should be false when $CLAUDE_ENV_FILE is unset")
	}
}
