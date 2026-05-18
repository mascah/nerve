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
	if string(raw) != "FOO=1\nBAR=2\n" {
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
