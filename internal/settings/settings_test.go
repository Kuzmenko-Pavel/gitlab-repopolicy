package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
)

type fakeAPI struct {
	projects []gitlab.Project
	files    map[string]string // "id:ref:path"
	branches map[int64][]string
	tags     map[int64][]string
	users    map[string]string // username -> state
	fail     map[string]error  // "files:id", "branches:id", "users"
}

func (f *fakeAPI) MemberProjects(context.Context) ([]gitlab.Project, error) {
	if err := f.fail["projects"]; err != nil {
		return nil, err
	}
	return f.projects, nil
}

func (f *fakeAPI) RawFile(_ context.Context, id int64, path, ref string, _ int) ([]byte, error) {
	if err := f.fail[fmt.Sprintf("files:%d", id)]; err != nil {
		return nil, err
	}
	data, ok := f.files[fmt.Sprintf("%d:%s:%s", id, ref, path)]
	if !ok {
		return nil, &gitlab.Error{Kind: gitlab.NotFound, Status: 404}
	}
	return []byte(data), nil
}

func (f *fakeAPI) ProtectedBranches(_ context.Context, id int64) ([]string, error) {
	if err := f.fail[fmt.Sprintf("branches:%d", id)]; err != nil {
		return nil, err
	}
	return f.branches[id], nil
}

func (f *fakeAPI) ProtectedTags(_ context.Context, id int64) ([]string, error) {
	return f.tags[id], nil
}

func (f *fakeAPI) UsersByUsernames(_ context.Context, names []string) ([]gitlab.GraphQLUser, error) {
	if err := f.fail["users"]; err != nil {
		return nil, err
	}
	var out []gitlab.GraphQLUser
	for _, n := range names {
		if st, ok := f.users[strings.ToLower(n)]; ok {
			out = append(out, gitlab.GraphQLUser{Username: n, State: st})
		}
	}
	return out, nil
}

const pathPolicy = `version: 1
admins: [platform-lead]
rules:
  - name: ci
    paths: [".gitlab-ci.yml", "ci/**"]
    allow: [platform-lead, alice]
`

const commitPolicy = `version: 1
mode: enforce
rulesets:
  - name: all
    refs: all
    except:
      users: [release-bot]
    rules:
      type-enum: { level: error, value: [feat, fix] }
`

func ptr[T any](v T) *T { return &v }

func field[T any](v T) gitlab.Field[T] { return gitlab.Field[T]{Present: true, Value: v} }

func project(id int64, path string) gitlab.Project {
	p := gitlab.Project{ID: id, PathWithNamespace: path, DefaultBranch: "main",
		CIConfigPath: field(""), JobTokenPush: field(false)}
	p.Namespace.Kind = "group"
	p.Permissions.Group = &struct {
		AccessLevel int `json:"access_level"`
	}{AccessLevel: 40}
	return p
}

func expected() config.SettingsCheck {
	return config.SettingsCheck{DefaultBranch: []string{"main"}, ProtectedBranches: []string{"release/*"}, ProtectedTags: []string{"v*"}, JobTokenPush: ptr(false)}
}

func healthy() *fakeAPI {
	return &fakeAPI{
		projects: []gitlab.Project{project(1, "acme/app")},
		files:    map[string]string{"1:main:PATHPOLICY": pathPolicy, "1:main:COMMITPOLICY": commitPolicy},
		branches: map[int64][]string{1: {"main", "release/*"}},
		tags:     map[int64][]string{1: {"v*"}},
		users:    map[string]string{"platform-lead": "active", "alice": "active", "release-bot": "active"},
		fail:     map[string]error{},
	}
}

func run(t *testing.T, api *fakeAPI, exp config.SettingsCheck) Report {
	t.Helper()
	set := modules.Default(modules.Settings{PatternTimeout: time.Second, PatternBudget: time.Second, Clock: time.Now})
	root, mods := set.Root, set.All()
	rep, err := Run(context.Background(), api, Options{Expected: exp, Guard: root, Modules: mods})
	require.NoError(t, err)
	return rep
}

func checks(p Project) []string {
	out := make([]string, 0, len(p.Drifts))
	for _, d := range p.Drifts {
		out = append(out, d.Check)
	}
	return out
}

func TestRun_NoDrift(t *testing.T) {
	rep := run(t, healthy(), expected())
	require.Len(t, rep.Checked, 1)
	assert.Empty(t, rep.Checked[0].Drifts)
	assert.Empty(t, rep.Drifted())
}

func TestRun_Drifts(t *testing.T) {
	cases := map[string]struct {
		change func(*fakeAPI)
		check  string
		reason string
	}{
		"ci config in another project": {func(f *fakeAPI) { f.projects[0].CIConfigPath = field("ci.yml@other/project") }, CheckCIConfigPath, "outside of the repository"},
		"ci config by URL":             {func(f *fakeAPI) { f.projects[0].CIConfigPath = field("https://example.com/ci.yml") }, CheckCIConfigPath, "outside"},
		"ci config not covered":        {func(f *fakeAPI) { f.projects[0].CIConfigPath = field("build/ci.yml") }, CheckCIConfigPath, "not covered"},
		"ci config not visible":        {func(f *fakeAPI) { f.projects[0].CIConfigPath = gitlab.Field[string]{} }, CheckCIConfigPath, "not visible"},
		"default branch changed": {func(f *fakeAPI) {
			f.projects[0].DefaultBranch = "old"
			f.files["1:old:PATHPOLICY"] = pathPolicy
		}, CheckDefaultBranch, `"old", expected one of main`},
		"default branch without PATHPOLICY": {func(f *fakeAPI) { f.projects[0].DefaultBranch = "empty" }, CheckDefaultBranch, `"empty"`},
		"main not protected":                {func(f *fakeAPI) { f.branches[1] = []string{"release/*"} }, CheckProtected, "not protected: main (the default branch)"},
		"release not protected":             {func(f *fakeAPI) { f.branches[1] = []string{"main"} }, CheckProtected, "not protected: release/*"},
		"tags not protected": {func(f *fakeAPI) {
			f.files["1:main:TAGPOLICY"] = "version: 1\n"
			f.tags[1] = nil
		}, CheckProtectedTags, "not protected: v*"},
		"job token push on":            {func(f *fakeAPI) { f.projects[0].JobTokenPush = field(true) }, CheckJobTokenPush, "is true, expected false"},
		"admin left":                   {func(f *fakeAPI) { delete(f.users, "platform-lead") }, CheckPrincipals, "platform-lead"},
		"user of COMMITPOLICY blocked": {func(f *fakeAPI) { f.users["release-bot"] = "blocked" }, CheckPrincipals, "release-bot"},
		"role below Maintainer": {func(f *fakeAPI) {
			f.projects[0].Permissions.Group.AccessLevel = 30
		}, CheckAccess, "access level 30"},
		"invalid PATHPOLICY":   {func(f *fakeAPI) { f.files["1:main:PATHPOLICY"] = "version: 2\n" }, CheckPolicy, "PATHPOLICY on main cannot be parsed"},
		"invalid COMMITPOLICY": {func(f *fakeAPI) { f.files["1:main:COMMITPOLICY"] = "bogus: 1\n" }, CheckPolicy, "COMMITPOLICY"},
		"files unavailable":    {func(f *fakeAPI) { f.fail["files:1"] = errors.New("HTTP 502") }, CheckUnavailable, "HTTP 502"},
		"branches unavailable": {func(f *fakeAPI) { f.fail["branches:1"] = errors.New("timeout") }, CheckUnavailable, "protected branches: timeout"},
		"users unavailable":    {func(f *fakeAPI) { f.fail["users"] = errors.New("HTTP 500") }, CheckUnavailable, "principals"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := healthy()
			tc.change(api)
			rep := run(t, api, expected())
			require.Len(t, rep.Drifted(), 1)
			p := rep.Drifted()[0]
			assert.Contains(t, checks(p), tc.check)
			reasons := make([]string, 0, len(p.Drifts))
			for _, d := range p.Drifts {
				reasons = append(reasons, d.Reason)
			}
			assert.Contains(t, strings.Join(reasons, "\n"), tc.reason)
		})
	}
}

func TestRun_ProtectionScope(t *testing.T) {
	api := healthy()
	api.tags[1] = nil
	api.branches[1] = []string{"*", "release/*"}
	assert.Empty(t, run(t, api, expected()).Drifted(), "tags matter only with TAGPOLICY; a wildcard protects main")

	api = healthy()
	api.branches[1] = nil
	rep := run(t, api, config.SettingsCheck{})
	require.Len(t, rep.Drifted(), 1, "the default branch is protected even when nothing is configured")
	assert.Equal(t, "not protected: main (the default branch)", rep.Drifted()[0].Drifts[0].Reason)
}

func TestRun_Scope(t *testing.T) {
	api := healthy()
	personal := project(2, "alice/app")
	personal.Namespace.Kind = "user"
	api.projects = append(api.projects, personal, project(3, "acme/no-policy"))
	api.files["2:main:PATHPOLICY"] = "garbage"
	rep := run(t, api, config.SettingsCheck{})
	require.Len(t, rep.Checked, 1, "personal projects and projects without PATHPOLICY are not checked")
	assert.Equal(t, 2, rep.Skipped)

	api = healthy()
	api.projects[0].CIConfigPath = gitlab.Field[string]{}
	api.projects[0].JobTokenPush = field(true)
	rep = run(t, api, config.SettingsCheck{})
	assert.Equal(t, []string{CheckCIConfigPath}, checks(rep.Checked[0]), "without expectations only ci_config_path and principals are checked")
}

func TestRun_ListFailure(t *testing.T) {
	api := healthy()
	api.fail["projects"] = errors.New("HTTP 401")
	set := modules.Default(modules.Settings{PatternTimeout: time.Second, PatternBudget: time.Second, Clock: time.Now})
	root, mods := set.Root, set.All()
	_, err := Run(context.Background(), api, Options{Guard: root, Modules: mods})
	assert.ErrorContains(t, err, "HTTP 401")
	_, err = Run(context.Background(), api, Options{Modules: mods})
	assert.ErrorContains(t, err, "PATHPOLICY module is disabled")
}
