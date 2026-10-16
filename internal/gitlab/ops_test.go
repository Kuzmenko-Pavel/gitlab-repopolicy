package gitlab_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
)

func TestOps_UserAndToken(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, "/api/v4/user", gitlabtest.OK(map[string]any{"id": 7, "username": "repopolicy", "bot": true, "external": true}))
	srv.Handle(http.MethodGet, "/api/v4/personal_access_tokens/self", gitlabtest.OK(map[string]any{
		"id": 3, "scopes": []string{"read_api", "self_rotate"}, "active": true, "expires_at": "2027-10-13", "user_id": 7}))
	srv.Handle(http.MethodPost, "/api/v4/personal_access_tokens/self/rotate",
		gitlabtest.OK(map[string]any{"id": 4, "token": "glpat-new", "expires_at": "2027-10-14"}),
		gitlabtest.Response{Status: http.StatusBadRequest, Body: map[string]any{"message": "400 Bad request - Expiration date must be before 2027-10-15"}},
		gitlabtest.OK(map[string]any{"id": 5}))
	c := newClient(t, srv, writeToken(t, "glpat-old"), 100)
	ctx := context.Background()

	u, err := c.CurrentUser(ctx)
	require.NoError(t, err)
	assert.Equal(t, gitlab.TokenUser{ID: 7, Username: "repopolicy", Bot: true, External: true}, u)
	tok, err := c.TokenSelf(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"read_api", "self_rotate"}, tok.Scopes)
	assert.Equal(t, "2027-10-13", tok.ExpiresAt)

	secret, info, err := c.RotateToken(ctx, "2027-10-14")
	require.NoError(t, err)
	assert.Equal(t, "glpat-new", secret)
	assert.Equal(t, "2027-10-14", info.ExpiresAt)
	assert.Equal(t, []string{`{"expires_at":"2027-10-14"}`}, srv.Bodies(http.MethodPost, "/api/v4/personal_access_tokens/self/rotate"))

	_, _, err = c.RotateToken(ctx, "2099-01-01")
	var apiErr *gitlab.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Status)
	assert.Contains(t, err.Error(), "Expiration date must be before 2027-10-15", "the GitLab message is kept for audit")

	_, _, err = c.RotateToken(ctx, "2027-10-14")
	assert.ErrorContains(t, err, "no token in the response")
}

func TestOps_Projects(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	full := make([]map[string]any, 100)
	for i := range full {
		full[i] = map[string]any{"id": i + 1, "path_with_namespace": fmt.Sprintf("g/p%d", i+1)}
	}
	full[1]["ci_config_path"] = nil
	srv.Handle(http.MethodGet, "/api/v4/projects?membership=true&archived=false&order_by=id&sort=asc&per_page=100&id_after=0", gitlabtest.OK(full))
	srv.Handle(http.MethodGet, "/api/v4/projects?membership=true&archived=false&order_by=id&sort=asc&per_page=100&id_after=100",
		gitlabtest.OK([]map[string]any{{"id": 101, "path_with_namespace": "g/last", "default_branch": "main",
			"ci_config_path": "ci/main.yml", "ci_push_repository_for_job_token_allowed": false,
			"namespace":   map[string]any{"kind": "group"},
			"permissions": map[string]any{"project_access": map[string]any{"access_level": 30}, "group_access": map[string]any{"access_level": 40}}}}))
	srv.Handle(http.MethodGet, "/api/v4/projects/acme%2Fapp", gitlabtest.OK(map[string]any{"id": 9, "default_branch": "main"}))
	srv.Handle(http.MethodGet, "/api/v4/projects/9/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.OK("version: 1\n"))
	srv.Handle(http.MethodGet, "/api/v4/projects/9/repository/files/BIG/raw?ref=main", gitlabtest.OK(strings.Repeat("x", 20)))
	srv.Handle(http.MethodHead, "/api/v4/projects/9/repository/files/dir%2FTAGPOLICY?ref=release%2F1", gitlabtest.Status(http.StatusOK))
	c := newClient(t, srv, writeToken(t, "glpat-1"), 100)
	ctx := context.Background()

	projects, err := c.MemberProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 101)
	last := projects[100]
	assert.Equal(t, 40, last.Access())
	assert.Equal(t, gitlab.Field[string]{Present: true, Value: "ci/main.yml"}, last.CIConfigPath)
	assert.Equal(t, gitlab.Field[bool]{Present: true}, last.JobTokenPush)
	assert.Equal(t, "group", last.Namespace.Kind)
	assert.Equal(t, gitlab.Field[string]{Present: true}, projects[1].CIConfigPath, "null: never set")
	assert.False(t, projects[0].CIConfigPath.Present, "absent without membership")

	p, err := c.Project(ctx, "acme/app")
	require.NoError(t, err)
	assert.Equal(t, int64(9), p.ID)
	data, err := c.RawFile(ctx, 9, "PATHPOLICY", "main", 100)
	require.NoError(t, err)
	assert.Equal(t, "version: 1\n", string(data), "the raw body, not JSON")
	_, err = c.RawFile(ctx, 9, "BIG", "main", 10)
	assert.ErrorContains(t, err, "larger than 10 bytes")
	_, err = c.RawFile(ctx, 9, "COMMITPOLICY", "main", 100)
	assert.Equal(t, gitlab.NotFound, gitlab.KindOf(err))

	ok, err := c.FileExists(ctx, 9, "dir/TAGPOLICY", "release/1")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = c.FileExists(ctx, 9, "PATHPOLICY", "main")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestOps_Groups(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, "/api/v4/groups?top_level_only=true&all_available=true&order_by=id&sort=asc&per_page=100&page=1",
		gitlabtest.OK([]map[string]any{{"id": 1, "full_path": "acme"}, {"id": 2, "full_path": "beta"}}))
	srv.Handle(http.MethodGet, "/api/v4/groups/1/members/7", gitlabtest.OK(map[string]any{"access_level": 40, "state": "active"}))
	srv.Handle(http.MethodGet, "/api/v4/groups/3/members/7", gitlabtest.OK(map[string]any{"access_level": 40, "state": "blocked"}))
	srv.Handle(http.MethodGet, "/api/v4/groups/2/projects?include_subgroups=true&archived=false&order_by=id&sort=asc&per_page=100&page=1",
		gitlabtest.OK([]map[string]any{{"id": 5, "path_with_namespace": "beta/sub/app", "default_branch": "main"}}))
	c := newClient(t, srv, writeToken(t, "glpat-admin"), 100)
	ctx := context.Background()

	groups, err := c.TopLevelGroups(ctx)
	require.NoError(t, err)
	assert.Equal(t, []gitlab.Group{{ID: 1, FullPath: "acme"}, {ID: 2, FullPath: "beta"}}, groups)
	for id, want := range map[int64]int{1: 40, 2: 0, 3: 0} {
		level, err := c.GroupMemberAccess(ctx, id, 7)
		require.NoError(t, err)
		assert.Equal(t, want, level, "group %d", id)
	}
	projects, err := c.GroupProjects(ctx, 2)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "beta/sub/app", projects[0].PathWithNamespace)

	srv.Handle(http.MethodGet, "/api/v4/groups/9/members/7", gitlabtest.Response{Status: http.StatusForbidden,
		Body: map[string]any{"error": "insufficient_scope"}})
	_, err = c.GroupMemberAccess(ctx, 9, 7)
	assert.Equal(t, gitlab.Unauthorized, gitlab.KindOf(err))
	assert.ErrorContains(t, err, "insufficient_scope")
}
