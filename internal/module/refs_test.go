package module

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
