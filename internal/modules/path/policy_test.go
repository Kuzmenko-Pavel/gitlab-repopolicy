package path

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

const basePolicy = `version: 1
admins: [platform-lead, sre-lead]
rules:
  ci-config:
    match: all
    paths: [".gitlab-ci.yml", ".gitlab/ci/**", "Makefile", "scripts/deploy/**"]
    allow: [platform-lead, sre-lead, ci-maintainer]
    message: >-
      CI/CD configuration is changed by the platform team.
  commit-policy:
    match: all
    paths: [COMMITPOLICY]
    allow: [dev-lead]
    message: COMMITPOLICY belongs to dev-lead.
  prod-values:
    match: [main, '/^release\//']
    paths: ["helm/**/values-prod.yaml"]
    allow: [platform-lead, release-manager]
    message: Prod values are changed by release managers.
    help:
      contact: "#release"
`

func TestParse(t *testing.T) {
	p, err := Parse([]byte(basePolicy), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"platform-lead", "sre-lead"}, p.admins)
	require.Len(t, p.rules, 3)
	assert.Equal(t, "ci-config", p.rules[0].name)
	assert.True(t, p.rules[0].match.All)
	assert.True(t, p.rules[2].match.Matches("release/1.0"))
	assert.Equal(t, module.Help{Contact: "#release"}, p.rules[2].help)
	assert.Equal(t, []string{"file"}, p.rules[2].source)
	assert.True(t, New(nil).Covers(p, "COMMITPOLICY"))
	assert.False(t, New(nil).Covers(p, "TAGPOLICY"))
	assert.Len(t, p.Usernames(), 8)
	_, err = Parse([]byte("version: 1\nadmins: [a]\n"), nil)
	assert.NoError(t, err, "a policy may only guard itself")
}

func TestParse_Presets(t *testing.T) {
	s := NewSettings(map[string]any{"org-ci": map[string]any{"rules": map[string]any{
		"ci":     map[string]any{"match": "all", "paths": []any{".gitlab-ci.yml"}, "allow": []any{"lead"}, "message": "CI belongs to the platform team."},
		"agents": map[string]any{"match": "default", "paths": []any{".gitlab/agents/**"}, "allow": []any{"lead"}, "message": "Agents."},
	}}})
	p, err := Parse([]byte("version: 1\nadmins: [a]\nextends: org-ci\nrules:\n  ci:\n    allow: [lead, alice]\n  agents: null\n"), s)
	require.NoError(t, err)
	require.Len(t, p.rules, 1)
	assert.Equal(t, []string{"lead", "alice"}, p.rules[0].allow)
	assert.Equal(t, []string{"preset:org-ci", "file"}, p.rules[0].source)
	require.Len(t, p.Presets(), 1)
	assert.Equal(t, "org-ci", p.Presets()[0].Name)
	assert.Len(t, p.Presets()[0].SHA256, 64)

	_, err = Parse([]byte("version: 1\nadmins: [a]\nextends: org-other\n"), s)
	assert.EqualError(t, err, `extends: preset "org-other" is not defined in the server config`)
	_, err = Parse([]byte("version: 1\nadmins: [a]\nrules:\n  ci:\n    allow: [alice]\n"), s)
	assert.EqualError(t, err, "line 5: rule ci: match: required", "a partial rule needs a preset rule of the same name")

	broken := NewSettings(map[string]any{"org-ci": map[string]any{"rules": map[string]any{"ci": map[string]any{"match": "all"}}}})
	assert.Equal(t, []string{"presets.pathpolicy.org-ci: rule ci: paths: 1 to 50 patterns are required"}, New(broken).PresetErrors())
}

func TestParse_GroupsAndRolesRejected(t *testing.T) {
	for _, principal := range []string{"group/subgroup", "@maintainers", "role:maintainer", "@alice"} {
		_, err := Parse([]byte(fmt.Sprintf("version: 1\nadmins: [a]\nrules:\n  r:\n    match: all\n    paths: [x]\n    allow: [%q]\n    message: m\n", principal)), nil)
		require.Error(t, err, principal)
		assert.Contains(t, err.Error(), "groups and roles are not supported", principal)
	}
	_, err := Parse([]byte("version: 1\nadmins: [\"group/admins\"]\n"), nil)
	assert.ErrorContains(t, err, "groups and roles are not supported")
}

func TestParse_AdminsLimit(t *testing.T) {
	_, err := Parse([]byte("version: 1\nadmins: [a, b, c, d, e, f]\n"), nil)
	assert.ErrorContains(t, err, "admins: 1 to 5 usernames are required")
	_, err = Parse([]byte("version: 1\nadmins: []\n"), nil)
	assert.ErrorContains(t, err, "admins: 1 to 5")
	_, err = Parse([]byte("version: 1\nadmins: [Alice, alice]\n"), nil)
	assert.ErrorContains(t, err, "listed twice")
}

func TestParse_Limits(t *testing.T) {
	rule := func(name, fields string) string {
		return "  " + name + ": {match: all, paths: [x], allow: [a], message: m" + fields + "}\n"
	}
	rules := func(n int) string {
		var b strings.Builder
		b.WriteString("version: 1\nadmins: [a]\nrules:\n")
		for i := range n {
			b.WriteString(rule(fmt.Sprintf("r%d", i), ""))
		}
		return b.String()
	}
	_, err := Parse([]byte(rules(100)), nil)
	require.NoError(t, err)
	_, err = Parse([]byte(rules(101)), nil)
	assert.ErrorContains(t, err, "101 rules, the limit is 100")

	list := func(n int, prefix string) string {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf("%s%d", prefix, i)
		}
		return "[" + strings.Join(items, ", ") + "]"
	}
	head := "version: 1\nadmins: [a]\nrules:\n"
	_, err = Parse([]byte(head+"  r: {match: all, paths: "+list(51, "p")+", allow: [a], message: m}\n"), nil)
	assert.ErrorContains(t, err, "paths: 1 to 50 patterns")
	_, err = Parse([]byte(head+"  r: {match: all, paths: [x], allow: "+list(51, "u")+", message: m}\n"), nil)
	assert.ErrorContains(t, err, "allow: 1 to 50 usernames")
	_, err = Parse([]byte(head+"  r: {match: all, paths: [x], allow: [], message: m}\n"), nil)
	assert.ErrorContains(t, err, "allow: 1 to 50 usernames", "an empty allow would freeze the paths")
	_, err = Parse([]byte(head+"  r: {match: all, paths: [x], allow: [a], message: "+strings.Repeat("m", 1001)+"}\n"), nil)
	assert.ErrorContains(t, err, "message: 1001 characters")
	for in, want := range map[string]string{
		"version: 2\nadmins: [a]\n":                                                "version: 2",
		"version: 1\nadmins: [a]\ndeny: [x]\n":                                     `unknown field "deny"`,
		head + rule("R", ""):                                                       `rule name "R" must match`,
		head + rule("r", ", refs: protected"):                                      `rule r: unknown field "refs"`,
		head + "  r: {paths: [x], allow: [a], message: m}\n":                       "rule r: match: required",
		head + "  r: {match: all, paths: [x], allow: [a]}\n":                       "rule r: message: required",
		head + "  r: {match: protected, paths: [x], allow: [a], message: m}\n":     `"protected" is not all, default or a list`,
		head + "  r: {match: ['release/*'], paths: [x], allow: [a], message: m}\n": "a glob is not supported",
		head + rule("r", ", help: {docs_url: ftp://x}"):                            "is not an http(s) URL",
		head + "  r: {match: all, paths: [/abs], allow: [a], message: m}\n":        "remove the leading /",
		"version: 1\nadmins: &x [a]\n":                                             "anchors are not allowed",
	} {
		_, err := Parse([]byte(in), nil)
		require.Error(t, err, in)
		assert.Contains(t, err.Error(), want, in)
	}
}

func TestValidatePattern(t *testing.T) {
	for _, ok := range []string{".gitlab-ci.yml", ".gitlab/ci/**", "helm/**/values-prod.yaml", "a/*/b", "x?y", "[abc].txt", "ядро/**"} {
		assert.NoError(t, validatePattern(ok), ok)
	}
	bad := map[string]string{
		"":                         "empty",
		"/abs":                     "leading /",
		`a\b`:                      `"\" is not allowed`,
		"!secret":                  "negation",
		"{a,b}":                    "alternatives",
		"a//b":                     `segment ""`,
		"./a":                      `segment "."`,
		"a/../b":                   `segment ".."`,
		"a/":                       `segment ""`,
		"\xff":                     "UTF-8",
		strings.Repeat("a", 257):   "257 bytes",
		"**/a/**/b/**/c/**/d/**/e": `more than 4 "**"`,
		"[a":                       "invalid glob",
	}
	for p, want := range bad {
		err := validatePattern(p)
		require.Error(t, err, p)
		assert.Contains(t, err.Error(), want, p)
	}
}

func TestMatch_Semantics(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"terraform/prod/**", "terraform/prod/main.tf", true},
		{"terraform/prod/**", "x/terraform/prod/main.tf", false},
		{"Makefile", "src/Makefile", false},
		{"*.yml", "a.yml", true},
		{"*.yml", "dir/a.yml", false},
		{"**/*.yml", "dir/sub/a.yml", true},
		{"**/*.yml", "a.yml", true},
		{"helm/**/values-prod.yaml", "helm/values-prod.yaml", true},
		{"helm/**/values-prod.yaml", "helm/a/b/values-prod.yaml", true},
		{"Makefile", "makefile", false},
		{"ядро/*", "ядро/файл", true},
		{"a?c", "abc", true},
		{"[ab].txt", "c.txt", false},
		{".gitlab/ci/**", ".gitlab/ci", true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, match(tt.pattern, tt.path), "%s ~ %s", tt.pattern, tt.path)
	}
}

// gitPath reports a path as git writes it in a tree: relative, no empty, "."
// or ".." segments.
func gitPath(p string) bool {
	if p == "" || !utf8.ValidString(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func FuzzMatch(f *testing.F) {
	f.Add("helm/**/values-prod.yaml", "helm/a/values-prod.yaml")
	f.Add("*.yml", "a.yml")
	f.Add("Makefile", "Makefile")
	f.Fuzz(func(t *testing.T, pattern, path string) {
		if validatePattern(pattern) != nil || !gitPath(path) {
			return
		}
		got := match(pattern, path)
		if !strings.ContainsAny(pattern, "*?[") && got != (pattern == path) {
			t.Fatalf("literal pattern %q against %q: %v", pattern, path, got)
		}
		if got && !strings.ContainsAny(pattern, "*?[") && strings.ToLower(path) != path && pattern != path {
			t.Fatalf("case-insensitive match of %q and %q", pattern, path)
		}
	})
}

func FuzzValidatePattern(f *testing.F) {
	f.Add(".gitlab/ci/**")
	f.Add("a/../b")
	f.Fuzz(func(t *testing.T, p string) {
		if validatePattern(p) != nil {
			return
		}
		if strings.HasPrefix(p, "/") || strings.Contains(p, "..") && strings.Contains("/"+p+"/", "/../") || len(p) > maxPatternBytes {
			t.Fatalf("accepted %q", p)
		}
	})
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("version: 1\nadmins: [alice]\nrules:\n  ci:\n    match: [main, '/^release\\//']\n    paths: [ci.yml, \"deploy/**\"]\n    allow: [alice]\n    message: m\n"))
	f.Add([]byte("version: 1\nadmins: [alice]\nrules:\n  x:\n    match: all\n    paths: [\"{a,b}\"]\n    allow: [group/x]\n    message: m\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data, nil)
		if err != nil {
			return
		}
		// A parsed policy keeps the limits.
		if len(p.admins) < 1 || len(p.admins) > maxAdmins || len(p.rules) > maxRules {
			t.Fatalf("limits not kept: %d admins, %d rules", len(p.admins), len(p.rules))
		}
		for _, r := range p.rules {
			for _, pattern := range r.paths {
				if validatePattern(pattern) != nil {
					t.Fatalf("invalid pattern %q accepted", pattern)
				}
			}
		}
	})
}
