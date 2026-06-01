package tui

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// pathSuggestSkipDirs are directories we never descend into when building
// autocomplete suggestions. They tend to be huge, irrelevant, or recursive
// (nerve worktrees themselves contain checkouts of the same repo).
var pathSuggestSkipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	".venv":        {},
	"venv":         {},
	".worktrees":   {},
	".nerve":       {},
}

// pathSuggestMaxDepth caps how deeply we descend below repoRoot. Two levels of
// nesting plus the root is plenty for clone-file targets (.env, .npmrc,
// config/settings.local.py, etc.) and keeps the walk fast on big trees.
const pathSuggestMaxDepth = 3

// pathSuggestScanCeiling bounds the total number of entries we inspect during
// a single call so a pathological tree can't lag the TUI keystroke loop.
const pathSuggestScanCeiling = 2000

// listPathSuggestions returns up to max relative paths under repoRoot whose
// names begin with query (case-insensitive). When query contains a "/", the
// portion before the final "/" is treated as a directory to descend into and
// the trailing segment is matched against entries inside that directory.
//
// Suggestions are returned sorted, with directories suffixed by "/" so the
// caller can distinguish them visually. Paths returned are relative to
// repoRoot using forward slashes (since users will type forward slashes in
// the form regardless of OS).
func listPathSuggestions(repoRoot, query string, max int) []string {
	if max <= 0 || repoRoot == "" {
		return nil
	}

	// Split the query into a directory prefix and a leaf to match. The leaf
	// may be empty (e.g. query == "config/") in which case we list everything
	// in the directory.
	dirPart, leafPart := splitQuery(query)
	leafLower := strings.ToLower(leafPart)

	scanRoot := repoRoot
	if dirPart != "" {
		// If the user is explicitly typing into a path under a skip-dir
		// (e.g. "node_modules/foo"), refuse to suggest anything inside it.
		// We check every path segment to catch nested cases too.
		for _, seg := range strings.Split(dirPart, "/") {
			if _, skip := pathSuggestSkipDirs[seg]; skip {
				return nil
			}
		}
		scanRoot = filepath.Join(repoRoot, filepath.FromSlash(dirPart))
	}

	var out []string
	scanned := 0

	// Walk errors (unreadable subtrees, a vanished scanRoot, etc.) are non-fatal:
	// we return whatever entries we managed to collect before the error.
	_ = filepath.WalkDir(scanRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Quietly skip unreadable subtrees rather than failing the whole walk.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == scanRoot {
			return nil
		}
		scanned++
		if scanned > pathSuggestScanCeiling {
			return filepath.SkipAll
		}

		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		depth := strings.Count(relSlash, "/") + 1

		name := d.Name()

		if d.IsDir() {
			if _, skip := pathSuggestSkipDirs[name]; skip {
				return filepath.SkipDir
			}
			if depth >= pathSuggestMaxDepth {
				// We've recorded this dir if it matches; don't descend further.
				if matchesAtScanLevel(rel, dirPart, name, leafLower) {
					out = appendUnique(out, relSlash+"/")
				}
				return filepath.SkipDir
			}
		}

		// Only suggest entries that live directly inside scanRoot — i.e. at
		// the level the user is currently typing. Deeper entries will surface
		// once the user types another "/".
		parent := filepath.Dir(path)
		if parent != scanRoot {
			return nil
		}

		if !strings.HasPrefix(strings.ToLower(name), leafLower) {
			return nil
		}

		entry := relSlash
		if d.IsDir() {
			entry += "/"
		}
		out = appendUnique(out, entry)
		return nil
	})

	sort.Slice(out, func(i, j int) bool {
		// Directories first, then alphabetical (case-insensitive).
		di := strings.HasSuffix(out[i], "/")
		dj := strings.HasSuffix(out[j], "/")
		if di != dj {
			return di
		}
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})

	if len(out) > max {
		out = out[:max]
	}
	return out
}

// splitQuery returns the directory portion (without trailing slash) and the
// leaf to match. Examples:
//
//	"" -> "", ""
//	".en" -> "", ".en"
//	"config/" -> "config", ""
//	"config/sett" -> "config", "sett"
func splitQuery(q string) (dir, leaf string) {
	q = filepath.ToSlash(strings.TrimPrefix(q, "./"))
	idx := strings.LastIndex(q, "/")
	if idx < 0 {
		return "", q
	}
	return q[:idx], q[idx+1:]
}

// matchesAtScanLevel decides whether a directory hit at the depth ceiling
// should still be reported as a suggestion.
func matchesAtScanLevel(rel, dirPart, name, leafLower string) bool {
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		parent = ""
	}
	if parent != dirPart {
		return false
	}
	return strings.HasPrefix(strings.ToLower(name), leafLower)
}

func appendUnique(s []string, v string) []string {
	for _, existing := range s {
		if existing == v {
			return s
		}
	}
	return append(s, v)
}
