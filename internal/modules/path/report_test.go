package path

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

const explainPolicy = `version: 1
admins: [platform-lead, ghost]
rules:
  ci:
    match: all
    paths: [".gitlab-ci.yml", "ci/**"]
    allow: [platform-lead, Alice]
    message: m
  main-only:
    match: default
    paths: ["ci/**"]
    allow: [alice]
    message: m
  listed:
    match: ['/^release\//']
    paths: ["ci/**"]
    allow: [alice]
    message: m
`

func parsed(t *testing.T, text string) *Policy {
	t.Helper()
	p, err := New(nil).Parse([]byte(text))
	require.NoError(t, err)
	return p.(*Policy)
}

func TestAdminReport(t *testing.T) {
	p := parsed(t, explainPolicy)
	a := team()
	report, st := p.AdminReport(context.Background(), a)
	require.True(t, st.IsConfirmed())
	assert.Equal(t, []Admin{
		{Username: "platform-lead", Exists: true, Active: true, Access: 40, Effective: true},
		{Username: "ghost"},
	}, report)

	a.users["ghost"] = module.User{ID: 4, Active: false}
	report, st = p.AdminReport(context.Background(), a)
	require.True(t, st.IsConfirmed())
	assert.Equal(t, Admin{Username: "ghost", Exists: true}, report[1], "blocked: exists but not active")

	a.levelsSt = module.Unknown("timeout")
	_, st = p.AdminReport(context.Background(), a)
	assert.False(t, st.IsConfirmed(), "a failure is never an empty report")
}

func TestExplainPath(t *testing.T) {
	p := parsed(t, explainPolicy)
	const main = "refs/heads/main"
	file := []string{"file"}

	got := p.ExplainPath("ci/release/deploy.yml", "refs/heads/feature", main, "alice")
	require.NotEmpty(t, got)
	assert.Equal(t, module.FieldOrigin{Field: "allow", Layers: file}, got[0].Fields[2])
	for i := range got {
		got[i].Fields = nil
	}
	assert.Equal(t, []PathRule{
		{Name: "ci", Match: "all", Applies: true, Allow: []string{"platform-lead", "Alice"}, Allowed: true, Source: file},
		{Name: "main-only", Match: "default", Applies: false, Allow: []string{"alice"}, Allowed: true, Source: file},
		{Name: "listed", Match: `[/^release\//]`, Applies: false, Allow: []string{"alice"}, Allowed: true, Source: file},
	}, got)

	got = p.ExplainPath("ci/release/deploy.yml", main, main, "alice")
	assert.True(t, got[1].Applies, "default is the default branch")
	got = p.ExplainPath("ci/release/deploy.yml", "refs/heads/release/1", main, "alice")
	assert.True(t, got[2].Applies, "a branch of the list")
	got = p.ExplainPath("ci/x", "refs/tags/v1", main, "alice")
	assert.False(t, got[0].Applies, "PATHPOLICY does not check tags")

	got = p.ExplainPath("README.md", main, main, "alice")
	assert.Empty(t, got)
	got = p.ExplainPath(File, main, main, "Platform-Lead")
	require.Len(t, got, 1)
	assert.Equal(t, PathRule{Name: "admins of PATHPOLICY", Match: "all", Applies: true,
		Allow: []string{"platform-lead", "ghost"}, Allowed: true, Source: file}, got[0])
}
