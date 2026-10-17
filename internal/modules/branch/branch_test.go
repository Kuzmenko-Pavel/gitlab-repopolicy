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

// standard is a branch standard as an installation would put it into
// branchpolicy_presets.
const standard = `rules:
  - name: release
    match: "release/*"
    pattern: '^release/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$'
    examples:
      valid: [release/v1.4.0, release/v1.4.0-staging]
      invalid: [release/foo, release/v1.2, release/v01.2.3]
    protected: true
    message: Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
  - name: hotfix
    match: "hotfix/*"
    pattern: '^hotfix/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$'
    examples:
      valid: [hotfix/v1.4.1]
      invalid: [hotfix/foo, hotfix/v1.4]
    protected: true
  - name: epic
    match: "epic/*"
    pattern: '^epic/[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$'
    examples:
      valid: [epic/payments]
      invalid: [epic/, epic/Payments]
    protected: true
  - name: demo
    match: "demo/*"
    pattern: '^demo/[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$'
    protected: true
  - name: work
    match: "*"
    pattern: '^[a-z0-9][a-z0-9._-]*/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)(/[a-z0-9][a-z0-9._-]*)*|[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*)$'
    examples:
      valid: [kpn/disable_auth, gks/INFRA-1719/change_logic, dmitry.mashoshin/123, dmitry.mashoshin/355/change_proce/auth]
      invalid: [kpn, kpn/, 123-fix-login, Kuzmenko.Pavel-main-patch-48213, revert-1a2b3c4d]
    message: Work branches are <prefix>/[<issue>/]<name>, for example kpn/disable_auth.
`

func settings(t *testing.T) *Settings {
	t.Helper()
	tree, err := strictyaml.DecodeTree([]byte("org-branches:\n"+indent(standard)), module.DefaultMaxPolicySize)
	require.NoError(t, err)
	raw, ok := tree.Interface().(map[string]any)
	require.True(t, ok)
	s := NewSettings(raw)
	require.Empty(t, s.presetErrors)
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
		"dmitry.mashoshin/355/change_proce/auth", "kpn/bump-1.2.3", "release/v1.2.3", "release/v10.0.12-staging", "epic/payments"} {
		res := protectedEnv().create(t, p, name)
		assert.Equal(t, module.Allow, res.Decision, name)
		assert.False(t, res.Touched, "an accepted branch writes no audit event: %s", name)
	}
	for _, name := range []string{"release/foo", "release/v1.2", "release/v01.2.3", "master", "kpn", "123-fix-login",
		"Kuzmenko.Pavel-main-patch-48213", "revert-1a2b3c4d", "Release/v1.0.0"} {
		res := protectedEnv().create(t, p, name)
		assert.Equal(t, module.Deny, res.Decision, name)
		assert.Equal(t, []string{module.CodeBranchName}, codes(res), name)
	}
	res := protectedEnv().create(t, p, "release/foo")
	assert.Equal(t, "release", res.Violations[0].Rule, "a release/ name is judged by the release rule only")
	assert.Contains(t, res.Violations[0].Message, "Release branches look like release/v1.4.0")
	assert.Contains(t, res.Violations[0].Summary, "branch release/foo breaks BRANCHPOLICY rule release")
	assert.NotContains(t, res.Violations[0].Summary, "^release", "the regex is not shown")
}

func TestDefaultUpdateDelete(t *testing.T) {
	p := parse(t, settings(t), "version: 1\nextends: org-branches\n")
	e := protectedEnv()
	res := e.create(t, p, "main")
	assert.Equal(t, module.Allow, res.Decision, "the default branch is the business of the core")
	res = e.run(t, p, module.RefUpdate{Old: strings.Repeat("a", 40), New: strings.Repeat("b", 40), Ref: "refs/heads/Bad_Name"},
		module.RefUpdate{Old: strings.Repeat("a", 40), New: zero, Ref: "refs/heads/Old_Name"},
		module.RefUpdate{Old: zero, New: strings.Repeat("a", 40), Ref: "refs/tags/Any"})
	assert.Equal(t, module.Allow, res.Decision, "updates, deletions and tags are not checked")
}

func TestMerge(t *testing.T) {
	s := settings(t)
	p := parse(t, s, `version: 1
extends: org-branches
rules:
  - name: no-master
    match: "master"
    deny: true
    message: This project uses main; master must not be created.
  - name: no-tmp
    match: "tmp/*"
    deny: true
  - name: work
    match: "*"
    deny: true
    message: Only release, hotfix, epic and demo branches here.
`)
	require.NoError(t, p.unusable)
	names := make([]string, 0, len(p.rules))
	for _, r := range p.rules {
		names = append(names, r.name)
	}
	assert.Equal(t, []string{"no-master", "no-tmp", "release", "hotfix", "epic", "demo", "work"}, names,
		"new rules first, the preset rule replaced in its place")
	e := protectedEnv()
	res := e.create(t, p, "master")
	require.Equal(t, []string{module.CodeBranchName}, codes(res))
	assert.Equal(t, "creating this branch is forbidden", res.Violations[0].Title)
	assert.Contains(t, res.Violations[0].Summary, "branch master is forbidden by BRANCHPOLICY rule no-master")
	assert.Equal(t, module.Deny, e.create(t, p, "kpn/disable_auth").Decision, "the strict project replaced the catch-all")
	assert.Equal(t, module.Allow, e.create(t, p, "release/v1.0.0").Decision)

	own := parse(t, s, "version: 1\nrules:\n  - name: feature\n    match: \"feature/*\"\n")
	assert.Equal(t, module.Allow, e.create(t, own, "feature/x").Decision)
	res = e.create(t, own, "kpn/x")
	assert.Equal(t, []string{module.CodeBranchName}, codes(res), "without a catch-all a name of no class is denied")
	assert.Equal(t, "no rule of BRANCHPOLICY allows this name", res.Violations[0].Title)
}

func TestCheckFinalList(t *testing.T) {
	s := settings(t)
	for name, tc := range map[string]struct{ file, want string }{
		"rule after catch-all": {"version: 1\nrules:\n  - name: all\n    match: \"*\"\n  - name: late\n    match: \"x/*\"\n",
			`rule late: comes after the rule all with match "*"`},
		"duplicate match": {"version: 1\nrules:\n  - name: a\n    match: \"x/*\"\n  - name: b\n    match: \"x/*\"\n",
			`rule b: match "x/*" is already the match of rule a`},
		"shadowed example": {"version: 1\nextends: org-branches\nrules:\n  - name: two-levels\n    match: \"*/*\"\n",
			`rule release: the valid example "release/v1.4.0" is taken by the rule two-levels above`},
		"valid example fails": {"version: 1\nrules:\n  - name: r\n    match: \"r/*\"\n    pattern: 'r/[0-9]+'\n    examples: {valid: [r/x]}\n",
			`rule r: pattern does not match the valid example "r/x"`},
		"invalid example passes": {"version: 1\nrules:\n  - name: r\n    match: \"r/*\"\n    pattern: 'r/[0-9]+'\n    examples: {invalid: [r/1]}\n",
			`rule r: pattern matches the invalid example "r/1"`},
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
	for name, file := range map[string]string{
		"deny with pattern":    "version: 1\nrules:\n  - name: r\n    match: x\n    deny: true\n    pattern: x\n",
		"deny with protected":  "version: 1\nrules:\n  - name: r\n    match: x\n    deny: true\n    protected: true\n",
		"examples no pattern":  "version: 1\nrules:\n  - name: r\n    match: x\n    examples: {valid: [x]}\n",
		"unknown field":        "version: 1\nrules:\n  - name: r\n    match: x\n    branches: [x]\n",
		"bad regex":            "version: 1\nrules:\n  - name: r\n    match: x\n    pattern: '('\n",
		"no match":             "version: 1\nrules:\n  - name: r\n",
		"protected not a bool": "version: 1\nrules:\n  - name: r\n    match: x\n    protected: \"yes\"\n",
	} {
		_, err := Parse([]byte(file), settings(t))
		assert.Error(t, err, name)
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
	assert.Contains(t, res.Violations[0].Summary, "a Maintainer adds epic/* in Settings > Repository > Protected branches")

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
	p := parse(t, settings(t), "version: 1\nrules:\n  - name: work\n    match: \"*\"\n    user_prefix: true\n")
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
	assert.Contains(t, e.create(t, p, "kpn/x").Violations[0].Facts[0], "kpn is not a GitLab user")

	e.authz.status = module.Unknown("no answer")
	res := e.create(t, p, "olga/x")
	assert.Equal(t, module.Warn, res.Decision)
	assert.Equal(t, []string{module.CodeAuthorizationBackend}, codes(res))
}

func TestModule(t *testing.T) {
	m := New(NewSettings(map[string]any{"broken": "x"}))
	require.Len(t, m.PresetErrors(), 1)
	assert.Contains(t, m.PresetErrors()[0], "branchpolicy_presets.broken: ")
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
	assert.Equal(t, []string{"release/*", "hotfix/*", "epic/*", "demo/*"}, p.ProtectedBranches())
}

// TestDenyByDefault is the example of the documentation: without a catch-all
// a name of no class is denied.
func TestDenyByDefault(t *testing.T) {
	p := parse(t, NewSettings(nil), `version: 1
rules:
  - name: no-master
    match: "master"
    deny: true
  - name: release
    match: "release/*"
    pattern: '^release/v[0-9]+\.[0-9]+\.[0-9]+(-[a-z][a-z0-9]*)?$'
    protected: true
  - name: hotfix
    match: "hotfix/*"
    pattern: '^hotfix/v[0-9]+\.[0-9]+\.[0-9]+(-[a-z][a-z0-9]*)?$'
    protected: true
  - name: user
    match: "*/*"
    user_prefix: true
`)
	e := protectedEnv()
	e.user = "jdoe"
	for name, want := range map[string]module.Decision{
		"master": module.Deny, "release/v1.4.0": module.Allow, "release/foo": module.Deny,
		"jdoe/disable_auth": module.Allow, "fix-login": module.Deny, "tmp": module.Deny,
	} {
		assert.Equal(t, want, e.create(t, p, name).Decision, name)
	}
	assert.Equal(t, "release", e.create(t, p, "release/foo").Violations[0].Rule, "release/foo does not fall through to user")
}
