package core

import (
	"context"
	"errors"
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
)

// hooked is a module with the optional entry points of the contract.
type hooked struct {
	testModule
	absent  func(*module.Input) (*module.Result, error)
	invalid func(*module.Input, error) (*module.Result, error)
}

func (h hooked) EvaluateAbsent(_ context.Context, in *module.Input) (*module.Result, error) {
	return h.absent(in)
}

func (h hooked) EvaluateInvalid(_ context.Context, in *module.Input, err error) (*module.Result, error) {
	return h.invalid(in, err)
}

func TestAbsentEvaluator(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"a": "1"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"TESTPOLICY": "touch\n"})
	r.stage(c2)
	var got *module.Input
	m := hooked{testModule: closed, absent: func(in *module.Input) (*module.Result, error) {
		got = in
		return &module.Result{Decision: module.Deny, Touched: true,
			Violations: []module.Violation{{Code: module.CodeLockoutPrevented, Ref: in.Updates[0].Ref}}}, nil
	}}
	res := r.run([]module.Module{m}, enforce, nil, "", module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"})
	require.NotNil(t, got, "the module is asked when the anchor has no file (bootstrap)")
	assert.Equal(t, c1, got.Anchor)
	assert.Equal(t, []string{"TESTPOLICY"}, got.PolicyFiles)
	assert.False(t, res.out.Allow)
	assert.Contains(t, res.out.Stderr, "new policy has no effective administrators")
	assert.Len(t, events(res.audit, audit.EventPolicyChange), 1, "the added file is accepted as a new version")
}

func TestInvalidEvaluator(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "garbage\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"a": "1"})
	r.stage(c2)
	var parseErr error
	m := hooked{testModule: closed, invalid: func(_ *module.Input, err error) (*module.Result, error) {
		parseErr = err
		return &module.Result{Notices: []string{"recovery is possible"}}, nil
	}}
	res := r.run([]module.Module{m}, enforce, nil, "", module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"})
	require.Error(t, parseErr)
	assert.Contains(t, parseErr.Error(), "unknown instruction garbage")
	assert.True(t, res.out.Allow, "the module decides instead of the default deny")
	assert.Contains(t, res.out.Stdout, "gitlab-repopolicy: recovery is possible")

	failing := hooked{testModule: closed, invalid: func(*module.Input, error) (*module.Result, error) {
		return nil, errors.New("broken")
	}}
	res = r.run([]module.Module{failing}, enforce, nil, "", module.RefUpdate{Old: c1, New: c2, Ref: "refs/heads/main"})
	assert.False(t, res.out.Allow, "an error of a FailClosed module denies")
}

type grant struct{ project int64 }

func (g grant) Active(projectID int64, _ time.Time) (string, bool) {
	return "INC-1", projectID == g.project
}

func TestBreakglass(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "deny\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"a": "1"})
	r.stage(c2)
	git, err := gitx.Open(gitx.Env{Dir: r.bare, Path: r.path})
	require.NoError(t, err)
	defer func() { assert.NoError(t, git.Close()) }()
	a := &memAudit{}
	env := hookenv.Env{Principal: module.Principal{GLID: "user-2", Username: "alice"}, ProjectPath: "a/b/c", ProjectID: 1}
	out := Run(context.Background(), enforce, Deps{Modules: []module.Module{closed}, Git: git, API: &fakeAPI{}, Audit: a,
		Breakglass: grant{project: 1}, Clock: time.Now}, Request{Env: env, Updates: []module.RefUpdate{{Old: c1, New: c2, Ref: "refs/heads/main"}}})
	assert.True(t, out.Allow)
	assert.Contains(t, out.Stdout, "break-glass is active")
	ev := events(a, audit.EventBreakglass)
	require.Len(t, ev, 1)
	assert.Equal(t, "INC-1", ev[0].Reason)
}

func TestObjectsAdapter(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"a": "hello"})
	r.publish(c1)
	git, err := gitx.Open(gitx.Env{Dir: r.bare, Path: r.path})
	require.NoError(t, err)
	defer func() { assert.NoError(t, git.Close()) }()
	o := objects{git: git}
	obj, err := o.Read(context.Background(), c1, 4096)
	require.NoError(t, err)
	assert.Equal(t, "commit", obj.Type)
	_, err = o.Read(context.Background(), c1, 10)
	assert.ErrorIs(t, err, module.ErrTooLarge)
	_, err = o.Read(context.Background(), strings.Repeat("1", 40), 10)
	assert.ErrorIs(t, err, module.ErrMissing)
	_, err = o.Read(context.Background(), "HEAD", 10)
	assert.Error(t, err)
}

func TestModuleReturnsNothing(t *testing.T) {
	_, err := call(func() (*module.Result, error) { return nil, nil })
	assert.ErrorContains(t, err, "no result")
	_, err = parse(nilParser{}, nil)
	assert.ErrorContains(t, err, "returned no policy")
	_, err = parse(panicParser{}, nil)
	assert.ErrorContains(t, err, "parser failed")
}

type nilParser struct{ testModule }

func (nilParser) Parse([]byte) (module.Policy, error) { return nil, nil }

type panicParser struct{ testModule }

func (panicParser) Parse([]byte) (module.Policy, error) { panic("bad input") }

type failingAudit struct{}

func (failingAudit) Write(audit.Event) error { return errors.New("disk full") }

func TestAuditFailureKeepsDecision(t *testing.T) {
	r := newRepo(t)
	c1 := r.commit(map[string]string{"TESTPOLICY": "deny\n"})
	r.publish(c1)
	c2 := r.commit(map[string]string{"a": "1"})
	r.stage(c2)
	git, err := gitx.Open(gitx.Env{Dir: r.bare, Path: r.path})
	require.NoError(t, err)
	defer func() { assert.NoError(t, git.Close()) }()
	env := hookenv.Env{Principal: module.Principal{GLID: "key-5", Username: "alice"}, ProjectPath: "a/b/c", ProjectID: 1}
	out := Run(context.Background(), Settings{Mode: config.ModeEnforce, Total: time.Minute},
		Deps{Modules: []module.Module{closed}, Git: git, API: &fakeAPI{}, Audit: failingAudit{}, Clock: time.Now},
		Request{Env: env, Updates: []module.RefUpdate{{Old: c1, New: c2, Ref: "refs/heads/main"}}})
	assert.False(t, out.Allow)
	assert.ErrorContains(t, out.AuditErr, "disk full")
	assert.Contains(t, out.Stderr, "user: alice (key 5)")
}

type listedPolicy struct{ users []string }

func (p listedPolicy) Usernames() []string                                       { return p.users }
func (listedPolicy) Validate(context.Context, module.Validator) []module.Problem { return nil }
func (listedPolicy) Needs() module.Needs                                         { return module.Needs{} }
func (listedPolicy) Evaluate(context.Context, *module.Input) (*module.Result, error) {
	return &module.Result{}, nil
}

func TestValidator_KnowsAnchorUsernames(t *testing.T) {
	r := &receive{deps: Deps{API: &fakeAPI{}}, req: Request{Env: hookenv.Env{ProjectID: 1, ProjectPath: "a/b/c"}}}
	r.input = r.newInput()
	v := r.validatorFor(&loaded{policy: listedPolicy{users: []string{"Alice"}}})
	assert.True(t, v.Known("alice"), "usernames of the anchor version are not checked again (D28)")
	assert.False(t, v.Known("bob"))
	assert.False(t, r.validatorFor(&loaded{}).Known("alice"))
}

func TestServiceAccountCheck(t *testing.T) {
	tests := []struct {
		name   string
		api    *fakeAPI
		reason string
	}{
		{"not visible", &fakeAPI{projectErr: &gitlab.Error{Kind: gitlab.NotFound}}, "project_not_visible"},
		{"developer", &fakeAPI{projectLevel: 30}, "role_below_maintainer"},
		{"API down", &fakeAPI{projectErr: &gitlab.Error{Kind: gitlab.Unavailable}}, "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &receive{deps: Deps{API: tt.api, Clock: time.Now}, req: Request{Env: hookenv.Env{ProjectID: 1, ProjectPath: "a/b/c"}}}
			for range 2 {
				st, stop := r.guard(context.Background())
				assert.True(t, stop)
				assert.Contains(t, st.Reason(), tt.reason)
			}
			assert.Equal(t, 1, tt.api.projectCalls, "checked once per receive")
			if tt.reason != "unavailable" {
				require.Len(t, r.events, 1, "the alert is an audit event")
				assert.Equal(t, audit.EventAuthorizationBackendFailure, r.events[0].Event)
				assert.Equal(t, tt.reason, r.events[0].Reason)
			}
		})
	}
	ok := &receive{deps: Deps{API: &fakeAPI{}, Clock: time.Now}, req: Request{Env: hookenv.Env{ProjectID: 1, ProjectPath: "a/b/c"}}}
	_, stop := ok.guard(context.Background())
	assert.False(t, stop)
}
