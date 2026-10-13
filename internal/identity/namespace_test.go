package identity_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
)

func client(t *testing.T, srv *gitlabtest.Server) *gitlab.Client {
	t.Helper()
	token := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(token, []byte("glpat-1"), 0o600))
	c, err := gitlab.New(gitlab.Options{Socket: srv.Socket(), BaseURL: srv.BaseURL(), TokenFile: token, Timeout: 200 * time.Millisecond, MaxCalls: 30})
	require.NoError(t, err)
	return c
}

func TestIsPersonalProject(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, "/api/v4/users?username=alice", gitlabtest.OK([]map[string]any{{"id": 2, "username": "Alice"}}))
	srv.Handle(http.MethodGet, "/api/v4/users?username=acme", gitlabtest.OK([]map[string]any{}))
	srv.Handle(http.MethodGet, "/api/v4/users?username=bob", gitlabtest.OK([]map[string]any{{"id": 3, "username": "bobby"}}))
	srv.Handle(http.MethodGet, "/api/v4/users?username=down", gitlabtest.Status(http.StatusInternalServerError))
	c := client(t, srv)
	ctx := context.Background()

	assert.True(t, identity.IsPersonalProject(ctx, c, "alice/scratch"), "two segments, user found (case-insensitive)")
	assert.False(t, identity.IsPersonalProject(ctx, c, "acme/app"), "two segments, no such user: group")
	assert.False(t, identity.IsPersonalProject(ctx, c, "bob/app"), "only an exact username counts")
	assert.False(t, identity.IsPersonalProject(ctx, c, "down/app"), "API failure: the project counts as a group project")
	calls := srv.TotalCalls()
	assert.False(t, identity.IsPersonalProject(ctx, c, "acme/platform/app"), "three segments are always a group")
	assert.False(t, identity.IsPersonalProject(ctx, c, "app"))
	assert.False(t, identity.IsPersonalProject(ctx, c, "/app"))
	assert.Equal(t, calls, srv.TotalCalls(), "no API call without two segments")
}
