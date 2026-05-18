package config

import (
	"fmt"
	"strings"
)

// RenderPath substitutes {branch}, {project}, {worktree_id} (and any caller-supplied
// keys) inside a path template. It deliberately does NOT support Go templating —
// keep it dumb and predictable.
//
//	RenderPath(".worktrees/{branch}", map[string]string{"branch": "feat-foo"})
//	-> ".worktrees/feat-foo"
func RenderPath(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// RenderTemplateBody applies {{key}} substitution to a file body. Unlike RenderPath
// this uses double braces so dotenv files using shell braces aren't accidentally
// substituted. Returns an error if an undeclared placeholder is encountered.
func RenderTemplateBody(body string, vars map[string]string) (string, error) {
	out := body
	var missing []string
	for {
		i := strings.Index(out, "{{")
		if i < 0 {
			break
		}
		j := strings.Index(out[i:], "}}")
		if j < 0 {
			return "", fmt.Errorf("unterminated template placeholder at offset %d", i)
		}
		key := strings.TrimSpace(out[i+2 : i+j])
		val, ok := vars[key]
		if !ok {
			missing = append(missing, key)
			// Skip past this placeholder so we keep scanning.
			out = out[:i] + "\x00" + out[i+j+2:]
			continue
		}
		out = out[:i] + val + out[i+j+2:]
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("undefined template vars: %s", strings.Join(missing, ", "))
	}
	return strings.ReplaceAll(out, "\x00", ""), nil
}
