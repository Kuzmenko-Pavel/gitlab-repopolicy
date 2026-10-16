package changes

import (
	"context"
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
	assert.Equal(t, 2, c.revLists)
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
	assert.Equal(t, 1, c.revLists, "one rev-list per ref and view")
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
