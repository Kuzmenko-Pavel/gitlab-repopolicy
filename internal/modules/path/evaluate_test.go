package path

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

type fakeChanges struct {
	paths    map[string][]string // by ref
	scopes   []module.Scope
	err      error
	changes  []module.PathChange
	known    map[string][]module.PathState
	stateErr error
	knownErr error
}

func (f *fakeChanges) CandidatePaths(_ context.Context, u module.RefUpdate) ([]string, error) {
	return f.paths[u.Ref], f.err
}

func (f *fakeChanges) Paths(_ context.Context, u module.RefUpdate, s module.Scope) ([]string, error) {
	f.scopes = append(f.scopes, s)
	return f.paths[u.Ref], f.err
}

func (*fakeChanges) CandidateCommits(context.Context, module.RefUpdate, int) ([]string, error) {
	return nil, nil
}

func (*fakeChanges) EnteringCommits(context.Context, module.RefUpdate, module.Scope, int) ([]string, error) {
	return nil, nil
}
func (*fakeChanges) NewCommits(context.Context, module.RefUpdate, int) ([]string, error) {
	return nil, nil
}

func (f *fakeChanges) NewPathChanges(context.Context, module.RefUpdate) ([]module.PathChange, error) {
	return f.changes, f.stateErr
}

func (f *fakeChanges) KnownStates(context.Context, module.RefUpdate, []string) (map[string][]module.PathState, error) {
	if f.knownErr != nil {
		return nil, f.knownErr
	}
	return f.known, f.stateErr
}

type fakeRefs struct {
	class  module.RefClass
	status module.Status
}

func (*fakeRefs) Default() string { return "refs/heads/main" }
func (r *fakeRefs) Class(context.Context, string) (module.RefClass, module.Status) {
	return r.class, r.status
}
func (r *fakeRefs) ProtectedScope(context.Context) (module.Scope, module.Status) {
	return module.Scope{Class: module.ClassProtected, Key: "gitlab"}, r.status
}

type fakePusher struct {
	person module.Person
	status module.Status
}

func (p fakePusher) Person(context.Context) (module.Person, module.Status) { return p.person, p.status }

type fakeAuthz struct {
	pusher     int
	pusherSt   module.Status
	users      map[string]module.User
	usersSt    module.Status
	levels     map[int64]int
	levelsSt   module.Status
	known      map[string]bool
	usersCalls int
}

func (a *fakeAuthz) PusherAccess(context.Context) (int, module.Status) { return a.pusher, a.pusherSt }
func (a *fakeAuthz) Users(_ context.Context, names []string) (map[string]module.User, module.Status) {
	a.usersCalls++
	out := map[string]module.User{}
	for _, n := range names {
		if u, ok := a.users[strings.ToLower(n)]; ok {
			out[strings.ToLower(n)] = u
		}
	}
	return out, a.usersSt
}
func (a *fakeAuthz) AccessLevels(_ context.Context, ids []int64) (map[int64]int, module.Status) {
	out := map[int64]int{}
	for _, id := range ids {
		out[id] = a.levels[id]
	}
	return out, a.levelsSt
}
func (a *fakeAuthz) Known(u string) bool { return a.known[strings.ToLower(u)] }

var ok = module.Confirmed()

// team: admins platform-lead (1) and sre-lead (2) are Maintainers, alice (3) a Developer.
func team() *fakeAuthz {
	return &fakeAuthz{pusherSt: ok, usersSt: ok, levelsSt: ok,
		users:  map[string]module.User{"platform-lead": {ID: 1, Active: true}, "sre-lead": {ID: 2, Active: true}, "alice": {ID: 3, Active: true}},
		levels: map[int64]int{1: 40, 2: 50, 3: 30}}
}

type env struct {
	in    *module.Input
	authz *fakeAuthz
}

func newEnv(user string, paths map[string][]string) *env {
	a := team()
	in := &module.Input{
		Changes:    &fakeChanges{paths: paths},
		Refs:       &fakeRefs{class: module.ClassOther, status: ok},
		Principal:  module.Principal{GLID: "user-9", Username: user},
		Principals: fakePusher{person: module.Person{Username: user, ID: 9}, status: ok},
		Authz:      a,
	}
	for ref := range paths {
		in.Updates = append(in.Updates, module.RefUpdate{Old: strings.Repeat("a", 40), New: strings.Repeat("b", 40), Ref: ref})
	}
	return &env{in: in, authz: a}
}

func (e *env) evaluate(t *testing.T, text string) *module.Result {
	t.Helper()
	p, err := New(nil).Parse([]byte(text))
	require.NoError(t, err)
	res, err := p.Evaluate(context.Background(), e.in)
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

func TestEvaluate_RuleAllowsAndDenies(t *testing.T) {
	e := newEnv("ci-maintainer", map[string][]string{"refs/heads/main": {".gitlab-ci.yml", "src/app.go"}})
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, module.Allow, res.Decision)
	assert.True(t, res.Touched)

	e = newEnv("CI-Maintainer", map[string][]string{"refs/heads/main": {".gitlab-ci.yml"}})
	assert.Equal(t, module.Allow, e.evaluate(t, basePolicy).Decision, "usernames are compared case-insensitively")

	e = newEnv("bob", map[string][]string{"refs/heads/main": {".gitlab-ci.yml", "scripts/deploy/run.sh", "src/app.go"}})
	res = e.evaluate(t, basePolicy)
	assert.Equal(t, module.Deny, res.Decision)
	require.Len(t, res.Violations, 1)
	v := res.Violations[0]
	assert.Equal(t, module.CodeProtectedPath, v.Code)
	assert.Equal(t, "ci-config", v.Rule)
	assert.Equal(t, []module.Item{{Name: ".gitlab-ci.yml"}, {Name: "scripts/deploy/run.sh"}}, v.Items)
	assert.Equal(t, "CI/CD configuration is changed by the platform team.", v.Message)
	assert.Equal(t, "not in allow: bob", v.Reason)
}

func TestEvaluate_UnmatchedPathAllowed(t *testing.T) {
	e := newEnv("bob", map[string][]string{"refs/heads/main": {"src/app.go", "README.md"}})
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, module.Allow, res.Decision)
	assert.False(t, res.Touched, "PATHPOLICY is not an allowlist and the push is not audited")
}

func TestEvaluate_BotByUsername(t *testing.T) {
	policy := "version: 1\nadmins: [a]\nrules:\n  r: {match: all, paths: [deploy/**], allow: [project_1_bot_e42782], message: m}\n"
	e := newEnv("project_1_bot_e42782", map[string][]string{"refs/heads/main": {"deploy/x"}})
	assert.Equal(t, module.Allow, e.evaluate(t, policy).Decision)
}

func TestMatch_NestedRulesAnd(t *testing.T) {
	policy := `version: 1
admins: [a]
rules:
  helm: {match: all, paths: ["helm/**"], allow: [alice, bob], message: m}
  prod: {match: all, paths: ["helm/**/values-prod.yaml"], allow: [bob], message: m}
`
	e := newEnv("alice", map[string][]string{"refs/heads/main": {"helm/app/values-prod.yaml"}})
	res := e.evaluate(t, policy)
	assert.Equal(t, []string{module.CodeProtectedPath}, codes(res), "every matching rule must be satisfied")
	assert.Equal(t, "prod", res.Violations[0].Rule)
	e = newEnv("bob", map[string][]string{"refs/heads/main": {"helm/app/values-prod.yaml"}})
	assert.Equal(t, module.Allow, e.evaluate(t, policy).Decision)
}

func TestEvaluate_UnknownPrincipal(t *testing.T) {
	e := newEnv("alice", map[string][]string{"refs/heads/main": {"COMMITPOLICY"}})
	e.in.Principals = fakePusher{status: ok}
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, []string{module.CodeUnknownPrincipal}, codes(res), "a deploy key is not a person")
	require.Len(t, res.Events, 1)
	assert.Equal(t, "unknown_principal", res.Events[0].Event)

	e = newEnv("alice", map[string][]string{"refs/heads/main": {"src/x"}})
	e.in.Principals = fakePusher{status: ok}
	assert.Equal(t, module.Allow, e.evaluate(t, basePolicy).Decision, "unprotected paths do not need a person")
}

func TestEvaluate_MatchList(t *testing.T) {
	e := newEnv("bob", map[string][]string{"refs/heads/feature": {"helm/x/values-prod.yaml"}})
	assert.Equal(t, module.Allow, e.evaluate(t, basePolicy).Decision, "the list of prod-values does not have the feature branch")
	e.in.Refs = &fakeRefs{class: module.ClassProtected, status: ok}
	assert.Equal(t, module.Allow, e.evaluate(t, basePolicy).Decision, "a list does not follow the protected branches of GitLab")

	e = newEnv("bob", map[string][]string{"refs/heads/release/1.0": {"helm/x/values-prod.yaml"}})
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, []string{module.CodeProtectedPath}, codes(res), "a branch of the list, without asking GitLab")
	assert.Equal(t, []module.Help{{Contact: "#release"}}, res.Violations[0].Help)
	assert.Equal(t, []string{"file"}, res.Violations[0].Source)

	e = newEnv("bob", map[string][]string{"refs/heads/feature": {".gitlab-ci.yml"}})
	e.in.Refs = &fakeRefs{status: module.Unknown("503")}
	assert.Equal(t, []string{module.CodeProtectedPath}, codes(e.evaluate(t, basePolicy)), "match all does not ask GitLab about the branch")
}

func TestEvaluate_TagsNotChecked(t *testing.T) {
	e := newEnv("bob", map[string][]string{"refs/tags/v1": {".gitlab-ci.yml"}})
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, module.Allow, res.Decision, "the commit of a tag is checked as part of its branch")
	assert.False(t, res.Touched)
}

func TestEvaluate_LimitIsAnError(t *testing.T) {
	e := newEnv("bob", map[string][]string{"refs/heads/main": nil})
	e.in.Changes = &fakeChanges{err: module.ErrLimitExceeded}
	p, err := New(nil).Parse([]byte(basePolicy))
	require.NoError(t, err)
	_, err = p.Evaluate(context.Background(), e.in)
	assert.ErrorIs(t, err, module.ErrLimitExceeded, "the core turns it into limit_exceeded, fail closed")
}

func TestEvaluate_KnownContentInOrdinaryBranch(t *testing.T) {
	alice := module.PathState{Mode: "100644", OID: strings.Repeat("1", 40)}
	bob := module.PathState{Mode: "100644", OID: strings.Repeat("2", 40)}
	commit := strings.Repeat("c", 40)
	tests := []struct {
		name    string
		changes []module.PathChange
		err     error
		denied  []module.Item
		hint    string
	}{
		{name: "rebased change of an allowed person",
			changes: []module.PathChange{{Commit: commit, Path: ".gitlab-ci.yml", State: alice}}},
		{name: "new content",
			changes: []module.PathChange{{Commit: commit, Path: ".gitlab-ci.yml", State: bob}},
			denied:  []module.Item{{Name: ".gitlab-ci.yml"}},
			hint:    "the content of .gitlab-ci.yml in commit ccccccccc differs both from this branch and from main; new content is pushed by one of them"},
		{name: "same content, other mode",
			changes: []module.PathChange{{Commit: commit, Path: ".gitlab-ci.yml", State: module.PathState{Mode: "100755", OID: alice.OID}}},
			denied:  []module.Item{{Name: ".gitlab-ci.yml"}}},
		{name: "path without a change of its own",
			denied: []module.Item{{Name: ".gitlab-ci.yml"}}, hint: "ask one of them to push the change"},
		{name: "over the commit limit", err: module.ErrLimitExceeded,
			changes: []module.PathChange{{Commit: commit, Path: ".gitlab-ci.yml", State: alice}},
			denied:  []module.Item{{Name: ".gitlab-ci.yml"}}, hint: "ask one of them to push the change"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv("bob", map[string][]string{"refs/heads/feature": {".gitlab-ci.yml"}})
			e.in.Changes = &fakeChanges{paths: map[string][]string{"refs/heads/feature": {".gitlab-ci.yml"}},
				changes: tt.changes, known: map[string][]module.PathState{".gitlab-ci.yml": {alice}}, stateErr: tt.err}
			res := e.evaluate(t, basePolicy)
			if tt.denied == nil {
				assert.Equal(t, module.Allow, res.Decision)
				return
			}
			require.Len(t, res.Violations, 1)
			assert.Equal(t, tt.denied, res.Violations[0].Items)
			if tt.hint != "" {
				assert.Contains(t, res.Violations[0].Hint, tt.hint)
			}
		})
	}
}

// TestEvaluate_CheckHistory: check_history, not match or the class of the
// branch, decides which commits count and whether known content is exempt.
func TestEvaluate_CheckHistory(t *testing.T) {
	alice := module.PathState{Mode: "100644", OID: strings.Repeat("1", 40)}
	run := func(policy, ref string) (*module.Result, []module.Scope) {
		e := newEnv("bob", map[string][]string{ref: {"helm/app/values-prod.yaml"}})
		changes := &fakeChanges{paths: map[string][]string{ref: {"helm/app/values-prod.yaml"}},
			changes: []module.PathChange{{Commit: strings.Repeat("c", 40), Path: "helm/app/values-prod.yaml", State: alice}},
			known:   map[string][]module.PathState{"helm/app/values-prod.yaml": {alice}}}
		e.in.Changes = changes
		e.in.Refs = &fakeRefs{class: module.ClassProtected, status: ok}
		res := e.evaluate(t, policy)
		return res, changes.scopes
	}
	res, scopes := run(basePolicy, "refs/heads/main")
	assert.Equal(t, module.Allow, res.Decision, "new-content exempts known content in the default branch too")
	assert.Equal(t, []module.Scope{{Class: module.ClassOther}}, scopes)

	incoming := strings.Replace(basePolicy, "    paths: [\"helm/**/values-prod.yaml\"]", "    check_history: incoming\n    paths: [\"helm/**/values-prod.yaml\"]", 1)
	res, scopes = run(incoming, "refs/heads/main")
	assert.Equal(t, module.Deny, res.Decision, "incoming counts every change")
	assert.Equal(t, []module.Scope{{Class: module.ClassDefault}}, scopes)
	res, scopes = run(incoming, "refs/heads/release/1")
	assert.Equal(t, module.Deny, res.Decision)
	assert.Equal(t, []module.Scope{{Class: module.ClassProtected, Key: module.HistoryIncoming}}, scopes)

	all := strings.Replace(incoming, "    match: [main, '/^release\\//']", "    match: ['/./']", 1)
	res, _ = run(all, "refs/heads/release/1")
	assert.Equal(t, module.Deny, res.Decision, "a list that selects every branch checks like match: all")
}

// TestEvaluate_KnownStatesFail: over the commit limit the known states do
// not exempt anything, and a failure of git is an error.
func TestEvaluate_KnownStatesFail(t *testing.T) {
	alice := module.PathState{Mode: "100644", OID: strings.Repeat("1", 40)}
	changes := []module.PathChange{{Commit: strings.Repeat("c", 40), Path: ".gitlab-ci.yml", State: alice}}
	e := newEnv("bob", map[string][]string{"refs/heads/feature": {".gitlab-ci.yml"}})
	e.in.Changes = &fakeChanges{paths: map[string][]string{"refs/heads/feature": {".gitlab-ci.yml"}}, changes: changes, knownErr: module.ErrLimitExceeded}
	assert.Equal(t, module.Deny, e.evaluate(t, basePolicy).Decision)

	e.in.Changes = &fakeChanges{paths: map[string][]string{"refs/heads/feature": {".gitlab-ci.yml"}}, changes: changes, knownErr: errors.New("git failed")}
	p, err := New(nil).Parse([]byte(basePolicy))
	require.NoError(t, err)
	_, err = p.Evaluate(context.Background(), e.in)
	assert.ErrorContains(t, err, "git failed")
}

func TestEvaluate_KnownContentStateError(t *testing.T) {
	e := newEnv("bob", map[string][]string{"refs/heads/feature": {".gitlab-ci.yml"}})
	e.in.Changes = &fakeChanges{paths: map[string][]string{"refs/heads/feature": {".gitlab-ci.yml"}}, stateErr: errors.New("git failed")}
	p, err := New(nil).Parse([]byte(basePolicy))
	require.NoError(t, err)
	_, err = p.Evaluate(context.Background(), e.in)
	assert.ErrorContains(t, err, "git failed")
}

func policyChange(user string, access int) *env {
	e := newEnv(user, map[string][]string{"refs/heads/main": {"PATHPOLICY"}})
	e.authz.pusher = access
	return e
}

func TestAdmins_DeveloperIsNotAdmin(t *testing.T) {
	e := policyChange("platform-lead", 40)
	assert.Equal(t, module.Allow, e.evaluate(t, basePolicy).Decision, "an admin with Maintainer access")
	e = policyChange("Platform-Lead", 30)
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, []string{module.CodePolicyAdmin}, codes(res), "an admin with Developer access is not an admin")
}

func TestRecovery_OwnerDeniedWhenAdminsAlive(t *testing.T) {
	e := policyChange("owner", 50)
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, []string{module.CodePolicyAdmin}, codes(res))
	assert.Contains(t, res.Violations[0].Facts, "admins: platform-lead, sre-lead")
	assert.Contains(t, res.Violations[0].Reason, "2 effective admins")
}

func TestRecovery_LastAdminRemoved(t *testing.T) {
	e := policyChange("owner", 50)
	e.authz.users = map[string]module.User{}
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, module.Allow, res.Decision)
	require.Len(t, res.Notices, 1)
	assert.Contains(t, res.Notices[0], "PATHPOLICY is changed by Owner owner because it has no working admins")
	require.Len(t, res.Events, 1)
	assert.Equal(t, "policy_recovery", res.Events[0].Event)
}

func TestRecovery_LastAdminBlocked(t *testing.T) {
	e := policyChange("owner", 50)
	e.authz.users = map[string]module.User{"platform-lead": {ID: 1, Active: false}}
	e.authz.levels = map[int64]int{1: 40}
	assert.Equal(t, module.Allow, e.evaluate(t, basePolicy).Decision)
}

func TestRecovery_AdminsWithoutAccess(t *testing.T) {
	e := policyChange("owner", 50)
	e.authz.levels = map[int64]int{1: 30, 2: 0}
	assert.Equal(t, module.Allow, e.evaluate(t, basePolicy).Decision, "admins below Maintainer are not effective")
}

func TestRecovery_OwnerViaParentGroup(t *testing.T) {
	// The effective access level includes inherited group membership.
	e := policyChange("group-owner", 50)
	e.authz.users = map[string]module.User{}
	assert.Equal(t, module.Allow, e.evaluate(t, basePolicy).Decision)
}

func TestRecovery_NonOwnerDenied(t *testing.T) {
	e := policyChange("maintainer", 40)
	e.authz.users = map[string]module.User{}
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, []string{module.CodePolicyAdmin}, codes(res))
	assert.Equal(t, module.CodePolicyAdmin, res.Violations[0].Code)
}

func TestRecovery_PushAlsoTouchesProtected(t *testing.T) {
	e := newEnv("owner", map[string][]string{"refs/heads/main": {"PATHPOLICY", ".gitlab-ci.yml"}})
	e.authz.pusher = 50
	e.authz.users = map[string]module.User{}
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, module.Deny, res.Decision, "protected paths are still checked by the old rules")
	assert.Equal(t, []string{module.CodeProtectedPath}, codes(res))
}

func TestRecovery_APIFailures(t *testing.T) {
	failures := map[string]module.Status{
		"500":        module.Unknown("unavailable (HTTP 500)"),
		"timeout":    module.Unknown("unavailable: timeout"),
		"401":        module.Unknown("unauthorized (HTTP 401)"),
		"429":        module.Unknown("unavailable (HTTP 429)"),
		"conn_reset": module.Unknown("unavailable: connection reset"),
		"budget":     module.UnknownLimit("budget_exceeded"),
	}
	for name, st := range failures {
		t.Run(name, func(t *testing.T) {
			e := policyChange("owner", 50)
			e.authz.usersSt = st
			res := e.evaluate(t, basePolicy)
			want := module.CodeAuthorizationBackend
			if st.IsLimit() {
				want = module.CodeLimitExceeded
			}
			assert.Equal(t, []string{want}, codes(res), "UNKNOWN never enables recovery")
			assert.Empty(t, res.Notices)
		})
	}
	e := policyChange("platform-lead", 0)
	e.authz.pusherSt = module.Unknown("timeout")
	assert.Equal(t, []string{module.CodeAuthorizationBackend}, codes(e.evaluate(t, basePolicy)))
}

func TestRecovery_InvalidAnchorOwner(t *testing.T) {
	m := New(nil)
	e := newEnv("owner", map[string][]string{"refs/heads/main": {"PATHPOLICY"}})
	e.authz.pusher = 50
	res, err := m.EvaluateInvalid(context.Background(), e.in, errors.New("line 2: bad"))
	require.NoError(t, err)
	assert.Equal(t, module.Allow, res.Decision, "an Owner may fix the invalid policy")
	assert.Len(t, res.Notices, 1)

	e = newEnv("owner", map[string][]string{"refs/heads/main": {"PATHPOLICY", "src/x"}})
	e.authz.pusher = 50
	res, err = m.EvaluateInvalid(context.Background(), e.in, errors.New("line 2: bad"))
	require.NoError(t, err)
	assert.Equal(t, []string{module.CodeInvalidPolicy}, codes(res), "the recovery push may change only PATHPOLICY")

	e = newEnv("dev", map[string][]string{"refs/heads/main": {"PATHPOLICY"}})
	e.authz.pusher = 40
	res, err = m.EvaluateInvalid(context.Background(), e.in, errors.New("line 2: bad"))
	require.NoError(t, err)
	assert.Equal(t, []string{module.CodeInvalidPolicy}, codes(res))
	assert.Equal(t, "invalid_policy", res.Events[0].Event)

	e = newEnv("dev", map[string][]string{})
	e.in.Updates = []module.RefUpdate{{Old: strings.Repeat("a", 40), New: strings.Repeat("0", 40), Ref: "refs/heads/old"}}
	res, err = m.EvaluateInvalid(context.Background(), e.in, errors.New("bad"))
	require.NoError(t, err)
	assert.Equal(t, module.Allow, res.Decision, "deleting refs is not blocked")
}

func TestBootstrap(t *testing.T) {
	m := New(nil)
	for _, tc := range []struct {
		access int
		want   module.Decision
	}{{30, module.Deny}, {40, module.Allow}, {50, module.Allow}} {
		e := newEnv("someone", map[string][]string{"refs/heads/main": {"PATHPOLICY"}})
		e.authz.pusher = tc.access
		e.in.PolicyChanges = []module.PolicyChange{{Ref: "refs/heads/main", File: File}}
		res, err := m.EvaluateAbsent(context.Background(), e.in)
		require.NoError(t, err)
		assert.Equal(t, tc.want, res.Decision, "access %d", tc.access)
		if tc.want == module.Allow {
			require.Len(t, res.Events, 1)
			assert.Equal(t, "policy_bootstrap", res.Events[0].Event)
		}
	}
	e := newEnv("someone", map[string][]string{"refs/heads/feature": {"PATHPOLICY"}})
	e.in.PolicyChanges = []module.PolicyChange{{Ref: "refs/heads/feature", File: File}}
	res, err := m.EvaluateAbsent(context.Background(), e.in)
	require.NoError(t, err)
	assert.Equal(t, module.Allow, res.Decision, "adding PATHPOLICY outside the default branch is only accepted as a new version")
	assert.False(t, res.Touched)
}

func TestDeletionWarning(t *testing.T) {
	e := policyChange("platform-lead", 40)
	e.in.PolicyChanges = []module.PolicyChange{{Ref: "refs/heads/main", File: File, Deleted: true}}
	e.in.AnchorPolicies = []string{"PATHPOLICY", "COMMITPOLICY", "TAGPOLICY"}
	res := e.evaluate(t, basePolicy)
	assert.Equal(t, module.Allow, res.Decision)
	require.Len(t, res.Notices, 1)
	assert.Contains(t, res.Notices[0], "anyone who can push may change COMMITPOLICY and TAGPOLICY")
}

func TestAcceptance_LockoutPrevented(t *testing.T) {
	p, err := New(nil).Parse([]byte(basePolicy))
	require.NoError(t, err)
	a := team()
	a.known = map[string]bool{"platform-lead": true, "sre-lead": true, "ci-maintainer": true, "dev-lead": true, "release-manager": true}
	assert.Empty(t, p.Validate(context.Background(), a))
	a.levels = map[int64]int{1: 30, 2: 30}
	got := p.Validate(context.Background(), a)
	require.Len(t, got, 2, "one line per admin, without the role GitLab returned")
	assert.Equal(t, module.CodeLockoutPrevented, got[0].Code)
	assert.Equal(t, `admin "platform-lead" is not an active user with Maintainer or higher in this project`, got[0].Msg)
	a.levels = map[int64]int{1: 40, 2: 30}
	got = p.Validate(context.Background(), a)
	require.Len(t, got, 2)
	assert.True(t, got[0].Warning, "a file with a working admin is valid")
	assert.Equal(t, "1 of 2 admins work", got[1].Msg)
	a.usersSt = module.Unknown("timeout")
	got = p.Validate(context.Background(), a)
	require.Len(t, got, 1)
	assert.True(t, got[0].Unknown, "UNKNOWN is authorization_backend_failure, not lockout")
	assert.Empty(t, p.Validate(context.Background(), nil), "validate without GitLab skips online checks")
}

func TestAcceptance_AddedPrincipalsSingleGraphQL(t *testing.T) {
	p, err := New(nil).Parse([]byte(basePolicy))
	require.NoError(t, err)
	a := team()
	a.known = map[string]bool{"platform-lead": true, "sre-lead": true}
	got := p.Validate(context.Background(), a)
	assert.Equal(t, 2, a.usersCalls, "one lookup of all added usernames, one for the admin count")
	msgs := make([]string, 0, len(got))
	for _, pr := range got {
		msgs = append(msgs, pr.Msg)
	}
	assert.ElementsMatch(t, []string{`"ci-maintainer" is not an active GitLab user`, `"dev-lead" is not an active GitLab user`,
		`"release-manager" is not an active GitLab user`}, msgs)
}
