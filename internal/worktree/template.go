package worktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mascah/nerve/internal/atomicfile"
	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/envfile"
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
		if err := atomicfile.Write(dst, []byte(rendered), 0o644); err != nil {
			return err
		}
		if log != nil {
			fmt.Fprintf(log, "  template: %s -> %s%s\n", t.Source, t.Dest, mergedSuffix(t.Merge))
		}
	}
	return nil
}

// RenderEnv (re)renders a worktree's templates and rewrites its .env.local from
// the project config: per-service ports plus static vars (each value run through
// the {{...}} engine with branch/project/worktree_path/branch_slug + ports.<id>
// in scope). It deliberately does NOT copy clone_files — those are one-time copies
// made at create. Create and `nerve refresh` both call this so the two paths can't
// drift. Returns the env map so callers can reuse it (e.g. for hook environments).
func RenderEnv(repoRoot, worktreePath, branch, project, branchSlug string, portByService map[string]int, cfg *config.ProjectConfig, log io.Writer) (map[string]string, error) {
	if log == nil {
		log = io.Discard
	}
	tmplVars := map[string]string{
		"branch":        branch,
		"project":       project,
		"worktree_path": worktreePath,
		"branch_slug":   branchSlug,
	}
	for id, p := range portByService {
		tmplVars["ports."+id] = strconv.Itoa(p)
	}

	if len(cfg.Templates) > 0 {
		fmt.Fprintln(log, "rendering templates:")
		if err := renderTemplates(repoRoot, worktreePath, cfg, tmplVars, log); err != nil {
			return nil, err
		}
	}

	// .env.local = per-service ports + static vars. Vars render through the same
	// {{...}} engine as templates, so a value can interpolate ports.<id> etc.
	envVars := make(map[string]string, len(cfg.Services)+len(cfg.Vars))
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		envVars[svc.EnvKey] = strconv.Itoa(portByService[svc.ID])
	}
	for i := range cfg.Vars {
		v := &cfg.Vars[i]
		rendered, err := config.RenderTemplateBody(v.Value, tmplVars)
		if err != nil {
			return nil, fmt.Errorf("render var %s: %w", v.EnvKey, err)
		}
		envVars[v.EnvKey] = rendered
	}
	envPath := filepath.Join(worktreePath, ".env.local")
	if err := envfile.WriteFile(envPath, envVars); err != nil {
		return nil, fmt.Errorf("write .env.local: %w", err)
	}
	fmt.Fprintf(log, "wrote %s\n", envPath)
	return envVars, nil
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
