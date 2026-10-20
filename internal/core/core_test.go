package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/hookenv"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/testmodule"
)

// testModule is the test module of the core; see its package for the instructions.
type testModule = testmodule.Module

type fakeAPI struct {
	users     []gitlab.User
	calls     int
	userCalls int
	// projectErr and projectLevel answer the service account check; the default is Maintainer.
	projectErr   error
	projectLevel int
	projectCalls int
}

func (f *fakeAPI) ProjectAccess(context.Context, int64) (int, error) {
	f.projectCalls++
	if f.projectErr != nil {
		return 0, f.projectErr
	}
	if f.projectLevel == 0 {
		return 40, nil
	}
	return f.projectLevel, nil
}

func (f *fakeAPI) UsersByUsernames(_ context.Context, names []string) ([]gitlab.GraphQLUser, error) {
	f.calls++
	out := make([]gitlab.GraphQLUser, 0, len(names))
	for i, n := range names {
		out = append(out, gitlab.GraphQLUser{ID: int64(100 + i), Username: n, State: "active"})
	}
	return out, nil
}

func (f *fakeAPI) MembersAccess(_ context.Context, _ int64, ids []int64) (map[int64]int, error) {
	f.calls++
	out := map[int64]int{}
	for _, id := range ids {
		out[id] = 40
	}
	return out, nil
}

func (f *fakeAPI) ProtectedBranches(context.Context, int64) ([]string, error) {
	f.calls++
	return []string{"main"}, nil
}

func (f *fakeAPI) ProtectedTags(context.Context, int64) ([]string, error) {
	f.calls++
	return nil, nil
}

func (f *fakeAPI) UsersByUsername(context.Context, string) ([]gitlab.User, error) {
	f.userCalls++
	return f.users, nil
}

func (f *fakeAPI) Calls() int { return f.calls + f.userCalls }

func (f *fakeAPI) UserHasKey(context.Context, int64, int64) (bool, error) {
	f.calls++
	return true, nil
}

type memAudit struct{ events []audit.Event }

func (m *memAudit) Write(e audit.Event) error {
	m.events = append(m.events, e)
	return nil
}

type repo struct {
	t          *testing.T
	work, bare string
	env        []string
	path       string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	path := os.Getenv("PATH")
	if bin := os.Getenv("GIT_BIN"); bin != "" && bin != "git" {
		path = filepath.Dir(bin) + string(filepath.ListSeparator) + path
	}
	r := &repo{t: t, work: filepath.Join(t.TempDir(), "w"), bare: filepath.Join(t.TempDir(), "s.git"), path: path}
	r.env = []string{"PATH=" + path, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "HOME=" + t.TempDir(),
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com"}
	r.git("", "init", "-q", "-b", "main", r.work)
	r.git("", "init", "-q", "--bare", "-b", "main", r.bare)
	return r
}

func (r *repo) git(dir string, args ...string) string {
	r.t.Helper()
	cmd := exec.CommandContext(r.t.Context(), fixtureGit(), args...)
	cmd.Env, cmd.Dir = r.env, dir
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func (r *repo) commit(files map[string]string) string {
	r.t.Helper()
	for name, content := range files {
		p := filepath.Join(r.work, name)
		require.NoError(r.t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(r.t, os.WriteFile(p, []byte(content), 0o644))
	}
	r.git(r.work, "add", "-A")
	r.git(r.work, "commit", "-q", "--allow-empty", "-m", "c")
	return r.git(r.work, "rev-parse", "HEAD")
}

func (r *repo) publish(rev string) { r.git(r.work, "push", "-q", "-f", r.bare, rev+":refs/heads/main") }

func (r *repo) stage(rev string) {
	r.git(r.work, "push", "-q", "-f", r.bare, rev+":refs/staging/x")
	r.git(r.bare, "update-ref", "-d", "refs/staging/x")
}

type run struct {
	out   Outcome
	audit *memAudit
	api   *fakeAPI
}

func (r *repo) run(modules []module.Module, settings Settings, api *fakeAPI, path string, updates ...module.RefUpdate) run {
	r.t.Helper()
	git, err := gitx.Open(gitx.Env{Dir: r.bare, Path: r.path})
	require.NoError(r.t, err)
	defer func() { assert.NoError(r.t, git.Close()) }()
	if api == nil {
		api = &fakeAPI{}
	}
	if path == "" {
		path = "acme/platform/app"
	}
	a := &memAudit{}
	env := hookenv.Env{Principal: module.Principal{GLID: "user-2", Username: "alice"}, Protocol: "http",
		Repository: "project-1", ProjectPath: path, ProjectID: 1}
	deps := Deps{Modules: modules, Git: git, API: api, Audit: a, Clock: time.Now}
	if root, ok := modules[0].(module.Root); ok {
		deps.Root, deps.Modules = root, modules[1:]
	}
	out := Run(context.Background(), settings, deps, Request{Env: env, Updates: updates})
	return run{out: out, audit: a, api: api}
}

var (
	enforce = Settings{Mode: config.ModeEnforce, Total: 10 * time.Second}
	closed  = testModule{FileName: "TESTPOLICY", Mode: module.FailClosed}
	open    = testModule{FileName: "OPENPOLICY", Mode: module.FailOpen}
	zero40  = strings.Repeat("0", 40)
)

func events(a *memAudit, name string) []audit.Event {
	var out []audit.Event
	for _, e := range a.events {
		if e.Event == name {
			out = append(out, e)
		}
	}
	return out
}

func TestModulePanic_FailClosed(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "panic\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"x": "1"})
	r.stage(c2)
	res := r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"})
	assert.False(t, res.out.Allow)
	assert.Contains(t, res.out.Stderr, "TESTPOLICY: internal error: the push was not checked and is rejected")
	assert.NotContains(t, res.out.Stderr, "boom", "panic details stay in audit")
	ev := events(res.audit, audit.EventInternalError)
	require.Len(t, ev, 1)
	assert.Contains(t, ev[0].Reason, "boom")
}

func TestModulePanic_FailOpen(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"OPENPOLICY": "panic\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"x": "1"})
	r.stage(c2)
	res := r.run([]module.Module{open}, enforce, nil, "", module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"})
	assert.True(t, res.out.Allow)
	assert.Contains(t, res.out.Stdout, "OPENPOLICY was not checked because of an internal error")
}

func TestLimits_Total(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "slow\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"x": "1"})
	r.stage(c2)
	s := Settings{Mode: config.ModeEnforce, Total: 200 * time.Millisecond}
	start := time.Now()
	res := r.run([]module.Module{closed}, s, nil, "", module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"})
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.False(t, res.out.Allow)
	assert.Contains(t, res.out.Stderr, "limit reached: the time of a push (timeouts.total 200ms)")
	assert.Equal(t, module.CodeLimitExceeded, events(res.audit, audit.EventDecision)[0].Code)
}

func TestInvalidAnchorPolicy(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "garbage\n", "OPENPOLICY": "garbage\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"x": "1"})
	r.stage(c2)
	upd := module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"}

	res := r.run([]module.Module{open}, enforce, nil, "", upd)
	assert.True(t, res.out.Allow, "FailOpen: warning only")
	assert.Contains(t, res.out.Stdout, "OPENPOLICY on the default branch is invalid")

	res = r.run([]module.Module{closed, open}, enforce, nil, "", upd)
	assert.False(t, res.out.Allow, "FailClosed: deny")
	assert.Contains(t, res.out.Stderr, "TESTPOLICY, branch main: the policy in the default branch is broken")
	assert.Len(t, events(res.audit, audit.EventInvalidPolicy), 2)

	res = r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: c1, New: zero40, Ref: "refs/heads/old"})
	assert.True(t, res.out.Allow, "deleting a ref is not blocked")
}

func TestPolicyFileKinds(t *testing.T) {
	for name, setup := range map[string]func(r *repo){
		"directory": func(r *repo) { r.commit(map[string]string{"TESTPOLICY/x": "touch"}) },
		"symlink": func(r *repo) {
			r.commit(map[string]string{"target": "touch"})
			require.NoError(t, os.Symlink("target", filepath.Join(r.work, "TESTPOLICY")))
			r.commit(nil)
		},
		"too large": func(r *repo) { r.commit(map[string]string{"TESTPOLICY": strings.Repeat("touch\n", 20000)}) },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			setup(r)
			c1 := r.git(r.work, "rev-parse", "HEAD")
			r.publish(c1)
			c2 := r.commit(map[string]string{"x": "1"})
			r.stage(c2)
			res := r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"})
			assert.False(t, res.out.Allow)
			assert.Contains(t, res.out.Stderr, "the policy in the default branch is broken")
		})
	}
}

func TestAcceptance(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "touch\n"})
	r.publish(c1)
	bad := r.commit(map[string]string{"TESTPOLICY": "bad-new\n"})
	r.stage(bad)
	res := r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: zero40, New: bad, Ref: "refs/heads/feature"})
	assert.False(t, res.out.Allow, "invalid new version is rejected on a feature branch")
	assert.Contains(t, res.out.Stderr, "the changed TESTPOLICY is invalid")
	assert.Contains(t, res.out.Stderr, "bad-new is rejected at acceptance")

	r.git(r.work, "reset", "-q", "--hard", c1)
	good := r.commit(map[string]string{"TESTPOLICY": "touch\ntouch\n"})
	r.stage(good)
	res = r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: c1, New: good, Ref: "refs/heads/main"})
	assert.True(t, res.out.Allow)
	changes := events(res.audit, audit.EventPolicyChange)
	require.Len(t, changes, 1)
	assert.Equal(t, "update", changes[0].Action)
}

func TestMode_Audit(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "deny\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"x": "1"})
	r.stage(c2)
	upd := module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"}
	res := r.run([]module.Module{closed}, Settings{Mode: config.ModeAudit, Total: 10 * time.Second}, nil, "", upd)
	assert.True(t, res.out.Allow)
	assert.Empty(t, res.out.Stderr)
	ev := events(res.audit, audit.EventDecision)
	require.Len(t, ev, 1)
	assert.Equal(t, "deny", ev[0].Decision)
	assert.Equal(t, "audit only", ev[0].Violations[0].Reason)

	res = r.run([]module.Module{closed}, Settings{Mode: config.ModeOff, Total: 10 * time.Second}, nil, "", upd)
	assert.True(t, res.out.Allow)
	assert.Empty(t, res.audit.events, "mode: off does nothing")
}

func TestPersonalNamespace(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "classify\ndeny\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"x": "1"})
	r.stage(c2)
	upd := module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/feature"}

	personal := &fakeAPI{users: []gitlab.User{{ID: 2, Username: "alice"}}}
	res := r.run([]module.Module{closed}, enforce, personal, "alice/scratch", upd)
	assert.True(t, res.out.Allow, "personal projects are not checked")
	assert.Empty(t, res.audit.events)
	assert.Zero(t, personal.calls, "no protected-class request for a personal project")
	assert.Equal(t, 1, personal.userCalls)

	group := &fakeAPI{}
	res = r.run([]module.Module{closed}, enforce, group, "acme/app", upd)
	assert.False(t, res.out.Allow)
	assert.Equal(t, 1, group.calls)

	deep := &fakeAPI{}
	res = r.run([]module.Module{closed}, enforce, deep, "", upd)
	assert.False(t, res.out.Allow)
	assert.Zero(t, deep.userCalls, "three segments need no user lookup")
}

func TestNonProjectAndOtherRefs(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "deny\n"})
	r.publish(c1)
	res := r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: zero40, New: c1, Ref: "refs/replace/" + c1})
	assert.True(t, res.out.Allow, "refs outside heads and tags are not evaluated")
	assert.Empty(t, res.audit.events)
}

// fixtureGit is the git of the fixtures: GIT_BIN when it is a path. exec looks
// a bare name up in the PATH of the test process, not in the child's.
func fixtureGit() string {
	if bin := os.Getenv("GIT_BIN"); bin != "" && bin != "git" {
		return bin
	}
	return "git"
}

func TestTagsOnlyOnBranches(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"a": "1"})
	r.publish(c1)
	r.git(r.work, "push", "-q", r.bare, c1+":refs/heads/old")
	c2 := r.commit(map[string]string{"a": "2"})
	r.stage(c2)
	tag := func(rev string) module.RefUpdate { return module.RefUpdate{Old: zero40, New: rev, Ref: "refs/tags/v1"} }
	code := func(res run) string {
		ev := events(res.audit, audit.EventDecision)
		require.NotEmpty(t, ev)
		return ev[0].Code
	}

	assert.True(t, r.run([]module.Module{closed}, enforce, nil, "", tag(c1)).out.Allow, "a commit of main")
	res := r.run([]module.Module{closed}, enforce, nil, "", tag(c2))
	assert.False(t, res.out.Allow)
	assert.Equal(t, module.CodeTagNotOnBranch, code(res))
	assert.Contains(t, res.out.Stderr, "git push origin <branch> v1")
	assert.True(t, r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: zero40, New: c2, Ref: "refs/heads/feature"}, tag(c2)).out.Allow,
		"the branch comes in the same push")
	assert.False(t, r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: c1, New: zero40, Ref: "refs/heads/main"},
		module.RefUpdate{Old: c1, New: zero40, Ref: "refs/heads/old"}, tag(c1)).out.Allow, "the branches are deleted in the same push")
	assert.True(t, r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/old"},
		module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"}, tag(c1)).out.Allow, "the new tips still hold the commit")
	side := r.git(r.work, "commit-tree", "-p", c1, "-m", "side", c1+"^{tree}")
	r.git(r.work, "push", "-q", r.bare, side+":refs/heads/side")
	assert.False(t, r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: side, New: c1, Ref: "refs/heads/side"}, tag(side)).out.Allow,
		"the old tip of a branch moved away does not count")
	assert.True(t, r.run([]module.Module{closed}, enforce, nil, "", module.RefUpdate{Old: c1, New: zero40, Ref: "refs/tags/v0"}).out.Allow,
		"a deletion is not checked")
	assert.True(t, r.run([]module.Module{closed}, Settings{Mode: config.ModeAudit, Total: 10 * time.Second}, nil, "", tag(c2)).out.Allow)

	tree := r.git(r.work, "rev-parse", c1+"^{tree}")
	res = r.run([]module.Module{closed}, enforce, nil, "", tag(tree))
	assert.False(t, res.out.Allow)
	assert.Equal(t, module.CodeTagObjectInvalid, code(res))

	personal := &fakeAPI{users: []gitlab.User{{ID: 2, Username: "alice"}}}
	assert.True(t, r.run([]module.Module{closed}, enforce, personal, "alice/scratch", tag(c2)).out.Allow, "personal projects are not checked")
}

func TestDefaultBranch(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"a": "1"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"a": "2"})
	r.stage(c2)
	upd := module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"}
	with := func(mode config.Mode, names ...string) Settings {
		return Settings{Mode: mode, Total: 10 * time.Second, DefaultBranch: names}
	}

	assert.True(t, r.run([]module.Module{closed}, with(config.ModeEnforce, "master", "main"), nil, "", upd).out.Allow)
	assert.True(t, r.run([]module.Module{closed}, with(config.ModeEnforce), nil, "", upd).out.Allow, "an empty list is not checked")

	res := r.run([]module.Module{closed}, with(config.ModeEnforce, "master"), nil, "", upd)
	assert.False(t, res.out.Allow)
	assert.Contains(t, res.out.Stderr, "the default branch main is not one of master")
	ev := events(res.audit, audit.EventDecision)
	require.Len(t, ev, 1)
	assert.Equal(t, module.CodeDefaultBranchNotAllowed, ev[0].Code)
	assert.Equal(t, "deny", ev[0].Decision)

	del := module.RefUpdate{Old: c1, New: zero40, Ref: "refs/heads/old"}
	assert.False(t, r.run([]module.Module{closed}, with(config.ModeEnforce, "master"), nil, "", del).out.Allow, "deletions are checked too")

	res = r.run([]module.Module{closed}, with(config.ModeAudit, "master"), nil, "", upd)
	assert.True(t, res.out.Allow)
	assert.Equal(t, module.CodeDefaultBranchNotAllowed, events(res.audit, audit.EventDecision)[0].Code)

	git, err := gitx.Open(gitx.Env{Dir: r.bare, Path: r.path})
	require.NoError(t, err)
	defer func() { assert.NoError(t, git.Close()) }()
	env := hookenv.Env{Principal: module.Principal{GLID: "user-2", Username: "alice"}, ProjectPath: "a/b/c", ProjectID: 1}
	out := Run(context.Background(), with(config.ModeEnforce, "master"), Deps{Modules: []module.Module{closed}, Git: git, API: &fakeAPI{},
		Audit: &memAudit{}, Breakglass: grant{project: 1}, Clock: time.Now}, Request{Env: env, Updates: []module.RefUpdate{upd}})
	assert.True(t, out.Allow, "break-glass comes first")
}

func TestDefaultBranch_EmptyRepository(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"a": "1"})
	r.stage(c1)
	settings := Settings{Mode: config.ModeEnforce, Total: 10 * time.Second, DefaultBranch: []string{"main"}}
	dev := module.RefUpdate{Old: zero40, New: c1, Ref: "refs/heads/dev"}
	main := module.RefUpdate{Old: zero40, New: c1, Ref: "refs/heads/main"}
	tag := module.RefUpdate{Old: zero40, New: c1, Ref: "refs/tags/v1"}

	res := r.run([]module.Module{closed}, settings, nil, "", dev)
	assert.False(t, res.out.Allow, "GitLab would make dev the default branch")
	assert.Contains(t, res.out.Stderr, "push main first")
	assert.True(t, r.run([]module.Module{closed}, settings, nil, "", dev, main).out.Allow, "main comes with the push")
	res = r.run([]module.Module{closed}, settings, nil, "", tag)
	assert.NotContains(t, res.out.Stderr, "push main first", "a tag does not set the default branch")
	assert.Contains(t, res.out.Stderr, "is in no branch")
	assert.True(t, r.run([]module.Module{closed}, Settings{Mode: config.ModeEnforce, Total: 10 * time.Second}, nil, "", dev).out.Allow, "without default_branch nothing is checked")

	r.publish(c1)
	assert.True(t, r.run([]module.Module{closed}, settings, nil, "", dev).out.Allow, "a repository with branches keeps its default branch")
}
