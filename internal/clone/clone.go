// Package clone copies files and directories from a source root into a destination
// root, preserving mode bits. It refuses to traverse outside either root.
package clone

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mascah/nerve/internal/config"
)

// Result tabulates what happened during a Run.
type Result struct {
	Copied  []string
	Skipped []string // present in config but missing on disk with required=false
	Errors  map[string]error
}

// HasErrors returns true if any entry failed.
func (r *Result) HasErrors() bool { return len(r.Errors) > 0 }

// Run copies each entry in entries from srcRoot to dstRoot. If a required entry is
// missing in srcRoot, Run returns a partial Result and a non-nil error.
func Run(srcRoot, dstRoot string, entries []config.CloneFile) (*Result, error) {
	srcRoot = filepath.Clean(srcRoot)
	dstRoot = filepath.Clean(dstRoot)
	if !filepath.IsAbs(srcRoot) || !filepath.IsAbs(dstRoot) {
		return nil, fmt.Errorf("clone: roots must be absolute (src=%q dst=%q)", srcRoot, dstRoot)
	}

	res := &Result{Errors: map[string]error{}}
	for _, e := range entries {
		if err := validateEntry(e); err != nil {
			res.Errors[e.Path] = err
			continue
		}
		src := filepath.Join(srcRoot, e.Path)
		dst := filepath.Join(dstRoot, e.Path)

		info, err := os.Lstat(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if e.Required {
					res.Errors[e.Path] = fmt.Errorf("required clone entry missing in source: %s", e.Path)
					continue
				}
				res.Skipped = append(res.Skipped, e.Path)
				continue
			}
			res.Errors[e.Path] = err
			continue
		}

		kind := e.Kind
		if kind == "" {
			if info.IsDir() {
				kind = config.CloneKindDirectory
			} else {
				kind = config.CloneKindFile
			}
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			res.Errors[e.Path] = err
			continue
		}

		switch kind {
		case config.CloneKindFile:
			if info.IsDir() {
				res.Errors[e.Path] = fmt.Errorf("expected file but found directory: %s", e.Path)
				continue
			}
			if err := copyFile(src, dst, info.Mode()); err != nil {
				res.Errors[e.Path] = err
				continue
			}
		case config.CloneKindDirectory:
			if !info.IsDir() {
				res.Errors[e.Path] = fmt.Errorf("expected directory but found file: %s", e.Path)
				continue
			}
			if err := copyTree(src, dst); err != nil {
				res.Errors[e.Path] = err
				continue
			}
		default:
			res.Errors[e.Path] = fmt.Errorf("unknown clone kind %q", kind)
			continue
		}
		res.Copied = append(res.Copied, e.Path)
	}
	if res.HasErrors() {
		return res, fmt.Errorf("clone: %d entry/entries failed", len(res.Errors))
	}
	return res, nil
}

func validateEntry(e config.CloneFile) error {
	if e.Path == "" {
		return errors.New("empty path")
	}
	clean := filepath.Clean(e.Path)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("path must stay inside the source root: %q", e.Path)
	}
	return nil
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode.Perm()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dst)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		default:
			return copyFile(path, target, info.Mode())
		}
	})
}
