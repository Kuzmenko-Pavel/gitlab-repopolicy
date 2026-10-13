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
