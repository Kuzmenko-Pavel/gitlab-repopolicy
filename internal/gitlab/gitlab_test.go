package gitlab_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
)

const branchesPath = "/api/v4/projects/1/protected_branches?per_page=100&page=1"

func writeToken(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte(value+"\n"), 0o600))
	return path
}

func newClient(t *testing.T, srv *gitlabtest.Server, tokenFile string, maxCalls int) *gitlab.Client {
	t.Helper()
	opts := gitlab.Options{Socket: srv.Socket(), BaseURL: srv.BaseURL(), TokenFile: tokenFile,
		Timeout: 200 * time.Millisecond, MaxCalls: maxCalls, UserAgent: "gitlab-repopolicy/test"}
	if srv.Socket() == "" {
		opts.CAFile = srv.WriteCA(t.TempDir())
	}
	c, err := gitlab.New(opts)
	require.NoError(t, err)
	return c
}

func TestClient_UnixAndTLS(t *testing.T) {
	for name, srv := range map[string]*gitlabtest.Server{"unix": gitlabtest.NewUnix(t), "tls": gitlabtest.NewTLS(t)} {
		t.Run(name, func(t *testing.T) {
			srv.Handle(http.MethodGet, branchesPath, gitlabtest.OK([]map[string]any{{"name": "main"}, {"name": "release/*"}}))
			c := newClient(t, srv, writeToken(t, "glpat-1"), 30)
			got, err := c.ProtectedBranches(context.Background(), 1)
			require.NoError(t, err)
			assert.Equal(t, []string{"main", "release/*"}, got)
			assert.Equal(t, []string{"glpat-1"}, srv.Tokens())
			assert.Equal(t, 1, c.Calls())
		})
	}
}

func TestClient_Errors(t *testing.T) {
	tests := []struct {
		name string
		resp gitlabtest.Response
		want gitlab.ErrorKind
	}{
		{"404", gitlabtest.Status(http.StatusNotFound), gitlab.NotFound},
		{"401", gitlabtest.Status(http.StatusUnauthorized), gitlab.Unauthorized},
		{"403", gitlabtest.Status(http.StatusForbidden), gitlab.Unauthorized},
		{"429", gitlabtest.Status(http.StatusTooManyRequests), gitlab.Unavailable},
		{"500", gitlabtest.Status(http.StatusInternalServerError), gitlab.Unavailable},
		{"400", gitlabtest.Status(http.StatusBadRequest), gitlab.Unexpected},
		{"302", gitlabtest.Status(http.StatusFound), gitlab.Unexpected},
		{"timeout", gitlabtest.Response{Status: 200, Body: "[]", Delay: time.Second}, gitlab.Unavailable},
		{"connection dropped", gitlabtest.Response{Drop: true}, gitlab.Unavailable},
		{"malformed body", gitlabtest.OK("{"), gitlab.Unexpected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := gitlabtest.NewUnix(t)
			srv.Handle(http.MethodGet, branchesPath, tt.resp)
			_, err := newClient(t, srv, writeToken(t, "glpat-1"), 30).ProtectedBranches(context.Background(), 1)
			require.Error(t, err)
			assert.Equal(t, tt.want, gitlab.KindOf(err), err.Error())
		})
	}
}

func TestClient_RereadsTokenOn401(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, branchesPath, gitlabtest.Status(http.StatusUnauthorized), gitlabtest.OK([]map[string]any{{"name": "main"}}))
	tokenFile := writeToken(t, "glpat-old")
	c := newClient(t, srv, tokenFile, 30)
	_, err := c.ProtectedTags(context.Background(), 1) // reads the token, 404 not scripted
	require.Error(t, err)
	require.NoError(t, os.WriteFile(tokenFile, []byte("glpat-new"), 0o600))
	got, err := c.ProtectedBranches(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, got)
	assert.Equal(t, []string{"glpat-old", "glpat-old", "glpat-new"}, srv.Tokens())
}

func TestClient_401WithSameTokenIsNotRetried(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, branchesPath, gitlabtest.Status(http.StatusUnauthorized))
	_, err := newClient(t, srv, writeToken(t, "glpat-1"), 30).ProtectedBranches(context.Background(), 1)
	assert.Equal(t, gitlab.Unauthorized, gitlab.KindOf(err))
	assert.Equal(t, 1, srv.TotalCalls())
}

func TestClient_TokenFileMissing(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	_, err := newClient(t, srv, filepath.Join(t.TempDir(), "none"), 30).ProtectedBranches(context.Background(), 1)
	assert.Equal(t, gitlab.Unauthorized, gitlab.KindOf(err))
	assert.Zero(t, srv.TotalCalls())
}

func TestClient_Budget(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, branchesPath, gitlabtest.OK([]map[string]any{}))
	c := newClient(t, srv, writeToken(t, "glpat-1"), 2)
	for range 2 {
		_, err := c.ProtectedBranches(context.Background(), 1)
		require.NoError(t, err)
	}
	_, err := c.ProtectedBranches(context.Background(), 1)
	assert.Equal(t, gitlab.BudgetExceeded, gitlab.KindOf(err))
	assert.Equal(t, 2, srv.TotalCalls())
}

func TestClient_Deadline(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newClient(t, srv, writeToken(t, "glpat-1"), 30).ProtectedBranches(ctx, 1)
	assert.Equal(t, gitlab.Deadline, gitlab.KindOf(err))
	assert.Zero(t, srv.TotalCalls())
}

func TestClient_Pagination(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	page := make([]map[string]any, 100)
	for i := range page {
		page[i] = map[string]any{"name": fmt.Sprintf("b%d", i)}
	}
	srv.Handle(http.MethodGet, "/api/v4/projects/1/protected_tags?per_page=100&page=1", gitlabtest.OK(page))
	srv.Handle(http.MethodGet, "/api/v4/projects/1/protected_tags?per_page=100&page=2", gitlabtest.OK([]map[string]any{{"name": "v*"}}))
	got, err := newClient(t, srv, writeToken(t, "glpat-1"), 30).ProtectedTags(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, got, 101)
	assert.Equal(t, "v*", got[100])
}

func TestClient_UsersByUsername(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, "/api/v4/users?username=a%2Bb", gitlabtest.OK([]map[string]any{{"id": 7, "username": "a+b", "state": "active"}}))
	got, err := newClient(t, srv, writeToken(t, "glpat-1"), 30).UsersByUsername(context.Background(), "a+b")
	require.NoError(t, err)
	assert.Equal(t, []gitlab.User{{ID: 7, Username: "a+b", State: "active"}}, got)
}

func TestNew_Errors(t *testing.T) {
	_, err := gitlab.New(gitlab.Options{BaseURL: "https://x", CAFile: "/nonexistent", Timeout: time.Second, MaxCalls: 1})
	assert.ErrorContains(t, err, "ca_file")
	bad := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(bad, []byte("not a cert"), 0o600))
	_, err = gitlab.New(gitlab.Options{BaseURL: "https://x", CAFile: bad, Timeout: time.Second, MaxCalls: 1})
	assert.ErrorContains(t, err, "no certificates")
	_, err = gitlab.New(gitlab.Options{BaseURL: "https://x", Timeout: time.Second})
	assert.ErrorContains(t, err, "MaxCalls")
}

func TestClient_UserHasKey(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, "/api/v4/users/2/keys/7", gitlabtest.OK(map[string]any{"id": 7}))
	srv.Handle(http.MethodGet, "/api/v4/users/2/keys/8", gitlabtest.Status(http.StatusNotFound))
	srv.Handle(http.MethodGet, "/api/v4/users/2/keys/9", gitlabtest.Status(http.StatusBadGateway))
	c := newClient(t, srv, writeToken(t, "glpat-1"), 30)
	ok, err := c.UserHasKey(context.Background(), 2, 7)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = c.UserHasKey(context.Background(), 2, 8)
	require.NoError(t, err)
	assert.False(t, ok, "404: not one of the user's keys, a deploy key")
	_, err = c.UserHasKey(context.Background(), 2, 9)
	assert.Equal(t, gitlab.Unavailable, gitlab.KindOf(err))
}

func TestClient_ProjectAccess(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, "/api/v4/projects/1", gitlabtest.OK(map[string]any{"permissions": map[string]any{
		"project_access": nil, "group_access": map[string]any{"access_level": 40}}}))
	srv.Handle(http.MethodGet, "/api/v4/projects/2", gitlabtest.OK(map[string]any{"permissions": map[string]any{
		"project_access": map[string]any{"access_level": 50}, "group_access": map[string]any{"access_level": 30}}}))
	srv.Handle(http.MethodGet, "/api/v4/projects/3", gitlabtest.OK(map[string]any{"permissions": map[string]any{}}))
	c := newClient(t, srv, writeToken(t, "glpat-1"), 30)
	for id, want := range map[int64]int{1: 40, 2: 50, 3: 0} {
		got, err := c.ProjectAccess(context.Background(), id)
		require.NoError(t, err)
		assert.Equal(t, want, got, "project %d", id)
	}
	_, err := c.ProjectAccess(context.Background(), 4)
	assert.Equal(t, gitlab.NotFound, gitlab.KindOf(err), "a private project without membership is 404")
}

func TestClient_UsersByUsernames(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodPost, "/api/graphql", gitlabtest.OK(map[string]any{"data": map[string]any{"users": map[string]any{"nodes": []any{
		map[string]any{"id": "gid://gitlab/User/2", "username": "alice", "state": "active"},
		map[string]any{"id": "gid://gitlab/User/8", "username": "dave", "state": "deactivated"},
	}}}}))
	c := newClient(t, srv, writeToken(t, "glpat-1"), 30)
	got, err := c.UsersByUsernames(context.Background(), []string{"alice", "dave", "nobody"})
	require.NoError(t, err)
	assert.Equal(t, []gitlab.GraphQLUser{{ID: 2, Username: "alice", State: "active"}, {ID: 8, Username: "dave", State: "deactivated"}}, got)
	assert.Equal(t, 1, srv.TotalCalls(), "one request for all usernames (D28)")

	names := make([]string, 150)
	for i := range names {
		names[i] = fmt.Sprintf("u%d", i)
	}
	_, err = c.UsersByUsernames(context.Background(), names)
	require.NoError(t, err)
	assert.Equal(t, 3, srv.TotalCalls(), "100 usernames per request")

	bad := gitlabtest.NewUnix(t)
	bad.Handle(http.MethodPost, "/api/graphql", gitlabtest.OK(map[string]any{"errors": []any{map[string]any{"message": "boom"}}}))
	_, err = newClient(t, bad, writeToken(t, "glpat-1"), 30).UsersByUsernames(context.Background(), []string{"a"})
	assert.Equal(t, gitlab.Unexpected, gitlab.KindOf(err))
	odd := gitlabtest.NewUnix(t)
	odd.Handle(http.MethodPost, "/api/graphql", gitlabtest.OK(map[string]any{"data": map[string]any{"users": map[string]any{"nodes": []any{
		map[string]any{"id": "gid://gitlab/Group/2", "username": "x"}}}}}))
	_, err = newClient(t, odd, writeToken(t, "glpat-1"), 30).UsersByUsernames(context.Background(), []string{"x"})
	assert.ErrorContains(t, err, "unexpected global ID")
}

func TestClient_MemberAccess(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, "/api/v4/projects/1/members/all/2", gitlabtest.OK(map[string]any{"access_level": 50, "state": "active"}))
	srv.Handle(http.MethodGet, "/api/v4/projects/1/members/all/3", gitlabtest.Status(http.StatusNotFound))
	srv.Handle(http.MethodGet, "/api/v4/projects/1/members/all/4", gitlabtest.OK(map[string]any{"access_level": 40, "state": "blocked"}))
	srv.Handle(http.MethodGet, "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=2&user_ids%5B%5D=4",
		gitlabtest.OK([]any{map[string]any{"id": 2, "access_level": 40, "state": "active"}, map[string]any{"id": 4, "access_level": 50, "state": "deactivated"}}))
	c := newClient(t, srv, writeToken(t, "glpat-1"), 30)
	for uid, want := range map[int64]int{2: 50, 3: 0, 4: 0} {
		got, err := c.MemberAccess(context.Background(), 1, uid)
		require.NoError(t, err)
		assert.Equal(t, want, got, "user %d", uid)
	}
	got, err := c.MembersAccess(context.Background(), 1, []int64{2, 4})
	require.NoError(t, err)
	assert.Equal(t, map[int64]int{2: 40}, got, "inactive members do not count")
	empty, err := c.MembersAccess(context.Background(), 1, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
