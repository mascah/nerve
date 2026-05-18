// Package envfile renders KEY=VALUE pairs into dotenv-compatible output and writes
// them either to a file inside a worktree (.env.local) or to $CLAUDE_ENV_FILE so
// Claude Code can source them as a Bash preamble.
package envfile

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Render returns dotenv-formatted text for the given map. Keys are emitted in sorted
// order so output is deterministic across runs. Values containing whitespace, quotes,
// or shell metacharacters are double-quoted with embedded quotes/backslashes escaped.
func Render(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(quoteIfNeeded(vars[k]))
		buf.WriteByte('\n')
	}
	return buf.String()
}

// RenderShell returns lines of the form `export KEY=VALUE` suitable for `eval`.
func RenderShell(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		fmt.Fprintf(&buf, "export %s=%s\n", k, quoteIfNeeded(vars[k]))
	}
	return buf.String()
}

func quoteIfNeeded(v string) string {
	if v == "" {
		return `""`
	}
	if !needsQuote(v) {
		return v
	}
	// Escape backslashes and double-quotes for double-quoted form.
	escaped := strings.ReplaceAll(v, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func needsQuote(v string) bool {
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '_', r == '-', r == '.', r == '/', r == ':':
		default:
			return true
		}
	}
	return false
}
