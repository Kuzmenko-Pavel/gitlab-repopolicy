package module

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRefScope(t *testing.T) {
	tests := []struct {
		in   any
		want RefScope
	}{
		{nil, RefScope{Kind: RefsAll}},
		{"all", RefScope{Kind: RefsAll}},
		{"default", RefScope{Kind: RefsDefault}},
		{"protected", RefScope{Kind: RefsProtected}},
		{[]any{"refs/heads/release/*", "refs/tags/v*"}, RefScope{Kind: RefsList, List: []string{"refs/heads/release/*", "refs/tags/v*"}}},
	}
	for _, tt := range tests {
		got, err := ParseRefScope(tt.in)
		require.NoError(t, err, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
	}
	for want, in := range map[string]any{
		`"main" is not one of all, default, protected or a list of refs`: "main",
		"the list is empty": []any{},
		"heads/main is not a ref pattern such as refs/heads/release/*": []any{"heads/main"},
		"7 is not a ref pattern such as refs/heads/release/*":          []any{7},
		`"refs/heads/[": syntax error in pattern`:                      []any{"refs/heads/["},
		"expected all, default, protected or a list of refs":           map[string]any{},
	} {
		_, err := ParseRefScope(in)
		assert.EqualError(t, err, want)
	}
}

func TestRefScope_MatchListAndString(t *testing.T) {
	s, err := ParseRefScope([]any{"refs/heads/release/*"})
	require.NoError(t, err)
	assert.True(t, s.MatchList("refs/heads/release/1.0"))
	assert.False(t, s.MatchList("refs/heads/release/1.0/x"), "* does not cross /")
	assert.False(t, s.MatchList("refs/heads/main"))
	assert.Equal(t, "refs/heads/release/*", s.String())
	assert.Equal(t, "protected", RefScope{Kind: RefsProtected}.String())
	assert.Equal(t, "all", RefScope{}.String())
}

func TestValidRuleName(t *testing.T) {
	assert.True(t, ValidRuleName("ci-1"))
	assert.False(t, ValidRuleName("CI"))
	assert.False(t, ValidRuleName(""))
	assert.True(t, ValidRuleName(strings.Repeat("a", 63)))
	assert.False(t, ValidRuleName(strings.Repeat("a", 64)))
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"main", "main", true},
		{"main", "main2", false},
		{"release/*", "release/1", true},
		{"release/*", "release/1/hotfix", true},
		{"release/*", "release", false},
		{"*", "feature/x", true},
		{"*-stable", "v1/2-stable", true},
		{"*-stable", "stable", false},
		{"v*", "v1.0.0", true},
		{"v*.*", "v1", false},
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "acb", false},
		{"a*a", "a", false},
		{"a*b", "ab", true},
		{"**", "x", true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, MatchPattern(tt.pattern, tt.name), "%s ~ %s", tt.pattern, tt.name)
	}
}
