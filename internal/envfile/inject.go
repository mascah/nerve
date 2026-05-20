package envfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AppendToClaudeEnv appends `export KEY=VALUE` lines from vars to the file at
// $CLAUDE_ENV_FILE. Claude Code sources that file as a shell preamble for subsequent
// Bash tool calls — per the hooks docs, the file must contain `export` statements,
// not bare dotenv assignments, otherwise the vars stay local to the sourcing shell
// and don't reach child processes. Returns false (no error) when $CLAUDE_ENV_FILE
// is unset; returns true after a successful append.
func AppendToClaudeEnv(vars map[string]string) (bool, error) {
	path := os.Getenv("CLAUDE_ENV_FILE")
	if path == "" {
		return false, nil
	}
	body := RenderShell(vars)
	if body == "" {
		return true, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	return true, nil
}

// WriteFile writes vars to the given path atomically, overwriting any prior contents.
// Used for per-worktree .env.local generation.
func WriteFile(path string, vars map[string]string) error {
	if path == "" {
		return errors.New("envfile: empty path")
	}
	body := Render(vars)
	tmp, err := os.CreateTemp(filepath.Dir(path), "nerve-envfile-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
