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
  - name: ci-config
    paths: [".gitlab-ci.yml", ".gitlab/ci/**", "Makefile", "scripts/deploy/**"]
    allow: [platform-lead, sre-lead, ci-maintainer]
    message: >-
      CI/CD configuration is changed by the platform team.
  - name: commit-policy
    paths: [COMMITPOLICY]
    allow: [dev-lead]
  - name: prod-values
    paths: ["helm/**/values-prod.yaml"]
    refs: protected
    allow: [platform-lead, release-manager]
`

func TestParse(t *testing.T) {
	p, err := Parse([]byte(basePolicy))
	require.NoError(t, err)
	assert.Equal(t, []string{"platform-lead", "sre-lead"}, p.admins)
	require.Len(t, p.rules, 3)
	assert.Equal(t, module.RefsAll, p.rules[0].refs.Kind)
	assert.Equal(t, module.RefsProtected, p.rules[2].refs.Kind)
	assert.True(t, New().Covers(p, "COMMITPOLICY"))
	assert.False(t, New().Covers(p, "TAGPOLICY"))
	assert.Len(t, p.Usernames(), 8)
	_, err = Parse([]byte("version: 1\nadmins: [a]\n"))
	assert.NoError(t, err, "a policy may only guard itself")
}

func TestParse_GroupsAndRolesRejected(t *testing.T) {
	for _, principal := range []string{"group/subgroup", "@maintainers", "role:maintainer", "@alice"} {
		_, err := Parse([]byte(fmt.Sprintf("version: 1\nadmins: [a]\nrules:\n  - name: r\n    paths: [x]\n    allow: [%q]\n", principal)))
		require.Error(t, err, principal)
		assert.Contains(t, err.Error(), "groups and roles are not supported", principal)
	}
	_, err := Parse([]byte("version: 1\nadmins: [\"group/admins\"]\n"))
	assert.ErrorContains(t, err, "groups and roles are not supported")
}

func TestParse_AdminsLimit(t *testing.T) {
	_, err := Parse([]byte("version: 1\nadmins: [a, b, c, d, e, f]\n"))
	assert.ErrorContains(t, err, "admins: 1 to 5 usernames are required")
	_, err = Parse([]byte("version: 1\nadmins: []\n"))
	assert.ErrorContains(t, err, "admins: 1 to 5")
	_, err = Parse([]byte("version: 1\nadmins: [Alice, alice]\n"))
	assert.ErrorContains(t, err, "listed twice")
}

func TestParse_Limits(t *testing.T) {
	rules := func(n int) string {
		var b strings.Builder
		b.WriteString("version: 1\nadmins: [a]\nrules:\n")
		for i := range n {
			fmt.Fprintf(&b, "  - {name: r%d, paths: [x], allow: [a]}\n", i)
		}
		return b.String()
	}
	_, err := Parse([]byte(rules(100)))
	require.NoError(t, err)
	_, err = Parse([]byte(rules(101)))
	assert.ErrorContains(t, err, "101 rules, the limit is 100")

	list := func(n int, prefix string) string {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf("%s%d", prefix, i)
		}
		return "[" + strings.Join(items, ", ") + "]"
	}
	_, err = Parse([]byte("version: 1\nadmins: [a]\nrules:\n  - {name: r, paths: " + list(51, "p") + ", allow: [a]}\n"))
	assert.ErrorContains(t, err, "paths: 1 to 50 patterns")
	_, err = Parse([]byte("version: 1\nadmins: [a]\nrules:\n  - {name: r, paths: [x], allow: " + list(51, "u") + "}\n"))
	assert.ErrorContains(t, err, "allow: 1 to 50 usernames")
	_, err = Parse([]byte("version: 1\nadmins: [a]\nrules:\n  - {name: r, paths: [x], allow: []}\n"))
	assert.ErrorContains(t, err, "allow: 1 to 50 usernames", "an empty allow would freeze the paths")
	_, err = Parse([]byte("version: 1\nadmins: [a]\nrules:\n  - {name: r, paths: [x], allow: [a], message: " + strings.Repeat("m", 1001) + "}\n"))
	assert.ErrorContains(t, err, "message: 1001 characters")
	_, err = Parse([]byte("version: 1\nadmins: [a]\nrules:\n  - {name: r, paths: [x], allow: [a]}\n  - {name: r, paths: [y], allow: [a]}\n"))
	assert.ErrorContains(t, err, "used twice")
	for in, want := range map[string]string{
		"version: 2\nadmins: [a]\n":                                                              "version: 2",
		"version: 1\nadmins: [a]\ndeny: [x]\n":                                                   `unknown field "deny"`,
		"version: 1\nadmins: [a]\nrules: [{name: R, paths: [x], allow: [a]}]\n":                  `name: "R" must match`,
		"version: 1\nadmins: [a]\nrules: [{name: r, paths: [x], allow: [a], refs: feature}]\n":   `refs: "feature"`,
		"version: 1\nadmins: [a]\nrules: [{name: r, paths: [x], allow: [a], refs: [heads/x]}]\n": "not a ref pattern",
		"version: 1\nadmins: [a]\nrules: [{name: r, paths: [x], allow: [a], help_url: y}]\n":     `rule r: unknown field "help_url"`,
		"version: 1\nadmins: [a]\nrules: [{name: r, paths: [/abs], allow: [a]}]\n":               "remove the leading /",
		"version: 1\nadmins: &x [a]\n":                                                           "anchors are not allowed",
	} {
		_, err := Parse([]byte(in))
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
	f.Add([]byte("version: 1\nadmins: [alice]\nrules:\n  - name: ci\n    paths: [ci.yml, \"deploy/**\"]\n    refs: [\"refs/heads/release/*\"]\n    allow: [alice]\n"))
	f.Add([]byte("version: 1\nadmins: [alice]\nrules:\n  - name: x\n    paths: [\"{a,b}\"]\n    allow: [group/x]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data)
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
