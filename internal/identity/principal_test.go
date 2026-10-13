package identity_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

func TestResolver(t *testing.T) {
	srv := gitlabtest.NewUnix(t)
	srv.Handle(http.MethodGet, "/api/v4/users?username=alice", gitlabtest.OK([]map[string]any{{"id": 2, "username": "alice"}}))
	srv.Handle(http.MethodGet, "/api/v4/users?username=ghost", gitlabtest.OK([]map[string]any{}))
	srv.Handle(http.MethodGet, "/api/v4/users?username=down", gitlabtest.Status(http.StatusServiceUnavailable))
	srv.Handle(http.MethodGet, "/api/v4/users/2/keys/7", gitlabtest.OK(map[string]any{"id": 7}))
	srv.Handle(http.MethodGet, "/api/v4/users/2/keys/8", gitlabtest.Status(http.StatusNotFound))
	c := client(t, srv)
	ctx := context.Background()
	tests := []struct {
		name     string
		p        module.Principal
		want     string
		known    bool
		apiCalls int
	}{
		{"HTTPS user", module.Principal{GLID: "user-2", Username: "alice"}, "alice", true, 0},
		{"personal SSH key", module.Principal{GLID: "key-7", Username: "alice"}, "alice", true, 2},
		{"deploy key", module.Principal{GLID: "key-8", Username: "alice"}, "", true, 2},
		{"owner not visible", module.Principal{GLID: "key-9", Username: "ghost"}, "", true, 1},
		{"API failure", module.Principal{GLID: "key-9", Username: "down"}, "", false, 1},
		{"unparsable GL_ID", module.Principal{GLID: "deploy-1", Username: "alice"}, "", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := srv.TotalCalls()
			r := identity.NewResolver(c, tt.p, nil)
			for range 2 {
				got, st := r.Username(ctx)
				assert.Equal(t, tt.want, got)
				assert.Equal(t, tt.known, st.IsConfirmed())
			}
			assert.Equal(t, tt.apiCalls, srv.TotalCalls()-before, "resolved once per receive")
		})
	}
	stopped := identity.NewResolver(c, module.Principal{GLID: "key-7", Username: "alice"},
		func(context.Context) (module.Status, bool) { return module.Unknown("personal namespace"), true })
	_, st := stopped.Username(ctx)
	assert.False(t, st.IsConfirmed())
}
