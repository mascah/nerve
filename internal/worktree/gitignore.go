package worktree

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureGitignore appends each entry to repoRoot/.gitignore if it isn't already
// present. Returns the list of entries that were actually added. Idempotent.
func EnsureGitignore(repoRoot string, entries []string) ([]string, error) {
	path := filepath.Join(repoRoot, ".gitignore")
	existing, err := readGitignoreLines(path)
	if err != nil {
		return nil, err
	}
	existingSet := make(map[string]bool, len(existing))
	for _, l := range existing {
		existingSet[strings.TrimSpace(l)] = true
	}

	var toAdd []string
	for _, e := range entries {
		if !existingSet[strings.TrimSpace(e)] {
			toAdd = append(toAdd, e)
		}
	}
	if len(toAdd) == 0 {
		return nil, nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	// Make sure we start on a fresh line.
	if needsLeadingNewline(path) {
		if _, err := f.WriteString("\n"); err != nil {
			return nil, err
		}
	}
	if _, err := f.WriteString("# Added by nerve\n"); err != nil {
		return nil, err
	}
	for _, e := range toAdd {
		if _, err := f.WriteString(e + "\n"); err != nil {
			return nil, err
		}
	}
	return toAdd, nil
}

func readGitignoreLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	return lines, s.Err()
}

func needsLeadingNewline(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.Seek(-1, 2); err != nil {
		return false
	}
	buf := make([]byte, 1)
	if _, err := f.Read(buf); err != nil {
		return false
	}
	return buf[0] != '\n'
}
