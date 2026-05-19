package worktree

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// copyWorktreeInclude reads <repoRoot>/.worktreeinclude (Claude Code's convention,
// gitignore-syntax) and copies each matching file from repoRoot into worktreePath.
// Only entries that ALSO appear in .gitignore would normally be eligible per the
// Claude Code spec, but for nerve's lightweight mode we don't enforce that — if a
// path is listed in .worktreeinclude, we copy it. Missing files are skipped silently.
func copyWorktreeInclude(repoRoot, worktreePath string, log io.Writer) error {
	path := filepath.Join(repoRoot, ".worktreeinclude")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		src := filepath.Join(repoRoot, line)
		dst := filepath.Join(worktreePath, line)
		info, err := os.Stat(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDirShallow(src, dst); err != nil {
				return err
			}
		} else {
			if err := copyFileSimple(src, dst, info.Mode()); err != nil {
				return err
			}
		}
		if log != nil {
			fmt.Fprintf(log, "  worktreeinclude: %s\n", line)
		}
	}
	return s.Err()
}

// copyClaudeSettings replicates <repoRoot>/.claude/settings.json (and the
// per-user override file settings.local.json, when present) into the new
// worktree. This is required for the Claude Code hooks integration: a linked
// worktree is treated by Claude Code as its own project root, so hooks
// installed only at the main checkout's .claude/settings.json are invisible
// inside the worktree. Best-effort — missing source files are not errors, and
// any IO failure is reported to the log without aborting the worktree create.
func copyClaudeSettings(repoRoot, worktreePath string, log io.Writer) error {
	srcDir := filepath.Join(repoRoot, ".claude")
	if _, err := os.Stat(srcDir); err != nil {
		return nil
	}
	dstDir := filepath.Join(worktreePath, ".claude")
	for _, name := range []string{"settings.json", "settings.local.json"} {
		src := filepath.Join(srcDir, name)
		info, err := os.Stat(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
		dst := filepath.Join(dstDir, name)
		if err := copyFileSimple(src, dst, info.Mode()); err != nil {
			return err
		}
		if log != nil {
			fmt.Fprintf(log, "  copied .claude/%s\n", name)
		}
	}
	return nil
}

func copyFileSimple(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyDirShallow(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFileSimple(path, target, info.Mode())
	})
}
