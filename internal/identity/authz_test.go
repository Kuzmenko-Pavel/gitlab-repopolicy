package identity_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

type pusher struct{ p module.Person }

func (p pusher) Person(context.Context) (module.Person, module.Status) {
	return p.p, module.Confirmed()
}

func TestAuthorizer(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodPost, "/api/graphql", gitlabtest.OK(map[string]any{"data": map[string]any{"users": map[string]any{"nodes": []any{
		map[string]any{"id": "gid://gitlab/User/2", "username": "Alice", "state": "active"},
		map[string]any{"id": "gid://gitlab/User/8", "username": "dave", "state": "deactivated"},
	}}}}))
	srv.Handle(http.MethodGet, "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=2",
		gitlabtest.OK([]any{map[string]any{"id": 2, "access_level": 50, "state": "active"}}))
	srv.Handle(http.MethodGet, "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=8", gitlabtest.OK([]any{}))
	a := identity.NewAuthorizer(client(t, srv), 1, pusher{module.Person{Username: "alice", ID: 2}}, nil)
	ctx := context.Background()

	users, st := a.Users(ctx, []string{"alice", "DAVE", "ghost"})
	require.True(t, st.IsConfirmed())
	assert.Equal(t, map[string]module.User{"alice": {ID: 2, Active: true}, "dave": {ID: 8}}, users)
	_, _ = a.Users(ctx, []string{"alice", "ghost"})
	assert.Equal(t, 1, srv.Calls(http.MethodPost, "/api/graphql"), "answers are memoised, missing users too")

	level, st := a.PusherAccess(ctx)
	require.True(t, st.IsConfirmed())
	assert.Equal(t, 50, level)
	levels, st := a.AccessLevels(ctx, []int64{2, 8})
	require.True(t, st.IsConfirmed())
	assert.Equal(t, map[int64]int{2: 50, 8: 0}, levels, "one level is cached, the other is asked")
	assert.Equal(t, 1, srv.Calls(http.MethodGet, "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=2"))

	k := a.WithKnown([]string{"Alice"})
	assert.True(t, k.Known("alice"))
	assert.False(t, a.Known("alice"))

	nobody := identity.NewAuthorizer(client(t, srv), 1, pusher{}, nil)
	level, st = nobody.PusherAccess(ctx)
	assert.True(t, st.IsConfirmed())
	assert.Zero(t, level, "a deploy key has no access of its own")
}

func TestAuthorizer_FailuresAndGuard(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodPost, "/api/graphql", gitlabtest.Status(http.StatusBadGateway))
	srv.Handle(http.MethodGet, "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=2", gitlabtest.Status(http.StatusTooManyRequests))
	a := identity.NewAuthorizer(client(t, srv), 1, pusher{module.Person{Username: "alice", ID: 2}}, nil)
	_, st := a.Users(context.Background(), []string{"alice"})
	assert.False(t, st.IsConfirmed())
	_, st = a.PusherAccess(context.Background())
	assert.False(t, st.IsConfirmed(), "a failure is unknown, never level 0")

	stopped := identity.NewAuthorizer(client(t, srv), 1, pusher{module.Person{Username: "alice", ID: 2}},
		func(context.Context) (module.Status, bool) { return module.Unknown("project_not_visible"), true })
	calls := srv.TotalCalls()
	_, st = stopped.Users(context.Background(), []string{"x"})
	assert.Equal(t, "project_not_visible", st.Reason())
	_, st = stopped.AccessLevels(context.Background(), []int64{3})
	assert.Equal(t, "project_not_visible", st.Reason())
	assert.Equal(t, calls, srv.TotalCalls(), "the guard stops calls")
}
