package worktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mascah/nerve/internal/config"
)

// renderTemplates applies each template entry from cfg.Templates against srcRoot,
// writing to dstRoot. Substitution variables are taken from tmplVars; the supported
// keys are: branch, project, worktree_path, branch_slug, and ports.<id> for each
// configured service. For entries with Merge=true, the existing destination file's
// keys are preserved and new keys from the source are appended (dotenv-style
// additive merge).
func renderTemplates(srcRoot, dstRoot string, cfg *config.ProjectConfig, tmplVars map[string]string, log io.Writer) error {
	for _, t := range cfg.Templates {
		src := filepath.Join(srcRoot, t.Source)
		dst := filepath.Join(dstRoot, t.Dest)

		raw, err := os.ReadFile(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if log != nil {
					fmt.Fprintf(log, "  template skip (missing source): %s\n", t.Source)
				}
				continue
			}
			return fmt.Errorf("read template %s: %w", src, err)
		}
		rendered, err := config.RenderTemplateBody(string(raw), tmplVars)
		if err != nil {
			return fmt.Errorf("render %s: %w", t.Source, err)
		}
		if t.Merge {
			merged, err := dotenvMerge(dst, rendered)
			if err != nil {
				return fmt.Errorf("merge %s: %w", dst, err)
			}
			rendered = merged
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(dst, []byte(rendered), 0o644); err != nil {
			return err
		}
		if log != nil {
			fmt.Fprintf(log, "  template: %s -> %s%s\n", t.Source, t.Dest, mergedSuffix(t.Merge))
		}
	}
	return nil
}

func mergedSuffix(m bool) string {
	if m {
		return " (merged)"
	}
	return ""
}

// dotenvMerge takes a rendered template body (the "source") and merges it with the
// existing content at dst (if any). Keys present in dst are kept; keys present only
// in the source are appended at the bottom. Lines that aren't KEY=VAL (comments, blank)
// from the source are appended only when the file doesn't already exist.
func dotenvMerge(dst, source string) (string, error) {
	existing, err := os.ReadFile(dst)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return source, nil
		}
		return "", err
	}
	existingKeys := dotenvKeys(string(existing))

	var newLines []string
	for _, line := range strings.Split(strings.TrimRight(source, "\n"), "\n") {
		key, isKV := parseDotenvKey(line)
		if !isKV {
			continue
		}
		if _, present := existingKeys[key]; present {
			continue
		}
		newLines = append(newLines, line)
	}
	if len(newLines) == 0 {
		return string(existing), nil
	}
	out := strings.TrimRight(string(existing), "\n") + "\n" + strings.Join(newLines, "\n") + "\n"
	return out, nil
}

func dotenvKeys(body string) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, line := range strings.Split(body, "\n") {
		if k, ok := parseDotenvKey(line); ok {
			keys[k] = struct{}{}
		}
	}
	return keys
}

func parseDotenvKey(line string) (string, bool) {
	trim := strings.TrimSpace(line)
	if trim == "" || strings.HasPrefix(trim, "#") {
		return "", false
	}
	if strings.HasPrefix(trim, "export ") {
		trim = strings.TrimSpace(strings.TrimPrefix(trim, "export "))
	}
	eq := strings.IndexByte(trim, '=')
	if eq <= 0 {
		return "", false
	}
	return strings.TrimSpace(trim[:eq]), true
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
