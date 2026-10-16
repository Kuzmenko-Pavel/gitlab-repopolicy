package refs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

type fakeAPI struct {
	branches, tags []string
	err            error
	calls          int
}

func (f *fakeAPI) ProtectedBranches(context.Context, int64) ([]string, error) {
	f.calls++
	return f.branches, f.err
}

func (f *fakeAPI) ProtectedTags(context.Context, int64) ([]string, error) {
	f.calls++
	return f.tags, f.err
}

func TestProtectedPattern(t *testing.T) {
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
		assert.Equal(t, tt.want, Match(tt.pattern, tt.name), "%s ~ %s", tt.pattern, tt.name)
	}
}

func TestClassifier_LazyOnce(t *testing.T) {
	api := &fakeAPI{branches: []string{"release/*"}, tags: []string{"v*"}}
	c := New("refs/heads/main", 1, api, nil)
	ctx := context.Background()

	class, status := c.Class(ctx, "refs/heads/main")
	assert.Equal(t, module.ClassDefault, class)
	assert.True(t, status.IsConfirmed())
	assert.Zero(t, api.calls, "default needs no API")

	class, _ = c.Class(ctx, "refs/replace/abc")
	assert.Equal(t, module.ClassOther, class)
	assert.Zero(t, api.calls, "refs outside heads and tags need no API")

	for range 3 {
		class, status = c.Class(ctx, "refs/heads/release/1")
		assert.Equal(t, module.ClassProtected, class)
		assert.True(t, status.IsConfirmed())
	}
	class, _ = c.Class(ctx, "refs/heads/feature")
	assert.Equal(t, module.ClassOther, class)
	assert.Equal(t, 1, api.calls, "branches are requested once")

	class, _ = c.Class(ctx, "refs/tags/v1.0.0")
	assert.Equal(t, module.ClassProtected, class)
	class, _ = c.Class(ctx, "refs/tags/latest")
	assert.Equal(t, module.ClassOther, class)
	assert.Equal(t, 2, api.calls, "tags are requested once")

	scope, status := c.ProtectedScope(ctx)
	assert.True(t, status.IsConfirmed())
	assert.Equal(t, module.ClassProtected, scope.Class)
	assert.True(t, scope.Protected("refs/heads/release/2"))
	assert.True(t, scope.Protected("refs/tags/v2"))
	assert.False(t, scope.Protected("refs/heads/main"), "default is not in the protected set unless protected")
	assert.False(t, scope.Protected("refs/merge-requests/1/head"))
	assert.Equal(t, 2, api.calls)
}

func TestClassifier_Failures(t *testing.T) {
	ctx := context.Background()
	api := &fakeAPI{err: &gitlab.Error{Kind: gitlab.Unavailable}}
	c := New("refs/heads/main", 1, api, nil)
	class, status := c.Class(ctx, "refs/heads/x")
	assert.Equal(t, module.ClassOther, class)
	assert.False(t, status.IsConfirmed())
	assert.False(t, status.IsLimit())
	_, status = c.Class(ctx, "refs/heads/y")
	assert.False(t, status.IsConfirmed(), "a failure is remembered for the receive")
	assert.Equal(t, 1, api.calls)
	_, status = c.ProtectedScope(ctx)
	assert.False(t, status.IsConfirmed())

	limit := New("", 1, &fakeAPI{err: &gitlab.Error{Kind: gitlab.BudgetExceeded}}, nil)
	_, status = limit.Class(ctx, "refs/heads/x")
	assert.True(t, status.IsLimit())

	assert.True(t, StatusOf(&gitlab.Error{Kind: gitlab.Deadline}).IsLimit())
	assert.False(t, StatusOf(errors.New("x")).IsConfirmed())
}

func TestClassifier_GuardRunsBeforeAPI(t *testing.T) {
	api := &fakeAPI{branches: []string{"*"}}
	guarded := 0
	c := New("refs/heads/main", 1, api, func(context.Context) (module.Status, bool) {
		guarded++
		return module.Unknown("personal namespace"), true
	})
	_, status := c.Class(context.Background(), "refs/heads/x")
	assert.False(t, status.IsConfirmed())
	assert.Zero(t, api.calls)
	assert.Equal(t, 1, guarded)

	pass := New("refs/heads/main", 1, api, func(context.Context) (module.Status, bool) { return module.Confirmed(), false })
	class, status := pass.Class(context.Background(), "refs/heads/x")
	assert.True(t, status.IsConfirmed())
	assert.Equal(t, module.ClassProtected, class)
}
