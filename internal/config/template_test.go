package config

import (
	"strings"
	"testing"
)

func TestRenderPath(t *testing.T) {
	cases := []struct {
		name string
		tmpl string
		vars map[string]string
		want string
	}{
		{"branch only", ".worktrees/{branch}", map[string]string{"branch": "feat-foo"}, ".worktrees/feat-foo"},
		{"branch + project", "{project}/{branch}", map[string]string{"branch": "x", "project": "demo"}, "demo/x"},
		{"no vars", "static/path", nil, "static/path"},
		{"unmatched placeholder is left alone", ".worktrees/{branch}/{missing}", map[string]string{"branch": "x"}, ".worktrees/x/{missing}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RenderPath(c.tmpl, c.vars)
			if got != c.want {
				t.Errorf("RenderPath(%q, %v) = %q, want %q", c.tmpl, c.vars, got, c.want)
			}
		})
	}
}

func TestRenderTemplateBody(t *testing.T) {
	got, err := RenderTemplateBody("port={{django}}\nbranch={{branch}}\n", map[string]string{"django": "8003", "branch": "feat-foo"})
	if err != nil {
		t.Fatal(err)
	}
	want := "port=8003\nbranch=feat-foo\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderTemplateBodyMissing(t *testing.T) {
	_, err := RenderTemplateBody("port={{django}}\n", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "django") {
		t.Errorf("expected missing-var error mentioning django, got %v", err)
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"feat/OF-351_thing", "feat_of_351_thing"},
		{"Bug-Fix.v2", "bug_fix_v2"},
		{"feat-foo", "feat_foo"},
		{"already_ok", "already_ok"},
		{"feat/", "feat"},      // trailing underscore trimmed
		{"feat__", "feat"},     // trailing underscores trimmed
		{".hidden", "_hidden"}, // leading underscore preserved
		{"a//b", "a__b"},       // runs not collapsed
		{"", ""},
		{"___", ""}, // no alphanumerics -> empty
		{"-.-", ""}, // no alphanumerics -> empty
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := Slugify(c.in); got != c.want {
				t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
