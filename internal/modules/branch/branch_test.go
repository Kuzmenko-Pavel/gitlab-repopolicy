package branch

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// standard is a branch standard as an installation would put it into the
// presets of the server config: an exact rule and a deny rule per family.
const standard = `rules:
  release:
    match: ['/^release\/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$/']
    priority: 100
    protected: true
    message: Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
  release-bad:
    match: ['/^release\//']
    priority: 110
    deny: true
    message: Release branches look like release/v1.4.0.
  hotfix:
    match: ['/^hotfix\/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$/']
    priority: 200
    protected: true
    message: Hotfix branches look like hotfix/v1.4.1.
  hotfix-bad:
    match: ['/^hotfix\//']
    priority: 210
    deny: true
    message: Hotfix branches look like hotfix/v1.4.1.
  epic:
    match: ['/^epic\/[a-z0-9][a-z0-9._-]*(\/[a-z0-9][a-z0-9._-]*)*$/']
    priority: 300
    protected: true
    message: Epic branches look like epic/payments.
  demo:
    match: ['/^demo\/[a-z0-9][a-z0-9._-]*(\/[a-z0-9][a-z0-9._-]*)*$/']
    priority: 400
    protected: true
    message: Demo branches look like demo/client-a.
  work:
    match: ['/^[a-z0-9][a-z0-9._-]*\/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)(\/[a-z0-9][a-z0-9._-]*)*|[a-z0-9][a-z0-9._-]*(\/[a-z0-9][a-z0-9._-]*)*)$/']
    priority: 900
    message: Work branches are <prefix>/[<issue>/]<name>, for example kpn/disable_auth.
`

func settings(t *testing.T) *Settings {
	t.Helper()
	tree, err := strictyaml.DecodeTree([]byte("org-branches:\n"+indent(standard)), module.DefaultMaxPolicySize)
	require.NoError(t, err)
	raw, ok := tree.Interface().(map[string]any)
	require.True(t, ok)
	s := NewSettings(raw)
	require.Empty(t, s.presets.Errors())
	return s
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n  ") + "\n"
}

func parse(t *testing.T, s *Settings, file string) *Policy {
	t.Helper()
	p, err := Parse([]byte(file), s)
	require.NoError(t, err)
	return p
}

type fakeRefs struct {
	class  module.RefClass
	status module.Status
	asked  int
}

func (r *fakeRefs) Default() string { return "refs/heads/main" }
func (r *fakeRefs) Class(context.Context, string) (module.RefClass, module.Status) {
	r.asked++
	return r.class, r.status
}
func (r *fakeRefs) ProtectedScope(context.Context) (module.Scope, module.Status) {
	return module.Scope{}, r.status
}

type fakeAuthz struct {
	users  map[string]module.User
	status module.Status
	asked  int
}

func (a *fakeAuthz) PusherAccess(context.Context) (int, module.Status) { return 0, module.Confirmed() }
func (a *fakeAuthz) AccessLevels(context.Context, []int64) (map[int64]int, module.Status) {
	return nil, module.Confirmed()
}
func (a *fakeAuthz) Users(_ context.Context, names []string) (map[string]module.User, module.Status) {
	a.asked++
	out := map[string]module.User{}
	for _, n := range names {
		if u, ok := a.users[strings.ToLower(n)]; ok {
			out[strings.ToLower(n)] = u
		}
	}
	return out, a.status
}

type env struct {
	refs  *fakeRefs
	authz *fakeAuthz
	user  string
}

func protectedEnv() *env {
	return &env{refs: &fakeRefs{class: module.ClassProtected, status: module.Confirmed()},
		authz: &fakeAuthz{status: module.Confirmed()}, user: "alice"}
}

var zero = strings.Repeat("0", 40)

// create evaluates the creation of the branches by the user of e.
func (e *env) create(t *testing.T, p *Policy, names ...string) *module.Result {
	t.Helper()
	updates := make([]module.RefUpdate, 0, len(names))
	for _, n := range names {
		updates = append(updates, module.RefUpdate{Old: zero, New: strings.Repeat("a", 40), Ref: "refs/heads/" + n})
	}
	return e.run(t, p, updates...)
}

func (e *env) run(t *testing.T, p *Policy, updates ...module.RefUpdate) *module.Result {
	t.Helper()
	in := &module.Input{Updates: updates, Refs: e.refs, Authz: e.authz,
		Principal: module.Principal{GLID: "user-2", Username: e.user}}
	res, err := p.Evaluate(context.Background(), in)
	require.NoError(t, err)
	return res
}

func codes(res *module.Result) []string {
	out := make([]string, 0, len(res.Violations))
	for _, v := range res.Violations {
		out = append(out, v.Code)
	}
	return out
}

func TestStandard_Names(t *testing.T) {
	p := parse(t, settings(t), "version: 1\nextends: org-branches\n")
	require.NoError(t, p.unusable)
	for _, name := range []string{"kpn/disable_auth", "gks/INFRA-1719/change_logic", "dmitry.mashoshin/123",
		"dmitry.mashoshin/355/change_proce/auth", "kpn/bump-1.2.3", "release/v1.2.3", "release/v10.0.12-staging", "epic/payments",
		"master", "kpn", "123-fix-login", "Release/v1.0.0"} {
		res := protectedEnv().create(t, p, name)
		assert.Equal(t, module.Allow, res.Decision, name)
		assert.False(t, res.Touched, "an accepted branch writes no audit event: %s", name)
	}
	for name, rule := range map[string]string{"release/foo": "release-bad", "release/v1.2": "release-bad",
		"release/v01.2.3": "release-bad", "hotfix/foo": "hotfix-bad"} {
		res := protectedEnv().create(t, p, name)
		assert.Equal(t, module.Deny, res.Decision, name)
		assert.Equal(t, []string{module.CodeBranchName}, codes(res), name)
		assert.Equal(t, rule, res.Violations[0].Rule, "the deny rule of the family: %s", name)
	}
	res := protectedEnv().create(t, p, "release/foo")
	assert.Equal(t, "Release branches look like release/v1.4.0.", res.Violations[0].Message)
	assert.Equal(t, []string{"preset:org-branches"}, res.Violations[0].Source)
}

func TestDefaultUpdateDelete(t *testing.T) {
	p := parse(t, settings(t), "version: 1\nrules:\n  all:\n    match: all\n    priority: 1\n    deny: true\n    message: m\n")
	e := protectedEnv()
	res := e.create(t, p, "main")
	assert.Equal(t, module.Allow, res.Decision, "the default branch is the business of the core")
	res = e.run(t, p, module.RefUpdate{Old: strings.Repeat("a", 40), New: strings.Repeat("b", 40), Ref: "refs/heads/Bad_Name"},
		module.RefUpdate{Old: strings.Repeat("a", 40), New: zero, Ref: "refs/heads/Old_Name"},
		module.RefUpdate{Old: zero, New: strings.Repeat("a", 40), Ref: "refs/tags/Any"})
	assert.Equal(t, module.Allow, res.Decision, "updates, deletions and tags are not checked")
}

func TestPresetOverride(t *testing.T) {
	s := settings(t)
	p := parse(t, s, `version: 1
extends: org-branches
rules:
  no-master:
    match: [master]
    priority: 10
    deny: true
    message: This project uses main; master must not be created.
  epic: null
  release:
    message: Release branches of this project look like release/v2.0.0.
`)
	require.NoError(t, p.unusable)
	names := make([]string, 0, len(p.rules))
	for _, r := range p.rules {
		names = append(names, r.name)
	}
	assert.Equal(t, []string{"no-master", "release", "release-bad", "hotfix", "hotfix-bad", "demo", "work"}, names, "in priority order")
	e := protectedEnv()
	res := e.create(t, p, "master")
	require.Equal(t, []string{module.CodeBranchName}, codes(res))
	assert.Equal(t, "creating this branch is forbidden", res.Violations[0].Title)
	assert.Contains(t, res.Violations[0].Summary, "branch master is forbidden by BRANCHPOLICY rule no-master")
	assert.Equal(t, module.Allow, e.create(t, p, "epic/Payments").Decision, "the epic rule is removed")
	e.refs.class = module.ClassOther
	res = e.create(t, p, "release/v1.0.0")
	require.Equal(t, []string{module.CodeBranchNotProtected}, codes(res))
	assert.Equal(t, "Release branches of this project look like release/v2.0.0.", res.Violations[0].Message)
	assert.Equal(t, []string{"preset:org-branches", "file"}, res.Violations[0].Source)
}

// TestDenyWithChecksOff: false turns a check off, so it goes with deny: true,
// also over a true of the preset; an active true is a conflict after merging.
func TestDenyWithChecksOff(t *testing.T) {
	s := settings(t)
	p := parse(t, s, "version: 1\nrules:\n  r: {match: [x], priority: 1, deny: true, protected: false, user_prefix: false, message: m}\n")
	require.NoError(t, p.unusable)
	assert.Equal(t, []string{module.CodeBranchName}, codes(protectedEnv().create(t, p, "x")))

	p = parse(t, s, "version: 1\nextends: org-branches\nrules:\n  release:\n    deny: true\n    protected: false\n")
	require.NoError(t, p.unusable)
	assert.Equal(t, []string{module.CodeBranchName}, codes(protectedEnv().create(t, p, "release/v1.0.0")),
		"the file denies the class and turns off the protection of the preset")

	_, err := Parse([]byte("version: 1\nextends: org-branches\nrules:\n  release:\n    deny: true\n"), s)
	assert.ErrorContains(t, err, "rule release: protected: true is not allowed with deny: true", "the true of the preset stays active")
}

func TestStrictMode(t *testing.T) {
	p := parse(t, settings(t), `version: 1
extends: org-branches
rules:
  other:
    match: all
    priority: 1000
    deny: true
    message: Only the branches of the standard are created here.
`)
	require.NoError(t, p.unusable)
	e := protectedEnv()
	assert.Equal(t, module.Allow, e.create(t, p, "kpn/disable_auth").Decision)
	res := e.create(t, p, "fix-login")
	assert.Equal(t, []string{module.CodeBranchName}, codes(res), "match: all with deny catches a name of no class")
	assert.Equal(t, "other", res.Violations[0].Rule)
}

func TestNoClassNotChecked(t *testing.T) {
	p := parse(t, settings(t), `version: 1
rules:
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/', '!release/v0.0.0']
    priority: 1
    protected: true
    message: m
`)
	e := protectedEnv()
	e.refs.class = module.ClassOther
	for _, name := range []string{"release/v0.0.0", "release/foo", "feature/x"} {
		res := e.create(t, p, name)
		assert.Equal(t, module.Allow, res.Decision, "%s matches no rule: not checked, an exclusion does not deny", name)
		assert.False(t, res.Touched)
	}
	assert.Equal(t, []string{module.CodeBranchNotProtected}, codes(e.create(t, p, "release/v1.0.0")))
}

func TestCheckMerged(t *testing.T) {
	s := settings(t)
	for name, tc := range map[string]struct{ file, want string }{
		"priority twice": {"version: 1\nrules:\n  a: {match: [a], priority: 1, message: m}\n  b: {match: [b], priority: 1, message: m}\n",
			"rule b: priority 1 is also the priority of rule a"},
		"rule after match all": {"version: 1\nrules:\n  all: {match: all, priority: 1, message: m}\n  late: {match: [x], priority: 2, message: m}\n",
			"rule late: comes after rule all with match: all and is never used; give match: all the highest priority"},
		"same match": {"version: 1\nrules:\n  a: {match: [x, y], priority: 1, message: m}\n  b: {match: [y, x], priority: 2, message: m}\n",
			"rule b: match [y, x] is the match of rule a with a lower priority, so it is never used"},
		"preset priority taken": {"version: 1\nextends: org-branches\nrules:\n  own: {match: [own], priority: 100, message: m}\n",
			"priority 100 is also the priority of rule"},
		"missing preset": {"version: 1\nextends: nope\n", `extends: preset "nope" is not defined`},
	} {
		t.Run(name, func(t *testing.T) {
			p := parse(t, s, tc.file)
			problems := p.Validate(context.Background(), nil)
			require.NotEmpty(t, problems)
			assert.Contains(t, problems[0].Msg, tc.want)
			res := protectedEnv().create(t, p, "anything/x")
			assert.Equal(t, module.Warn, res.Decision, "an unusable policy is skipped with a warning")
			assert.Equal(t, []string{module.CodeInvalidPolicy}, codes(res))
			assert.Equal(t, module.Allow, protectedEnv().run(t, p).Decision, "a push without a new branch is not warned")
		})
	}
}

func TestParse_Invalid(t *testing.T) {
	for name, tc := range map[string]struct{ file, want string }{
		"deny with protected":  {"version: 1\nrules:\n  r: {match: [x], priority: 1, deny: true, protected: true, message: m}\n", "protected: true is not allowed with deny: true"},
		"deny with prefix":     {"version: 1\nrules:\n  r: {match: [x], priority: 1, deny: true, user_prefix: true, message: m}\n", "user_prefix: true is not allowed with deny: true"},
		"pattern is gone":      {"version: 1\nrules:\n  r: {match: [x], priority: 1, pattern: x, message: m}\n", `unknown field "pattern"`},
		"examples are gone":    {"version: 1\nrules:\n  r: {match: [x], priority: 1, examples: {valid: [x]}, message: m}\n", `unknown field "examples"`},
		"no match":             {"version: 1\nrules:\n  r: {priority: 1, message: m}\n", "match: required"},
		"no priority":          {"version: 1\nrules:\n  r: {match: [x], message: m}\n", "priority: required"},
		"no message":           {"version: 1\nrules:\n  r: {match: [x], priority: 1}\n", "message: required"},
		"default":              {"version: 1\nrules:\n  r: {match: default, priority: 1, message: m}\n", "default is not allowed here"},
		"glob":                 {"version: 1\nrules:\n  r: {match: ['release/*'], priority: 1, message: m}\n", `use a regex such as /^release\//`},
		"protected not a bool": {"version: 1\nrules:\n  r: {match: [x], priority: 1, protected: \"yes\", message: m}\n", "expected true or false"},
	} {
		_, err := Parse([]byte(tc.file), settings(t))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tc.want, name)
	}
}

func TestWarnMode(t *testing.T) {
	p := parse(t, settings(t), "version: 1\nextends: org-branches\nmode: warn\n")
	res := protectedEnv().create(t, p, "release/foo")
	assert.Equal(t, module.Warn, res.Decision)
	assert.Equal(t, []string{module.CodeBranchName}, codes(res))
	assert.True(t, res.Touched)
}

func TestProtected(t *testing.T) {
	p := parse(t, settings(t), "version: 1\nextends: org-branches\n")
	e := protectedEnv()
	e.refs.class = module.ClassOther
	res := e.create(t, p, "epic/payments")
	require.Equal(t, []string{module.CodeBranchNotProtected}, codes(res))
	assert.Equal(t, module.Deny, res.Decision)
	assert.Equal(t, "Epic branches look like epic/payments.", res.Violations[0].Message)
	assert.Contains(t, res.Violations[0].Hint, "a Maintainer protects epic/payments in Settings > Repository > Protected branches")

	for st, code := range map[module.Status]string{module.Unknown("no answer"): module.CodeAuthorizationBackend, module.UnknownLimit("budget"): module.CodeLimitExceeded} {
		e := protectedEnv()
		e.refs.status = st
		res := e.create(t, p, "epic/payments")
		assert.Equal(t, module.Warn, res.Decision)
		assert.Equal(t, []string{code}, codes(res))
	}

	e = protectedEnv()
	e.create(t, p, "kpn/x")
	assert.Zero(t, e.refs.asked, "a class without protected does not ask GitLab")
}

func TestUserPrefix(t *testing.T) {
	p := parse(t, settings(t), "version: 1\nrules:\n  work:\n    match: all\n    priority: 1\n    user_prefix: true\n    message: m\n    help: {contact: \"#branches\"}\n")
	e := protectedEnv()
	e.user = "Kuzmenko.Pavel"
	assert.Equal(t, module.Allow, e.create(t, p, "kuzmenko.pavel/disable_auth").Decision)
	assert.Zero(t, e.authz.asked, "the own username of the pusher needs no API")

	e.authz.users = map[string]module.User{"olga": {ID: 3, Active: true}, "gone": {ID: 4}}
	assert.Equal(t, module.Allow, e.create(t, p, "olga/fix").Decision, "a branch for another existing user")
	assert.Equal(t, 1, e.authz.asked)
	for _, name := range []string{"kpn/disable_auth", "gone/x", "nouser"} {
		res := e.create(t, p, name)
		assert.Equal(t, []string{module.CodeBranchName}, codes(res), name)
	}
	res := e.create(t, p, "kpn/x")
	assert.Contains(t, res.Violations[0].Facts[0], "kpn is not a GitLab user")
	assert.Equal(t, []module.Help{{Contact: "#branches"}}, res.Violations[0].Help)

	e.authz.status = module.Unknown("no answer")
	res = e.create(t, p, "olga/x")
	assert.Equal(t, module.Warn, res.Decision)
	assert.Equal(t, []string{module.CodeAuthorizationBackend}, codes(res))
}

func TestModule(t *testing.T) {
	m := New(NewSettings(map[string]any{"broken": "x"}))
	assert.Equal(t, []string{"presets.branchpolicy.broken: expected a mapping, got a string"}, m.PresetErrors())
	res, err := m.EvaluateInvalid(context.Background(), &module.Input{Refs: &fakeRefs{}}, assert.AnError)
	require.NoError(t, err)
	assert.Equal(t, module.Allow, res.Decision, "no new branch, no warning")
	res, err = m.EvaluateInvalid(context.Background(), &module.Input{Refs: &fakeRefs{},
		Updates: []module.RefUpdate{{Old: zero, New: strings.Repeat("a", 40), Ref: "refs/heads/x"}}}, assert.AnError)
	require.NoError(t, err)
	assert.Equal(t, module.Warn, res.Decision)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "invalid_branch_policy", res.Events[0].Event)

	p := parse(t, settings(t), "version: 1\nextends: org-branches\n")
	assert.True(t, p.ProtectsBranches())
	assert.True(t, p.MustProtect("release/v1.0.0"))
	assert.False(t, p.MustProtect("release/foo"), "the deny rule of the family has no protected")
	assert.False(t, p.MustProtect("kpn/x"))
	assert.False(t, parse(t, nil, "version: 1\n").ProtectsBranches())
}

// TestUnusable_NoAnswers: rules that cannot be used answer no rule and no
// protection, so validate and settings-check do not act on them.
func TestUnusable_NoAnswers(t *testing.T) {
	p, err := Parse([]byte("version: 1\nextends: missing\n"), nil)
	require.NoError(t, err)
	_, ok := p.RuleFor("release/v1.0.0")
	assert.False(t, ok)
	assert.False(t, p.MustProtect("release/v1.0.0"))
	assert.False(t, p.ProtectsBranches())
	assert.NotEmpty(t, p.Validate(context.Background(), nil))
}
