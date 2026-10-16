package module

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRefUpdate_Kind(t *testing.T) {
	z40, z64 := strings.Repeat("0", 40), strings.Repeat("0", 64)
	a40, a64 := strings.Repeat("a", 40), strings.Repeat("a", 64)
	assert.Equal(t, Create, RefUpdate{Old: z40, New: a40}.Kind())
	assert.Equal(t, Delete, RefUpdate{Old: a40, New: z40}.Kind())
	assert.Equal(t, Update, RefUpdate{Old: a40, New: a40}.Kind())
	assert.Equal(t, Create, RefUpdate{Old: z64, New: a64}.Kind(), "SHA-256 null revision")
	assert.False(t, IsZero(""))
	assert.True(t, RefUpdate{Ref: "refs/heads/main"}.IsBranch())
	assert.True(t, RefUpdate{Ref: "refs/tags/v1"}.IsTag())
	assert.False(t, RefUpdate{Ref: "refs/replace/abc"}.IsBranch())
}

func TestPrincipal_Kind(t *testing.T) {
	tests := []struct {
		glid string
		kind PrincipalKind
		id   int64
	}{
		{"user-314", PrincipalUser, 314},
		{"key-7", PrincipalKey, 7},
		{"user-0", PrincipalUnknown, 0},
		{"user-007", PrincipalUnknown, 0},
		{"user--1", PrincipalUnknown, 0},
		{"user-", PrincipalUnknown, 0},
		{"deploy-1", PrincipalUnknown, 0},
		{"", PrincipalUnknown, 0},
		{"user-99999999999999999999", PrincipalUnknown, 0},
	}
	for _, tt := range tests {
		kind, id := Principal{GLID: tt.glid}.Kind()
		assert.Equal(t, tt.kind, kind, tt.glid)
		assert.Equal(t, tt.id, id, tt.glid)
	}
}

func TestStatus(t *testing.T) {
	assert.True(t, Confirmed().IsConfirmed())
	s := Unknown("timeout")
	assert.False(t, s.IsConfirmed())
	assert.Equal(t, "timeout", s.Reason())
	assert.False(t, s.IsLimit())
	l := UnknownLimit("budget")
	assert.True(t, l.IsLimit())
	assert.False(t, l.IsConfirmed())
	var zero Status
	assert.False(t, zero.IsConfirmed(), "zero status is unknown")
	assert.Equal(t, "no answer", zero.Reason())
	assert.Equal(t, "deny", Deny.String())
	assert.Equal(t, "protected", ClassProtected.String())
}

func TestValidUsername(t *testing.T) {
	for _, ok := range []string{"a", "_", "alice", "Alice.Smith", "dev-lead", ".svc", "a_b.c-d", "project_1_bot_e42782", strings.Repeat("a", 256)} {
		assert.True(t, ValidUsername(ok), ok)
	}
	for _, bad := range []string{"", "-a", "a.", ".", "-", "group/sub", "@alice", "a b", "repo.git", "feed.atom", "ядро", strings.Repeat("a", 257)} {
		assert.False(t, ValidUsername(bad), bad)
	}
}
