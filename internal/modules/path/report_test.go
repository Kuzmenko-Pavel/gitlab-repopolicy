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
  - name: ci
    paths: [".gitlab-ci.yml", "ci/**"]
    allow: [platform-lead, Alice]
  - name: release-ci
    paths: ["ci/release/**"]
    allow: [sre-lead]
    refs: protected
  - name: main-only
    paths: ["ci/**"]
    allow: [alice]
    refs: default
  - name: listed
    paths: ["ci/**"]
    allow: [alice]
    refs: [refs/heads/release/*]
`

func parsed(t *testing.T, text string) *Policy {
	t.Helper()
	p, err := New().Parse([]byte(text))
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
	ctx := context.Background()
	refs := &fakeRefs{class: module.ClassOther, status: ok}

	got, st := p.ExplainPath(ctx, "ci/release/deploy.yml", "refs/heads/feature", "alice", refs)
	require.True(t, st.IsConfirmed())
	assert.Equal(t, []PathRule{
		{Name: "ci", Refs: "all", Applies: true, Allow: []string{"platform-lead", "Alice"}, Allowed: true},
		{Name: "release-ci", Refs: "protected", Applies: false, Allow: []string{"sre-lead"}},
		{Name: "main-only", Refs: "default", Applies: false, Allow: []string{"alice"}, Allowed: true},
		{Name: "listed", Refs: "refs/heads/release/*", Applies: false, Allow: []string{"alice"}, Allowed: true},
	}, got)

	got, st = p.ExplainPath(ctx, "ci/release/deploy.yml", "refs/heads/main", "alice", refs)
	require.True(t, st.IsConfirmed())
	assert.True(t, got[1].Applies, "the default branch is protected")
	assert.True(t, got[2].Applies)
	got, _ = p.ExplainPath(ctx, "ci/release/deploy.yml", "refs/heads/release/1", "alice", &fakeRefs{class: module.ClassProtected, status: ok})
	assert.True(t, got[1].Applies)
	assert.True(t, got[3].Applies)

	got, _ = p.ExplainPath(ctx, "README.md", "refs/heads/main", "alice", refs)
	assert.Empty(t, got)
	got, _ = p.ExplainPath(ctx, File, "refs/heads/main", "Platform-Lead", refs)
	require.Len(t, got, 1)
	assert.Equal(t, PathRule{Name: "admins of PATHPOLICY", Refs: "all", Applies: true,
		Allow: []string{"platform-lead", "ghost"}, Allowed: true}, got[0])

	_, st = p.ExplainPath(ctx, "ci/release/x", "refs/heads/feature", "alice", &fakeRefs{status: module.Unknown("api down")})
	assert.False(t, st.IsConfirmed())
}
