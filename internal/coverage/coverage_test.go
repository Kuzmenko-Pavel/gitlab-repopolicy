package coverage

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
)

type fakeAPI struct {
	groups   []gitlab.Group
	access   map[int64]int
	projects map[int64][]gitlab.Project
	files    map[string]bool // "id:path"
	fail     error
}

func (f *fakeAPI) TopLevelGroups(context.Context) ([]gitlab.Group, error) { return f.groups, f.fail }
func (f *fakeAPI) GroupMemberAccess(_ context.Context, g, u int64) (int, error) {
	if u != 7 {
		return 0, errors.New("asked about the wrong user")
	}
	return f.access[g], nil
}
func (f *fakeAPI) GroupProjects(_ context.Context, g int64) ([]gitlab.Project, error) {
	return f.projects[g], nil
}
func (f *fakeAPI) FileExists(_ context.Context, id int64, path, ref string) (bool, error) {
	if ref != "main" {
		return false, errors.New("not the default branch")
	}
	return f.files[fmt.Sprintf("%d:%s", id, path)], nil
}

func TestRun(t *testing.T) {
	api := &fakeAPI{
		groups: []gitlab.Group{{ID: 1, FullPath: "acme"}, {ID: 2, FullPath: "beta"}, {ID: 3, FullPath: "gamma"}},
		access: map[int64]int{1: 40, 2: 30},
		projects: map[int64][]gitlab.Project{
			2: {{ID: 20, PathWithNamespace: "beta/app", DefaultBranch: "main"}, {ID: 21, PathWithNamespace: "beta/empty"}},
			3: {{ID: 30, PathWithNamespace: "gamma/sub/svc", DefaultBranch: "main"}, {ID: 31, PathWithNamespace: "gamma/docs", DefaultBranch: "main"}},
		},
		files: map[string]bool{"20:PATHPOLICY": true, "30:TAGPOLICY": true},
	}
	rep, err := Run(context.Background(), api, 7, []string{"PATHPOLICY", "COMMITPOLICY", "TAGPOLICY"})
	require.NoError(t, err)
	assert.Equal(t, 3, rep.Groups)
	assert.Equal(t, []Gap{
		{Group: gitlab.Group{ID: 2, FullPath: "beta"}, Access: 30, PolicyProjects: []string{"beta/app"}},
		{Group: gitlab.Group{ID: 3, FullPath: "gamma"}, PolicyProjects: []string{"gamma/sub/svc"}},
	}, rep.Gaps)

	api.fail = errors.New("HTTP 401")
	_, err = Run(context.Background(), api, 7, nil)
	assert.ErrorContains(t, err, "HTTP 401")
}
