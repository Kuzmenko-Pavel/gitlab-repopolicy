package commit

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// fakeRepo serves commit objects and the commit lists of the change set.
type fakeRepo struct {
	objects   map[string][]byte
	entering  map[string][]string // by ref
	firstPar  map[string][]string // by ref, for FirstParent scopes
	candidate map[string][]string
	limit     bool
	scopes    []module.Scope
}

func (f *fakeRepo) add(oid, message string, parents int) string {
	var b strings.Builder
	b.WriteString("tree t\n")
	for i := range parents {
		fmt.Fprintf(&b, "parent p%d\n", i)
	}
	b.WriteString("author a <a@b> 1 +0000\ncommitter a <a@b> 1 +0000\n\n" + message)
	f.objects[oid] = []byte(b.String())
	return oid
}

func (f *fakeRepo) Read(_ context.Context, oid string, _ int) (module.Object, error) {
	return module.Object{OID: oid, Type: "commit", Data: f.objects[oid]}, nil
}

func (f *fakeRepo) CandidatePaths(context.Context, module.RefUpdate) ([]string, error) {
	return nil, nil
}
func (f *fakeRepo) Paths(context.Context, module.RefUpdate, module.Scope) ([]string, error) {
	return nil, nil
}
func (f *fakeRepo) NewCommits(context.Context, module.RefUpdate, int) ([]string, error) {
	return nil, nil
}

func (f *fakeRepo) CandidateCommits(_ context.Context, u module.RefUpdate, _ int) ([]string, error) {
	if f.limit {
		return nil, fmt.Errorf("x: %w", module.ErrLimitExceeded)
	}
	if c, ok := f.candidate[u.Ref]; ok {
		return c, nil
	}
	return f.entering[u.Ref], nil
}

func (f *fakeRepo) EnteringCommits(_ context.Context, u module.RefUpdate, s module.Scope, _ int) ([]string, error) {
	f.scopes = append(f.scopes, s)
	if f.limit {
		return nil, fmt.Errorf("x: %w", module.ErrLimitExceeded)
	}
	if s.FirstParent {
		return f.firstPar[u.Ref], nil
	}
	return f.entering[u.Ref], nil
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
	return module.Scope{Class: module.ClassProtected, Key: "gitlab"}, r.status
}

type fakePrincipal struct {
	name   string
	status module.Status
	asked  int
}

func (p *fakePrincipal) Username(context.Context) (string, module.Status) {
	p.asked++
	return p.name, p.status
}

func newRepo() *fakeRepo {
	return &fakeRepo{objects: map[string][]byte{}, entering: map[string][]string{}, firstPar: map[string][]string{}, candidate: map[string][]string{}}
}

func input(repo *fakeRepo, refs *fakeRefs, who *fakePrincipal, refNames ...string) *module.Input {
	in := &module.Input{Refs: refs, Changes: repo, Objects: repo, Principals: who, Clock: time.Now}
	for _, r := range refNames {
		in.Updates = append(in.Updates, module.RefUpdate{Old: strings.Repeat("a", 40), New: strings.Repeat("b", 40), Ref: r})
	}
	return in
}

func evaluate(t *testing.T, policyText string, in *module.Input) *module.Result {
	t.Helper()
	p, err := New(testSettings()).Parse([]byte(policyText))
	require.NoError(t, err)
	res, err := p.Evaluate(context.Background(), in)
	require.NoError(t, err)
	return res
}

func confirmed() module.Status { return module.Confirmed() }

func TestRulesets_And(t *testing.T) {
	repo := newRepo()
	repo.entering["refs/heads/main"] = []string{repo.add("c1", "feat: Add thing.\n", 1)}
	policyText := `version: 1
rulesets:
  - name: case
    rules: {subject-case: {level: error, value: lower-first}}
  - name: stop
    refs: default
    rules: {subject-full-stop: {level: error, value: forbid}}
  - name: release
    refs: [refs/heads/release/*]
    rules: {type-enum: {level: error, value: [fix]}}
`
	res := evaluate(t, policyText, input(repo, &fakeRefs{}, &fakePrincipal{}, "refs/heads/main"))
	assert.Equal(t, module.Deny, res.Decision)
	require.Len(t, res.Violations, 2, "every matching ruleset applies")
	assert.Equal(t, "case", res.Violations[0].Rule)
	assert.Equal(t, "stop", res.Violations[1].Rule)
	assert.Equal(t, module.CodeCommitFormat, res.Violations[0].Code)
	assert.Equal(t, `c1 "feat: Add thing."`, res.Violations[0].Items[0].Name)
	assert.True(t, res.Violations[0].Items[0].Commit)
	assert.True(t, res.Touched)

	repo.entering["refs/heads/release/1"] = repo.entering["refs/heads/main"]
	res = evaluate(t, policyText, input(repo, &fakeRefs{}, &fakePrincipal{}, "refs/heads/release/1"))
	require.Len(t, res.Violations, 2, "default does not match release/1, the listed ruleset does")
	assert.Equal(t, "release", res.Violations[1].Rule)
	assert.Equal(t, "commitpolicy/release", repo.scopes[len(repo.scopes)-1].Key)
}

const exceptPolicy = `version: 1
rulesets:
  - name: r
    except: {users: [release-bot]}
    rules: {type-enum: {level: error, value: [feat]}}
`

func TestExcept_UsersHTTPS(t *testing.T) {
	repo := newRepo()
	repo.entering["refs/heads/main"] = []string{repo.add("c1", "chore: release\n", 1)}
	bot := &fakePrincipal{name: "Release-Bot", status: confirmed()}
	assert.Equal(t, module.Allow, evaluate(t, exceptPolicy, input(repo, &fakeRefs{}, bot, "refs/heads/main")).Decision)
	other := &fakePrincipal{name: "alice", status: confirmed()}
	assert.Equal(t, module.Deny, evaluate(t, exceptPolicy, input(repo, &fakeRefs{}, other, "refs/heads/main")).Decision)

	repo.entering["refs/heads/main"] = []string{repo.add("c2", "feat: ok\n", 1)}
	lazy := &fakePrincipal{name: "alice", status: confirmed()}
	evaluate(t, exceptPolicy, input(repo, &fakeRefs{}, lazy, "refs/heads/main"))
	assert.Zero(t, lazy.asked, "the principal is resolved only when a rule is broken")
}

func TestExcept_UsersSSH(t *testing.T) {
	repo := newRepo()
	repo.entering["refs/heads/main"] = []string{repo.add("c1", "chore: release\n", 1)}
	deployKey := &fakePrincipal{name: "", status: confirmed()}
	assert.Equal(t, module.Deny, evaluate(t, exceptPolicy, input(repo, &fakeRefs{}, deployKey, "refs/heads/main")).Decision,
		"a deploy key is not a person: except.users does not apply")
	failed := &fakePrincipal{status: module.Unknown("timeout")}
	res := evaluate(t, exceptPolicy, input(repo, &fakeRefs{}, failed, "refs/heads/main"))
	assert.Equal(t, module.Warn, res.Decision, "a failed resolution skips the ruleset with a warning (FailOpen)")
	assert.Equal(t, module.CodeAuthorizationBackend, res.Violations[0].Code)
}

func TestMergedCommits(t *testing.T) {
	repo := newRepo()
	merge := repo.add("m", "Merge branch 'f' into 'main'\n", 2)
	bad := repo.add("b", "wip\n", 1)
	repo.entering["refs/heads/main"] = []string{merge, bad}
	repo.firstPar["refs/heads/main"] = []string{merge}
	policyText := "version: 1\nrulesets:\n  - name: r\n    except: {merge_commits: true, merged_commits: true}\n    rules: {}\n"
	assert.Equal(t, module.Allow, evaluate(t, policyText, input(repo, &fakeRefs{}, &fakePrincipal{}, "refs/heads/main")).Decision)
	assert.True(t, repo.scopes[0].FirstParent)
	policyText = "version: 1\nrulesets:\n  - name: r\n    except: {merge_commits: true}\n    rules: {}\n"
	assert.Equal(t, module.Deny, evaluate(t, policyText, input(repo, &fakeRefs{}, &fakePrincipal{}, "refs/heads/main")).Decision,
		"without merged_commits the branch commit is checked")
}

func TestProtectedRuleset(t *testing.T) {
	policyText := "version: 1\nrulesets:\n  - name: p\n    refs: protected\n    rules: {type-enum: {level: error, value: [feat]}}\n"
	repo := newRepo()
	good := repo.add("g", "feat: ok\n", 1)
	repo.entering["refs/heads/x"] = []string{good}
	refs := &fakeRefs{class: module.ClassProtected, status: confirmed()}
	evaluate(t, policyText, input(repo, refs, &fakePrincipal{}, "refs/heads/x"))
	assert.Zero(t, refs.asked, "no violation in the candidates: no API")

	repo.entering["refs/heads/x"] = []string{repo.add("b", "wip\n", 1)}
	refs = &fakeRefs{class: module.ClassOther, status: confirmed()}
	assert.Equal(t, module.Allow, evaluate(t, policyText, input(repo, refs, &fakePrincipal{}, "refs/heads/x")).Decision,
		"a feature branch is free under refs: protected")
	assert.Equal(t, 1, refs.asked)

	refs = &fakeRefs{class: module.ClassProtected, status: confirmed()}
	assert.Equal(t, module.Deny, evaluate(t, policyText, input(repo, refs, &fakePrincipal{}, "refs/heads/x")).Decision)

	refs = &fakeRefs{status: module.Unknown("503")}
	res := evaluate(t, policyText, input(repo, refs, &fakePrincipal{}, "refs/heads/x"))
	assert.Equal(t, module.Warn, res.Decision, "API failure skips refs: protected with a warning")
	assert.Equal(t, "p", res.Violations[0].Rule)
}

func TestTooManyCommits(t *testing.T) {
	repo := newRepo()
	repo.limit = true
	res := evaluate(t, "version: 1\nrulesets: [{name: r}]\n", input(repo, &fakeRefs{}, &fakePrincipal{}, "refs/heads/main"))
	assert.Equal(t, module.Deny, res.Decision)
	assert.Equal(t, module.CodeLimitExceeded, res.Violations[0].Code)
	res = evaluate(t, "version: 1\nmode: warn\nrulesets: [{name: r}]\n", input(repo, &fakeRefs{}, &fakePrincipal{}, "refs/heads/main"))
	assert.Equal(t, module.Warn, res.Decision)
}

func TestUnknownRule_Anchor(t *testing.T) {
	repo := newRepo()
	repo.entering["refs/heads/main"] = []string{repo.add("c", "feat: ok\n", 1)}
	res := evaluate(t, "version: 1\nrulesets: [{name: r, rules: {future-rule: {level: error}}}]\n", input(repo, &fakeRefs{}, &fakePrincipal{}, "refs/heads/main"))
	assert.Equal(t, module.Warn, res.Decision)
	assert.Contains(t, res.Violations[0].Message, "future-rule")
}

func TestUnknownRule_NewVersion(t *testing.T) {
	p, err := New(testSettings()).Parse([]byte("version: 1\nrulesets: [{name: r, rules: {typo-enum: {level: error}}}]\n"))
	require.NoError(t, err)
	got := p.Validate(context.Background(), nil)
	require.Len(t, got, 1)
	assert.False(t, got[0].Warning)
}

func TestNeeds_NoIdentity(t *testing.T) {
	p, err := New(testSettings()).Parse([]byte("version: 1\nrulesets: [{name: r}]\n"))
	require.NoError(t, err)
	assert.False(t, p.Needs().Identity, "COMMITPOLICY gets no authorization backend")
}

type activeUsers map[string]bool

func (a activeUsers) ActiveUsers(_ context.Context, users []string) (map[string]bool, module.Status) {
	return a, module.Confirmed()
}

func TestValidate_ExceptUsers(t *testing.T) {
	p, err := New(testSettings()).Parse([]byte(exceptPolicy))
	require.NoError(t, err)
	assert.Empty(t, p.Validate(context.Background(), activeUsers{"release-bot": true}))
	got := p.Validate(context.Background(), activeUsers{})
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Msg, `"release-bot" is not an active GitLab user`)
	assert.Equal(t, []string{"release-bot"}, p.(module.UsernameLister).Usernames())
}

func TestEvaluateInvalid(t *testing.T) {
	res, err := New(testSettings()).EvaluateInvalid(context.Background(), &module.Input{}, fmt.Errorf("line 3: bad"))
	require.NoError(t, err)
	assert.Equal(t, module.Warn, res.Decision)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "invalid_commit_policy", res.Events[0].Event)
}
