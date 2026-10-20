package changes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// fixture is a work repository and a bare "server" repository. Objects of a
// push are copied to the server without moving its refs, as in a quarantine.
type fixture struct {
	t          *testing.T
	work, bare string
	env        []string
	path       string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := os.Getenv("PATH")
	if bin := os.Getenv("GIT_BIN"); bin != "" && bin != "git" {
		path = filepath.Dir(bin) + string(filepath.ListSeparator) + path
	}
	f := &fixture{t: t, work: filepath.Join(t.TempDir(), "w"), bare: filepath.Join(t.TempDir(), "s.git"), path: path}
	f.env = []string{"PATH=" + path, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "HOME=" + t.TempDir(),
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com"}
	f.git("", "init", "-q", "-b", "main", f.work)
	f.git("", "init", "-q", "--bare", "-b", "main", f.bare)
	return f
}

func (f *fixture) git(dir string, args ...string) string {
	f.t.Helper()
	cmd := exec.CommandContext(f.t.Context(), fixtureGit(), args...)
	cmd.Env, cmd.Dir = f.env, dir
	out, err := cmd.CombinedOutput()
	require.NoError(f.t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func (f *fixture) write(files map[string]string) {
	f.t.Helper()
	for name, content := range files {
		p := filepath.Join(f.work, name)
		if content == "" {
			require.NoError(f.t, os.Remove(p))
			continue
		}
		require.NoError(f.t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(f.t, os.WriteFile(p, []byte(content), 0o644))
	}
}

func (f *fixture) commit(msg string, files map[string]string) string {
	f.t.Helper()
	f.write(files)
	f.git(f.work, "add", "-A")
	f.git(f.work, "commit", "-q", "--allow-empty", "-m", msg)
	return f.git(f.work, "rev-parse", "HEAD")
}

// publish sets server refs: the state before the push.
func (f *fixture) publish(refspecs ...string) {
	f.t.Helper()
	f.git(f.work, append([]string{"push", "-q", "-f", f.bare}, refspecs...)...)
}

// stage copies the objects of rev to the server without changing its refs.
func (f *fixture) stage(rev string) {
	f.t.Helper()
	f.git(f.work, "push", "-q", "-f", f.bare, rev+":refs/staging/x")
	f.git(f.bare, "update-ref", "-d", "refs/staging/x")
}

func (f *fixture) set(defaultRef string) (*Set, *counting) {
	f.t.Helper()
	repo, err := gitx.Open(gitx.Env{Dir: f.bare, Path: f.path})
	require.NoError(f.t, err)
	f.t.Cleanup(func() { assert.NoError(f.t, repo.Close()) })
	c := &counting{Git: repo}
	return New(c, defaultRef), c
}

type counting struct {
	Git
	revLists int
}

func (c *counting) RevList(ctx context.Context, o gitx.RevListOptions) ([]string, error) {
	c.revLists++
	return c.Git.RevList(ctx, o)
}

var zero = strings.Repeat("0", 40)

func update(old, new, ref string) module.RefUpdate {
	return module.RefUpdate{Old: old, New: new, Ref: ref}
}

var (
	ctx      = context.Background()
	other    = module.Scope{Class: module.ClassOther}
	def      = module.Scope{Class: module.ClassDefault}
	listed   = module.Scope{Class: module.ClassProtected, Key: "release", Protected: func(r string) bool { return strings.HasPrefix(r, "refs/heads/release/") }}
	noFilter = module.Scope{Class: module.ClassProtected, Key: "none"}
)

func TestPaths_RenameInto(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base", map[string]string{"src/conf.yml": "x"})
	f.publish(base+":refs/heads/main", base+":refs/heads/feature")
	head := f.commit("move", map[string]string{"src/conf.yml": "", "deploy/conf.yml": "x"})
	f.stage(head)
	s, _ := f.set("refs/heads/main")
	u := update(base, head, "refs/heads/feature")
	for _, get := range []func() ([]string, error){
		func() ([]string, error) { return s.CandidatePaths(ctx, u) },
		func() ([]string, error) { return s.Paths(ctx, u, other) },
		func() ([]string, error) { return s.Paths(ctx, u, listed) },
	} {
		paths, err := get()
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"src/conf.yml", "deploy/conf.yml"}, paths, "rename is delete + add")
	}
}

func TestPaths_RenameOut(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base", map[string]string{"deploy/conf.yml": "x"})
	f.publish(base + ":refs/heads/main")
	head := f.commit("move out", map[string]string{"deploy/conf.yml": "", "tmp.yml": "x"})
	f.stage(head)
	s, _ := f.set("refs/heads/main")
	paths, err := s.Paths(ctx, update(base, head, "refs/heads/main"), def)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"deploy/conf.yml", "tmp.yml"}, paths)
}

func TestPaths_ForcePushRollback(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1", map[string]string{"ci.yml": "v1"})
	c2 := f.commit("2", map[string]string{"ci.yml": "v2"})
	f.publish(c2 + ":refs/heads/main")
	s, c := f.set("refs/heads/main")
	u := update(c2, c1, "refs/heads/main")
	paths, err := s.Paths(ctx, u, def)
	require.NoError(t, err)
	assert.Equal(t, []string{"ci.yml"}, paths, "rollback without new commits is a change of the default branch")
	commits, err := s.NewCommits(ctx, u, 10)
	require.NoError(t, err)
	assert.Empty(t, commits)
	entering, err := s.EnteringCommits(ctx, u, def, 10)
	require.NoError(t, err)
	assert.Empty(t, entering)
	assert.Equal(t, 3, c.revLists, "the entering commits of Paths, then the views of the test with their own limit")
}

// TestPaths_ChangedAndRevertedInOnePush: a commit that changes a file and a
// commit that reverts it leave no net diff, but the default branch and a
// protected branch still count the change, and so do the candidates of a
// branch that takes both commits from another branch.
func TestPaths_ChangedAndRevertedInOnePush(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base", map[string]string{"ci.yml": "v1", "app.go": "a"})
	f.commit("evil", map[string]string{"ci.yml": "evil"})
	back := f.commit("revert", map[string]string{"ci.yml": "v1"})
	f.publish(base+":refs/heads/main", base+":refs/heads/release/1")
	f.stage(back)
	s, _ := f.set("refs/heads/main")
	for _, tc := range []struct {
		ref   string
		scope module.Scope
	}{{"refs/heads/main", def}, {"refs/heads/release/1", module.Scope{Class: module.ClassProtected, Key: "gitlab"}}} {
		u := update(base, back, tc.ref)
		paths, err := s.Paths(ctx, u, tc.scope)
		require.NoError(t, err)
		assert.Equal(t, []string{"ci.yml"}, paths, tc.ref)
		candidates, err := s.CandidatePaths(ctx, u)
		require.NoError(t, err)
		assert.Equal(t, []string{"ci.yml"}, candidates, "the candidates are a superset of Paths: %s", tc.ref)
	}

	// Both commits are in another branch first: no new commits, still a candidate.
	f.publish(back + ":refs/heads/feature")
	s, _ = f.set("refs/heads/main")
	u := update(base, back, "refs/heads/release/1")
	commits, err := s.NewCommits(ctx, u, 10)
	require.NoError(t, err)
	assert.Empty(t, commits)
	candidates, err := s.CandidatePaths(ctx, u)
	require.NoError(t, err)
	assert.Equal(t, []string{"ci.yml"}, candidates)
	paths, err := s.Paths(ctx, u, module.Scope{Class: module.ClassProtected, Key: "gitlab"})
	require.NoError(t, err)
	assert.Equal(t, []string{"ci.yml"}, paths)
	paths, err = s.Paths(ctx, update(base, back, "refs/heads/feature2"), other)
	require.NoError(t, err)
	assert.Empty(t, paths, "an ordinary branch checks only new commits")
}

func TestPaths_RebaseOnMainOnlyOwnChanges(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base", map[string]string{"ci.yml": "v1", "app.go": "a"})
	f.git(f.work, "checkout", "-q", "-b", "feature")
	mine := f.commit("mine", map[string]string{"app.go": "b"})
	f.git(f.work, "checkout", "-q", "main")
	theirs := f.commit("devops", map[string]string{"ci.yml": "v2"})
	f.publish(theirs+":refs/heads/main", mine+":refs/heads/feature")
	f.git(f.work, "checkout", "-q", "feature")
	f.git(f.work, "rebase", "-q", "main")
	rebased := f.git(f.work, "rev-parse", "HEAD")
	f.stage(rebased)
	s, _ := f.set("refs/heads/main")
	u := update(mine, rebased, "refs/heads/feature")
	paths, err := s.Paths(ctx, u, other)
	require.NoError(t, err)
	assert.Equal(t, []string{"app.go"}, paths, "the foreign change is not attributed to the rebase")
	cand, err := s.CandidatePaths(ctx, u)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"app.go", "ci.yml"}, cand, "candidates also hold the net diff")
	_ = base
}

func TestPaths_BranchFromOldCommit(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1", map[string]string{"ci.yml": "v1"})
	c2 := f.commit("2", map[string]string{"ci.yml": "v2"})
	f.publish(c2 + ":refs/heads/main")
	s, _ := f.set("refs/heads/main")
	u := update(zero, c1, "refs/heads/old")
	for _, get := range []func() ([]string, error){
		func() ([]string, error) { return s.CandidatePaths(ctx, u) },
		func() ([]string, error) { return s.Paths(ctx, u, other) },
		func() ([]string, error) { return s.Paths(ctx, u, listed) },
	} {
		paths, err := get()
		require.NoError(t, err)
		assert.Empty(t, paths)
	}
}

func TestPaths_CreateProtectedFromFeature(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base", map[string]string{"a": "1"})
	f.git(f.work, "checkout", "-q", "-b", "release/1")
	rel := f.commit("rel", map[string]string{"r": "1"})
	f.git(f.work, "checkout", "-q", "-b", "feature")
	feat := f.commit("feat", map[string]string{"ci.yml": "x"})
	f.publish(base+":refs/heads/main", rel+":refs/heads/release/1", feat+":refs/heads/feature")
	s, _ := f.set("refs/heads/main")
	u := update(zero, feat, "refs/heads/release/2")
	paths, err := s.Paths(ctx, u, listed)
	require.NoError(t, err)
	assert.Equal(t, []string{"ci.yml"}, paths, "commits already in the default branch and protected refs are not new")
	commits, err := s.EnteringCommits(ctx, u, listed, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{feat}, commits)
	all, err := s.EnteringCommits(ctx, u, noFilter, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{feat, rel}, all, "without the protected set only the default branch is excluded")
	newOnes, err := s.NewCommits(ctx, u, 10)
	require.NoError(t, err)
	assert.Empty(t, newOnes, "the feature branch already holds the commits")
	cand, err := s.CandidatePaths(ctx, u)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ci.yml", "r"}, cand)
	cc, err := s.CandidateCommits(ctx, u, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{feat, rel}, cc)
}

func TestPaths_EvilMerge(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base", map[string]string{"a": "1"})
	f.git(f.work, "checkout", "-q", "-b", "side")
	side := f.commit("side", map[string]string{"b": "1"})
	f.git(f.work, "checkout", "-q", "main")
	f.publish(base+":refs/heads/main", side+":refs/heads/side")
	f.git(f.work, "merge", "-q", "--no-ff", "--no-commit", "side")
	f.write(map[string]string{"ci.yml": "evil"})
	f.git(f.work, "add", "-A")
	f.git(f.work, "commit", "-q", "-m", "merge")
	merge := f.git(f.work, "rev-parse", "HEAD")
	f.stage(merge)
	s, _ := f.set("refs/heads/main")
	paths, err := s.Paths(ctx, update(zero, merge, "refs/heads/topic"), other)
	require.NoError(t, err)
	assert.Equal(t, []string{"ci.yml"}, paths, "combined diff shows the change that is in no parent")
}

func TestPaths_DeleteAndDefaultCreate(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1", map[string]string{"a": "1"})
	c2 := f.commit("2", map[string]string{"b": "1"})
	f.stage(c2)
	s, _ := f.set("refs/heads/main")
	paths, err := s.Paths(ctx, update(c1, zero, "refs/heads/x"), other)
	require.NoError(t, err)
	assert.Nil(t, paths, "delete changes nothing")
	commits, err := s.NewCommits(ctx, update(c1, zero, "refs/heads/x"), 10)
	require.NoError(t, err)
	assert.Nil(t, commits)
	paths, err = s.Paths(ctx, update(zero, c2, "refs/heads/main"), def)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b"}, paths, "first push: all commits")
	commits, err = s.EnteringCommits(ctx, update(zero, c2, "refs/heads/main"), def, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{c2, c1}, commits)
}

func TestTags(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1", map[string]string{"a": "1"})
	f.publish(c1 + ":refs/heads/main")
	c2 := f.commit("2", map[string]string{"b": "1"})
	f.git(f.work, "tag", "-a", "-m", "v", "v1")
	tree := f.git(f.work, "rev-parse", "HEAD^{tree}")
	f.git(f.work, "tag", "-a", "-m", "tree", "vt", tree)
	f.stage("refs/tags/v1")
	f.stage("refs/tags/vt")
	tag := f.git(f.work, "rev-parse", "refs/tags/v1")
	treeTag := f.git(f.work, "rev-parse", "refs/tags/vt")
	s, _ := f.set("refs/heads/main")
	paths, err := s.Paths(ctx, update(zero, tag, "refs/tags/v1"), other)
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, paths, "an annotated tag is peeled to its commit")
	commits, err := s.NewCommits(ctx, update(zero, tag, "refs/tags/v1"), 10)
	require.NoError(t, err)
	assert.Equal(t, []string{c2}, commits)
	_, err = s.Paths(ctx, update(zero, treeTag, "refs/tags/vt"), other)
	assert.ErrorIs(t, err, module.ErrNotCommit)
}

func TestLimits(t *testing.T) {
	f := newFixture(t)
	f.commit("1", nil)
	f.commit("2", nil)
	c3 := f.commit("3", nil)
	f.stage(c3)
	s, _ := f.set("refs/heads/main")
	u := update(zero, c3, "refs/heads/x")
	_, err := s.NewCommits(ctx, u, 2)
	assert.ErrorIs(t, err, module.ErrLimitExceeded)
	commits, err := s.NewCommits(ctx, u, 3)
	require.NoError(t, err)
	assert.Len(t, commits, 3)
}

func TestMemoised(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1", map[string]string{"a": "1"})
	f.publish(c1 + ":refs/heads/main")
	c2 := f.commit("2", map[string]string{"b": "1"})
	f.stage(c2)
	s, c := f.set("refs/heads/main")
	u := update(c1, c2, "refs/heads/feature")
	for range 3 {
		_, err := s.CandidatePaths(ctx, u)
		require.NoError(t, err)
		_, err = s.NewCommits(ctx, u, MaxPathCommits)
		require.NoError(t, err)
		_, err = s.Paths(ctx, u, other)
		require.NoError(t, err)
	}
	assert.Equal(t, 2, c.revLists, "one rev-list per ref and view: old..new of the candidates, then the new commits")
}

// fixtureGit is the git of the fixtures: GIT_BIN when it is a path. exec looks
// a bare name up in the PATH of the test process, not in the child's.
func fixtureGit() string {
	if bin := os.Getenv("GIT_BIN"); bin != "" && bin != "git" {
		return bin
	}
	return "git"
}

func TestEnteringCommits_FirstParent(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base", map[string]string{"a": "1"})
	f.publish(base + ":refs/heads/main")
	f.git(f.work, "checkout", "-q", "-b", "feature")
	side := f.commit("side", map[string]string{"b": "1"})
	f.publish(side + ":refs/heads/feature")
	f.git(f.work, "checkout", "-q", "main")
	f.git(f.work, "merge", "-q", "--no-ff", "-m", "merge", "feature")
	merge := f.git(f.work, "rev-parse", "HEAD")
	f.stage(merge)
	s, _ := f.set("refs/heads/main")
	u := update(base, merge, "refs/heads/main")
	all, err := s.EnteringCommits(ctx, u, def, 10)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{merge, side}, all)
	fp, err := s.EnteringCommits(ctx, u, module.Scope{Class: module.ClassDefault, FirstParent: true}, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{merge}, fp, "the merged branch commit is not on the first-parent chain")
	created, err := s.EnteringCommits(ctx, update(zero, merge, "refs/heads/copy"), module.Scope{Class: module.ClassOther, FirstParent: true}, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{merge}, created)
	paths, err := s.Paths(ctx, update(zero, merge, "refs/heads/copy"), module.Scope{Class: module.ClassProtected, Key: "x", FirstParent: true})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"b"}, paths, "paths ignore FirstParent")
}

func (f *fixture) state(rev, path string) module.PathState {
	f.t.Helper()
	return module.PathState{Mode: "100644", OID: f.git(f.work, "rev-parse", rev+":"+path)}
}

func TestKnownStates_Rebase(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base", map[string]string{"ci.yml": "v1", "app.go": "a"})
	f.git(f.work, "checkout", "-q", "-b", "feature")
	alice := f.commit("alice", map[string]string{"ci.yml": "v2"})
	f.git(f.work, "checkout", "-q", "main")
	fresh := f.commit("main", map[string]string{"app.go": "b"})
	f.publish(fresh+":refs/heads/main", alice+":refs/heads/feature")
	f.git(f.work, "checkout", "-q", "feature")
	f.git(f.work, "rebase", "-q", "main")
	rebased := f.git(f.work, "rev-parse", "HEAD")
	f.stage(rebased)
	s, _ := f.set("refs/heads/main")
	u := update(alice, rebased, "refs/heads/feature")

	changes, err := s.NewPathChanges(ctx, u)
	require.NoError(t, err)
	assert.Equal(t, []module.PathChange{{Commit: rebased, Path: "ci.yml", State: f.state(alice, "ci.yml")}}, changes)
	known, err := s.KnownStates(ctx, u, []string{"ci.yml"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []module.PathState{f.state(base, "ci.yml"), f.state(alice, "ci.yml")}, known["ci.yml"])
}

func TestKnownStates_OnlyTheBranch(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1", map[string]string{"ci.yml": "v1"})
	c2 := f.commit("2", map[string]string{"ci.yml": "v2"})
	f.git(f.work, "checkout", "-q", "-b", "feature", c1)
	v3 := f.commit("3", map[string]string{"ci.yml": "v3"})
	tip := f.commit("app", map[string]string{"app.go": "x"})
	f.publish(c2+":refs/heads/main", tip+":refs/heads/feature")
	s, _ := f.set("refs/heads/main")

	known, err := s.KnownStates(ctx, update(tip, tip, "refs/heads/feature"), []string{"ci.yml", "app.go"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []module.PathState{f.state(c2, "ci.yml"), f.state(v3, "ci.yml")}, known["ci.yml"],
		"the state of main before the branch changed the path is not known")
	assert.ElementsMatch(t, []module.PathState{{}, f.state(tip, "app.go")}, known["app.go"], "a missing path is a state")

	known, err = s.KnownStates(ctx, update(zero, tip, "refs/heads/feature-2"), []string{"ci.yml"})
	require.NoError(t, err)
	assert.Equal(t, []module.PathState{f.state(c2, "ci.yml")}, known["ci.yml"], "a new branch knows only the default branch")
}

func TestKnownStates_CommitKeepsForkState(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1", map[string]string{"ci.yml": "v1"})
	c2 := f.commit("2", map[string]string{"ci.yml": "v2"})
	f.git(f.work, "checkout", "-q", "-b", "feature", c1)
	f.commit("app", map[string]string{"app.go": "x"})
	tip := f.commit("3", map[string]string{"ci.yml": "v3"})
	f.publish(c2+":refs/heads/main", tip+":refs/heads/feature")
	s, _ := f.set("refs/heads/main")

	known, err := s.KnownStates(ctx, update(tip, tip, "refs/heads/feature"), []string{"ci.yml"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []module.PathState{f.state(c1, "ci.yml"), f.state(c2, "ci.yml"), f.state(tip, "ci.yml")}, known["ci.yml"],
		"a commit of the branch that keeps the state of the fork point has that state")
}

func TestKnownStates_NoDefaultBranch(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1", map[string]string{"ci.yml": "v1"})
	c2 := f.commit("2", map[string]string{"ci.yml": "v2"})
	f.publish(c2 + ":refs/heads/feature")
	s, _ := f.set("refs/heads/main")

	known, err := s.KnownStates(ctx, update(c2, c2, "refs/heads/feature"), []string{"ci.yml"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []module.PathState{f.state(c1, "ci.yml"), f.state(c2, "ci.yml")}, known["ci.yml"])
}

// failing is a repository whose methods fail on demand: by method name, or
// the n-th call of RevList.
type failing struct {
	Git
	errs        map[string]error
	revListFail int
	revLists    int
}

var errGit = errors.New("git failed")

func (f *failing) Peel(ctx context.Context, rev string) (string, error) {
	if err := f.errs["Peel "+rev]; err != nil {
		return "", err
	}
	return f.Git.Peel(ctx, rev)
}

func (f *failing) DiffNames(ctx context.Context, a, b string) ([]string, error) {
	if err := f.errs["DiffNames"]; err != nil {
		return nil, err
	}
	return f.Git.DiffNames(ctx, a, b)
}

func (f *failing) CommitPaths(ctx context.Context, commits []string) ([]string, error) {
	if err := f.errs["CommitPaths"]; err != nil {
		return nil, err
	}
	return f.Git.CommitPaths(ctx, commits)
}

func (f *failing) RevList(ctx context.Context, o gitx.RevListOptions) ([]string, error) {
	f.revLists++
	if f.revLists == f.revListFail {
		return nil, f.errs["RevList"]
	}
	return f.Git.RevList(ctx, o)
}

func (f *failing) CommitChanges(ctx context.Context, commits []string) ([]gitx.Commit, error) {
	if err := f.errs["CommitChanges"]; err != nil {
		return nil, err
	}
	return f.Git.CommitChanges(ctx, commits)
}

func (f *failing) Entries(ctx context.Context, commit string, paths []string) (map[string]gitx.Info, error) {
	if err := f.errs["Entries "+commit]; err != nil {
		return nil, err
	}
	return f.Git.Entries(ctx, commit, paths)
}

// sharedFixture is main with ci.yml v1 and a branch with ci.yml v2 on top,
// pushed again with one more commit.
func sharedFixture(t *testing.T) (*fixture, string, string, string) {
	t.Helper()
	f := newFixture(t)
	base := f.commit("base", map[string]string{"ci.yml": "v1"})
	f.git(f.work, "checkout", "-q", "-b", "feature")
	old := f.commit("v2", map[string]string{"ci.yml": "v2"})
	f.publish(base+":refs/heads/main", old+":refs/heads/feature")
	head := f.commit("v3", map[string]string{"ci.yml": "v3"})
	f.stage(head)
	return f, base, old, head
}

func (f *fixture) failingSet(g *failing) *Set {
	f.t.Helper()
	repo, err := gitx.Open(gitx.Env{Dir: f.bare, Path: f.path})
	require.NoError(f.t, err)
	f.t.Cleanup(func() { assert.NoError(f.t, repo.Close()) })
	g.Git = repo
	return New(g, "refs/heads/main")
}

// TestKnownStates_Errors: a failure of git stops the check instead of
// leaving out known states, and only a missing default branch means no anchor.
func TestKnownStates_Errors(t *testing.T) {
	f, base, old, head := sharedFixture(t)
	u := update(old, head, "refs/heads/feature")
	for name, g := range map[string]*failing{
		"peel of the default branch": {errs: map[string]error{"Peel refs/heads/main": errGit}},
		"peel of the old tip":        {errs: map[string]error{"Peel " + old: errGit}},
		"entries of the anchor":      {errs: map[string]error{"Entries " + base: errGit}},
		"entries of the old tip":     {errs: map[string]error{"Entries " + old: errGit}},
		"rev-list of the branch":     {errs: map[string]error{"RevList": errGit}, revListFail: 1},
		"changes of the branch":      {errs: map[string]error{"CommitChanges": errGit}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.failingSet(g).KnownStates(ctx, u, []string{"ci.yml"})
			assert.ErrorIs(t, err, errGit)
		})
	}
	t.Run("missing default branch", func(t *testing.T) {
		g := &failing{errs: map[string]error{"Peel refs/heads/main": gitx.ErrMissing}}
		known, err := f.failingSet(g).KnownStates(ctx, u, []string{"ci.yml"})
		require.NoError(t, err)
		assert.Contains(t, known["ci.yml"], f.state(old, "ci.yml"), "without an anchor the branch is all history")
	})
	t.Run("limit of the branch", func(t *testing.T) {
		g := &failing{errs: map[string]error{"RevList": gitx.ErrLimit}, revListFail: 1}
		_, err := f.failingSet(g).KnownStates(ctx, u, []string{"ci.yml"})
		assert.ErrorIs(t, err, module.ErrLimitExceeded)
	})
}

// TestNewPathChanges_ErrorsAndMemo: the changes of the new commits are read
// once per ref, and a failure of git is returned.
func TestNewPathChanges_ErrorsAndMemo(t *testing.T) {
	f, _, old, head := sharedFixture(t)
	u := update(old, head, "refs/heads/feature")
	s, c := f.set("refs/heads/main")
	first, err := s.NewPathChanges(ctx, u)
	require.NoError(t, err)
	again, err := s.NewPathChanges(ctx, u)
	require.NoError(t, err)
	assert.Equal(t, first, again)
	assert.Equal(t, 1, c.revLists, "the second call uses the memo")

	_, err = f.failingSet(&failing{errs: map[string]error{"CommitChanges": errGit}}).NewPathChanges(ctx, u)
	assert.ErrorIs(t, err, errGit)
}

// TestPaths_DefaultBranchErrors: a failure of git while the paths of an
// update of the default branch are listed is returned, and more entering
// commits than the limit is ErrLimitExceeded.
func TestPaths_DefaultBranchErrors(t *testing.T) {
	f, base, _, head := sharedFixture(t)
	u := update(base, head, "refs/heads/main")
	for name, g := range map[string]*failing{
		"diff":         {errs: map[string]error{"DiffNames": errGit}},
		"commit paths": {errs: map[string]error{"CommitPaths": errGit}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.failingSet(g).Paths(ctx, u, def)
			assert.ErrorIs(t, err, errGit)
		})
	}
	t.Run("limit", func(t *testing.T) {
		g := &failing{errs: map[string]error{"RevList": gitx.ErrLimit}, revListFail: 1}
		_, err := f.failingSet(g).Paths(ctx, u, def)
		assert.ErrorIs(t, err, module.ErrLimitExceeded)
	})
}
