package module

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMatch_Values(t *testing.T) {
	m, err := ParseMatch("all", false)
	require.NoError(t, err)
	assert.True(t, m.All)
	assert.False(t, m.IsList())
	assert.Equal(t, "all", m.String())

	m, err = ParseMatch("default", true)
	require.NoError(t, err)
	assert.True(t, m.Default)
	assert.Equal(t, "default", m.String())

	_, err = ParseMatch("default", false)
	assert.EqualError(t, err, "default is not allowed here; use all or a list of names")
}

func TestMatch_Matches(t *testing.T) {
	tests := []struct {
		match []any
		yes   []string
		no    []string
	}{
		{[]any{"main"}, []string{"main"}, []string{"main2", "x/main"}},
		{[]any{`/^v\d+$/`}, []string{"v1", "v20"}, []string{"v1.0", "build-v2"}},
		{[]any{`/v\d+/`}, []string{"v1", "build-v2", "v1.0"}, []string{"main"}},
		{[]any{"/issue/"}, []string{"severe-issues"}, []string{"Issue"}},
		{[]any{"/issue/i"}, []string{"Issue-7", "issue"}, []string{"main"}},
		{[]any{"main", `/^release\//`}, []string{"main", "release/1.0", "release/1.0/x"}, []string{"releases"}},
		{[]any{`/^release\//`, "!release/old"}, []string{"release/1"}, []string{"release/old", "main"}},
		{[]any{"!main"}, []string{"feature", "release/1"}, []string{"main"}},
		{[]any{"!main", "!/^tmp-/"}, []string{"feature"}, []string{"main", "tmp-1"}},
		{[]any{"/^!name$/"}, []string{"!name"}, []string{"name"}},
	}
	for _, tt := range tests {
		m, err := ParseMatch(tt.match, false)
		require.NoError(t, err, tt.match)
		for _, n := range tt.yes {
			assert.True(t, m.Matches(n), "%v must match %s", tt.match, n)
		}
		for _, n := range tt.no {
			assert.False(t, m.Matches(n), "%v must not match %s", tt.match, n)
		}
	}
	all, err := ParseMatch("all", false)
	require.NoError(t, err)
	assert.False(t, all.Matches("main"), "the module resolves all itself")
}

func TestParseMatch_Errors(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, "match is required; write match: all for every name"},
		{"main", `"main" is not all or a list; write a single name as [main]`},
		{[]any{}, "the list is empty"},
		{[]any{7}, "7 is not a string"},
		{[]any{"all"}, "all is a value of match, not an element of a list; write match: all"},
		{[]any{"main", "default"}, "default is a value of match, not an element of a list; write match: default"},
		{[]any{"!"}, `"!": ! must be followed by a name or a regex`},
		{[]any{"/(/"}, "\"/(/\": not a valid RE2 regex: error parsing regexp: missing closing ): `(`"},
		{[]any{"/x/g"}, `"/x/g": flag "g" is not supported; the only flag is i`},
		{[]any{"//"}, `"//": the regex is empty`},
		{[]any{"/x"}, `"/x": a regex is written as /re/ or /re/i`},
		{[]any{`/(?=x)/`}, "\"/(?=x)/\": not a valid RE2 regex: error parsing regexp: invalid or unsupported Perl syntax: `(?=`"},
		{[]any{"release/*"}, `"release/*": a glob is not supported; use a regex such as /^release\//`},
		{[]any{`v\d+`}, `"v\\d+": not a branch or tag name; did you mean the regex /v\d+/?`},
		{[]any{"refs/heads/main"}, `"refs/heads/main": write the branch or tag name without refs/heads/ or refs/tags/`},
		{[]any{"a b"}, `"a b": ' ' is not allowed in a branch or tag name`},
		{[]any{"a:b"}, `"a:b": ':' is not allowed in a branch or tag name`},
		{[]any{"a..b"}, `"a..b": not a valid branch or tag name`},
		{[]any{"a.lock"}, `"a.lock": not a valid branch or tag name`},
		{[]any{"x/.hidden"}, `"x/.hidden": not a valid branch or tag name`},
		{map[string]any{}, "expected all, a list of names, or default where it is allowed"},
	}
	for _, tt := range tests {
		_, err := ParseMatch(tt.in, true)
		if tt.want == `"main" is not all or a list; write a single name as [main]` {
			_, err = ParseMatch(tt.in, false)
		}
		assert.EqualError(t, err, tt.want, tt.in)
	}
}

func TestMatch_SameAndString(t *testing.T) {
	a, err := ParseMatch([]any{"main", `/^release\//`}, false)
	require.NoError(t, err)
	b, err := ParseMatch([]any{`/^release\//`, "main", "main"}, false)
	require.NoError(t, err)
	c, err := ParseMatch([]any{"main"}, false)
	require.NoError(t, err)
	all, err := ParseMatch("all", false)
	require.NoError(t, err)
	assert.True(t, a.Same(b))
	assert.False(t, a.Same(c))
	assert.False(t, a.Same(all))
	assert.True(t, all.Same(Match{All: true}))
	assert.Equal(t, `[main, /^release\//]`, a.String())
}

func TestParsePattern(t *testing.T) {
	p, err := ParsePattern(`/#[0-9]+/`)
	require.NoError(t, err)
	assert.True(t, p.Found("Refs: #12"))
	assert.False(t, p.Negated())

	p, err = ParsePattern(`!/^(WIP|fixup!)/i`)
	require.NoError(t, err)
	assert.True(t, p.Negated())
	assert.True(t, p.Found("wip: x"))
	assert.Equal(t, `!/^(WIP|fixup!)/i`, p.String())

	_, err = ParsePattern("PAY-1")
	assert.EqualError(t, err, `"PAY-1": write a regex as /re/, /re/i, !/re/ or !/re/i`)
	_, err = ParsePattern("/x/m")
	assert.EqualError(t, err, `"/x/m": flag "m" is not supported; the only flag is i`)
}
