package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPath returns PATH for git in tests: the directory of GIT_BIN first when it is a path.
func testPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("PATH")
	if bin := os.Getenv("GIT_BIN"); bin != "" && bin != "git" {
		path = filepath.Dir(bin) + string(filepath.ListSeparator) + path
	}
	return path
}

type fixture struct {
	t    *testing.T
	work string
	bare string
	path string
	env  []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := testPath(t)
	f := &fixture{t: t, work: filepath.Join(t.TempDir(), "work"), bare: filepath.Join(t.TempDir(), "repo.git"), path: path}
	f.env = []string{"PATH=" + path, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "HOME=" + t.TempDir(),
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com",
		"GIT_AUTHOR_DATE=2026-10-13T12:00:00Z", "GIT_COMMITTER_DATE=2026-10-13T12:00:00Z"}
	f.git("", "init", "-q", "-b", "main", f.work)
	f.git("", "init", "-q", "--bare", "-b", "main", f.bare)
	return f
}

func (f *fixture) git(dir string, args ...string) string {
	f.t.Helper()
	cmd := exec.CommandContext(f.t.Context(), fixtureGit(), args...)
	cmd.Env = f.env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(f.t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func (f *fixture) write(name, content string) {
	f.t.Helper()
	p := filepath.Join(f.work, name)
	require.NoError(f.t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(f.t, os.WriteFile(p, []byte(content), 0o644))
}

func (f *fixture) commit(msg string) string {
	f.t.Helper()
	f.git(f.work, "add", "-A")
	f.git(f.work, "commit", "-q", "--allow-empty", "-m", msg)
	return f.git(f.work, "rev-parse", "HEAD")
}

func (f *fixture) push(refspecs ...string) {
	f.t.Helper()
	f.git(f.work, append([]string{"push", "-q", "-f", f.bare}, refspecs...)...)
}

func (f *fixture) open() *Repo {
	f.t.Helper()
	r, err := Open(Env{Dir: f.bare, Path: f.path})
	require.NoError(f.t, err)
	f.t.Cleanup(func() { assert.NoError(f.t, r.Close()) })
	return r
}

func TestOpen(t *testing.T) {
	_, err := Open(Env{Dir: "/x", Path: "/nonexistent"})
	assert.ErrorContains(t, err, "git not found")
	_, err = Open(Env{Path: "/usr/bin"})
	assert.ErrorContains(t, err, "GIT_DIR is not set")
}

func TestOpen_IsolatesChildren(t *testing.T) {
	f := newFixture(t)
	r := f.open()
	env := strings.Join(r.env, "\n")
	for _, v := range []string{"GIT_NO_REPLACE_OBJECTS=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_DIR=" + f.bare} {
		assert.Contains(t, env, v)
	}
	assert.NotContains(t, env, "GIT_OBJECT_DIRECTORY")
	cwd, err := os.Getwd()
	require.NoError(t, err)
	r, err = Open(Env{Dir: ".", ObjectDirectory: "objects/incoming", AlternateObjectDirs: "objects:alt", Path: f.path})
	require.NoError(t, err)
	env = strings.Join(r.env, "\n")
	assert.Contains(t, env, "GIT_DIR="+cwd+"\n")
	assert.Contains(t, env, "GIT_OBJECT_DIRECTORY="+filepath.Join(cwd, "objects/incoming"))
	assert.Contains(t, env, "GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(cwd, "objects")+":"+filepath.Join(cwd, "alt"))
}

func TestVersionAndRefs(t *testing.T) {
	f := newFixture(t)
	f.commit("one")
	f.push("HEAD:refs/heads/main", "HEAD:refs/heads/feature", "HEAD:refs/tags/v1")
	r := f.open()
	ctx := context.Background()
	v, err := r.Version(ctx)
	require.NoError(t, err)
	assert.Regexp(t, `^\d+\.\d+`, v)
	ref, err := r.HeadRef(ctx)
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/main", ref)
	refs, err := r.Refs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/feature", "refs/heads/main", "refs/tags/v1"}, refs)
}

func TestEntries(t *testing.T) {
	f := newFixture(t)
	f.write("PATHPOLICY", "version: 1\n")
	f.write("COMMITPOLICY/inner", "x")
	f.write("run.sh", "#!/bin/sh\n")
	require.NoError(t, os.Chmod(filepath.Join(f.work, "run.sh"), 0o755))
	require.NoError(t, os.Symlink("PATHPOLICY", filepath.Join(f.work, "TAGPOLICY")))
	f.git(f.work, "add", "-A")
	f.git(f.work, "update-index", "--add", "--cacheinfo", "160000,1111111111111111111111111111111111111111,GITLINK")
	f.git(f.work, "commit", "-q", "-m", "tree")
	c := f.git(f.work, "rev-parse", "HEAD")
	f.push("HEAD:refs/heads/main")
	r := f.open()
	ctx := context.Background()
	tests := []struct {
		path, mode, typ string
		size            int64
	}{
		{"PATHPOLICY", ModeFile, "blob", 11},
		{"run.sh", ModeExecutable, "blob", 10},
		{"COMMITPOLICY", ModeTree, "tree", 0},
		{"TAGPOLICY", ModeSymlink, "blob", 10},
		{"GITLINK", ModeGitlink, "commit", 0},
	}
	paths := make([]string, 0, 1+len(tests))
	paths = append(paths, "NONE")
	for _, tt := range tests {
		paths = append(paths, tt.path)
	}
	entries, err := r.Entries(ctx, c, paths)
	require.NoError(t, err)
	assert.Len(t, entries, len(tests), "a path without an entry is not in the map")
	for _, tt := range tests {
		info := entries[tt.path]
		assert.Equal(t, tt.mode, info.Mode, tt.path)
		assert.Equal(t, tt.typ, info.Type, tt.path)
		assert.Equal(t, tt.size, info.Size, tt.path)
	}
	entries, err = r.Entries(ctx, c, nil)
	require.NoError(t, err)
	assert.Empty(t, entries, "no paths is not the whole root tree")
	_, err = r.Entries(ctx, c, []string{"/abs"})
	assert.Error(t, err)
	_, err = r.Entries(ctx, "HEAD", []string{"PATHPOLICY"})
	assert.Error(t, err)
}

func TestRead(t *testing.T) {
	f := newFixture(t)
	f.write("A", "hello\n")
	c := f.commit("one")
	f.push("HEAD:refs/heads/main")
	r := f.open()
	ctx := context.Background()
	entries, err := r.Entries(ctx, c, []string{"A"})
	require.NoError(t, err)
	info := entries["A"]
	got, data, err := r.Read(ctx, info.OID, 100)
	require.NoError(t, err)
	assert.Equal(t, "blob", got.Type)
	assert.Equal(t, "hello\n", string(data))
	_, _, err = r.Read(ctx, info.OID, 3)
	assert.ErrorIs(t, err, ErrTooLarge)
	_, data, err = r.Read(ctx, c, 1000)
	require.NoError(t, err)
	assert.Contains(t, string(data), "\n\none\n")
	_, _, err = r.Read(ctx, strings.Repeat("1", 40), 10)
	assert.ErrorIs(t, err, ErrMissing)
	_, _, err = r.Read(ctx, "HEAD", 10)
	assert.Error(t, err)
}

func TestPeel(t *testing.T) {
	f := newFixture(t)
	f.write("A", "a")
	c := f.commit("one")
	f.git(f.work, "tag", "-a", "-m", "t", "v1")
	f.git(f.work, "tag", "-a", "-m", "tt", "v1-of-tag", "v1")
	tree := f.git(f.work, "rev-parse", "HEAD^{tree}")
	f.git(f.work, "tag", "-a", "-m", "tree", "v-tree", tree)
	f.push("HEAD:refs/heads/main", "refs/tags/*:refs/tags/*")
	r := f.open()
	ctx := context.Background()
	for _, tag := range []string{"v1", "v1-of-tag"} {
		oid := f.git(f.bare, "rev-parse", "refs/tags/"+tag)
		got, err := r.Peel(ctx, oid)
		require.NoError(t, err)
		assert.Equal(t, c, got)
	}
	_, err := r.Peel(ctx, f.git(f.bare, "rev-parse", "refs/tags/v-tree"))
	assert.ErrorIs(t, err, ErrNotCommit)
	_, err = r.Peel(ctx, "HEAD~1")
	assert.Error(t, err)
}

func TestDiffAndCommitPaths(t *testing.T) {
	f := newFixture(t)
	f.write("A", "a")
	f.write("dir/B", "b")
	root := f.commit("root")
	f.write("A", "a2")
	f.write("weird\nname", "x")
	f.write(strings.Repeat("a", 40), "hex-named file")
	two := f.commit("two")
	f.git(f.work, "checkout", "-q", "-b", "side", root)
	f.write("S", "s")
	side := f.commit("side")
	f.git(f.work, "checkout", "-q", "main")
	f.git(f.work, "merge", "-q", "--no-edit", "side")
	f.write("EVIL", "e")
	f.git(f.work, "add", "-A")
	f.git(f.work, "commit", "-q", "--amend", "--no-edit")
	merge := f.git(f.work, "rev-parse", "HEAD")
	empty := f.commit("empty")
	f.push("HEAD:refs/heads/main")
	r := f.open()
	ctx := context.Background()

	names, err := r.DiffNames(ctx, root, merge)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"A", "EVIL", "S", "weird\nname", strings.Repeat("a", 40)}, names)

	paths, err := r.CommitPaths(ctx, []string{empty, merge, side, two, root})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"EVIL", "S", "A", "weird\nname", strings.Repeat("a", 40), "dir/B"}, paths,
		"merge shows only the evil change, root commit shows its whole tree")
	paths, err = r.CommitPaths(ctx, []string{merge})
	require.NoError(t, err)
	assert.Equal(t, []string{"EVIL"}, paths)
	_, err = r.CommitPaths(ctx, []string{"HEAD"})
	assert.Error(t, err)
	_, err = r.DiffNames(ctx, "--output=/tmp/x", root)
	assert.Error(t, err)
}

func TestCommitChanges(t *testing.T) {
	f := newFixture(t)
	f.write("A", "a")
	root := f.commit("root")
	f.write("A", "a2")
	f.write("weird\nname", "x")
	two := f.commit("two")
	f.git(f.work, "checkout", "-q", "-b", "side", root)
	f.write("S", "s")
	side := f.commit("side")
	f.git(f.work, "checkout", "-q", "main")
	f.git(f.work, "merge", "-q", "--no-edit", "side")
	f.write("EVIL", "e")
	f.git(f.work, "add", "-A")
	f.git(f.work, "commit", "-q", "--amend", "--no-edit")
	merge := f.git(f.work, "rev-parse", "HEAD")
	require.NoError(t, os.Chmod(filepath.Join(f.work, "A"), 0o755))
	require.NoError(t, os.Remove(filepath.Join(f.work, "S")))
	modes := f.commit("modes")
	empty := f.commit("empty")
	f.push("HEAD:refs/heads/main")
	r := f.open()
	ctx := context.Background()
	oid := func(rev string) string { return f.git(f.work, "rev-parse", rev) }

	commits, err := r.CommitChanges(ctx, []string{root, two, merge, modes, empty})
	require.NoError(t, err)
	require.Len(t, commits, 5, "every commit is listed, also one without changes")
	assert.Equal(t, Commit{OID: empty, Parents: []string{modes}}, commits[4])
	assert.Equal(t, []string{}, commits[0].Parents)
	assert.Equal(t, []string{root}, commits[1].Parents)
	assert.Equal(t, []string{two, side}, commits[2].Parents)
	byPath := map[string][]Change{}
	for _, c := range commits {
		for _, ch := range c.Changes {
			byPath[ch.Path] = append(byPath[ch.Path], ch)
		}
	}
	require.Len(t, byPath["A"], 3)
	assert.Equal(t, Change{Path: "A", From: []State{{}}, To: State{Mode: "100644", OID: oid(root + ":A")}}, byPath["A"][0],
		"a root commit adds its whole tree")
	assert.Equal(t, State{Mode: "100644", OID: oid(root + ":A")}, byPath["A"][1].From[0])
	assert.Equal(t, State{Mode: "100755", OID: oid(two + ":A")}, byPath["A"][2].To, "a mode change keeps the blob")
	assert.Equal(t, []Change{{Path: "EVIL", From: []State{{}, {}}, To: State{Mode: "100644", OID: oid(merge + ":EVIL")}}}, commits[2].Changes,
		"a merge shows only what differs from every parent")
	require.Len(t, byPath["S"], 1, "the merge takes S from side as it is")
	assert.Equal(t, State{}, byPath["S"][0].To, "a deletion leaves no state")
	assert.Len(t, byPath["weird\nname"], 1)
	_, err = r.CommitChanges(ctx, []string{"HEAD"})
	assert.Error(t, err)
}

func TestOnBranch(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1")
	f.push("HEAD:refs/heads/main")
	c2 := f.commit("2")
	f.push("HEAD:refs/heads/feature", "HEAD:refs/heads/feature-2")
	c3 := f.commit("3")
	f.push(c3 + ":refs/tags/v3")
	r := f.open()
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		commit     string
		tips, skip []string
		want       bool
	}{
		{name: "in a branch", commit: c1, want: true},
		{name: "in no branch", commit: c3},
		{name: "in a branch of the same prefix", commit: c2, skip: []string{"feature"}, want: true},
		{name: "in skipped branches only", commit: c2, skip: []string{"feature", "feature-2"}},
		{name: "in a new tip", commit: c3, tips: []string{c3}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			on, err := r.OnBranch(ctx, tc.commit, tc.tips, tc.skip)
			require.NoError(t, err)
			assert.Equal(t, tc.want, on)
		})
	}
	_, err := r.OnBranch(ctx, c1, nil, []string{"feature*"})
	assert.Error(t, err)
	_, err = r.OnBranch(ctx, "HEAD", nil, nil)
	assert.Error(t, err)
}

func TestRevList(t *testing.T) {
	f := newFixture(t)
	c1 := f.commit("1")
	f.push("HEAD:refs/heads/main")
	c2 := f.commit("2")
	c3 := f.commit("3")
	f.push("HEAD:refs/heads/other")
	r := f.open()
	ctx := context.Background()
	got, err := r.RevList(ctx, RevListOptions{Include: []string{c3}, Exclude: []string{"refs/heads/main"}, Max: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{c3, c2}, got)
	got, err = r.RevList(ctx, RevListOptions{Include: []string{c3}, NotBranchesTags: true, Max: 10})
	require.NoError(t, err)
	assert.Empty(t, got, "everything is reachable from branches")
	_, err = r.RevList(ctx, RevListOptions{Include: []string{c3}, Exclude: []string{c1}, Max: 1})
	assert.ErrorIs(t, err, ErrLimit)
	_, err = r.RevList(ctx, RevListOptions{Include: []string{"--all"}, Max: 1})
	assert.Error(t, err)

	f.git(f.work, "checkout", "-q", "-b", "side", c1)
	side := f.commit("side")
	f.git(f.work, "checkout", "-q", "-")
	f.git(f.work, "merge", "-q", "--no-ff", "-m", "merge", "side")
	merge := f.git(f.work, "rev-parse", "HEAD")
	f.push("HEAD:refs/heads/merged")
	got, err = r.RevList(ctx, RevListOptions{Include: []string{merge}, Exclude: []string{c3}, Max: 10})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{merge, side}, got)
	got, err = r.RevList(ctx, RevListOptions{Include: []string{merge}, Exclude: []string{c3}, FirstParent: true, Max: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{merge}, got, "the side branch is not on the first-parent chain")
}

func FuzzLsTree(f *testing.F) {
	f.Add("100644 blob 587be6b4c3f93f93c489c0111bba5596147a26cb       2\tA")
	f.Add("040000 tree f13d7a9fb7393bc2311e55592fc17d8814d98eb6       -\tD")
	f.Fuzz(func(t *testing.T, rec string) {
		info, _, err := parseLsTree(rec)
		if err == nil && (!IsOID(info.OID) || info.Size < 0) {
			t.Fatalf("accepted %q as %+v", rec, info)
		}
	})
}

func FuzzCatFileHeader(f *testing.F) {
	f.Add("blob 587be6b4c3f93f93c489c0111bba5596147a26cb 2")
	f.Add("HEAD:none missing")
	f.Add("commit 371891cad60e0c06ff80b9026658d2695bc572c2 230")
	f.Fuzz(func(t *testing.T, line string) {
		info, err := parseHeader("spec", line)
		if err == nil && (!IsOID(info.OID) || info.Size < 0) {
			t.Fatalf("accepted %q as %+v", line, info)
		}
	})
}

func FuzzCommitChanges(f *testing.F) {
	f.Add([]byte("/5ad379 371891\n:100644 100644 587be6b4c3f93f93c489c0111bba5596147a26cb 371891cad60e0c06ff80b9026658d2695bc572c2 M\x00A\x00"))
	f.Add([]byte("/5ad379\x00/371891 5ad379\x00"))
	f.Add([]byte("/5ad379 a b\n::100644 100644 100644 587be6b4c3f93f93c489c0111bba5596147a26cb 587be6b4c3f93f93c489c0111bba5596147a26cb 371891cad60e0c06ff80b9026658d2695bc572c2 MM\x00E\x00"))
	f.Fuzz(func(t *testing.T, out []byte) {
		commits, err := parseCommitChanges(out)
		if err != nil {
			return
		}
		for _, c := range commits {
			for _, ch := range c.Changes {
				if len(ch.From) == 0 {
					t.Fatalf("a change without the states of its parents: %+v", ch)
				}
			}
		}
	})
}

func FuzzCommitPaths(f *testing.F) {
	f.Add([]byte("/5ad379\x00/371891\nE\x00\x00/568c\nA\x00G\x00"))
	f.Fuzz(func(t *testing.T, out []byte) {
		for _, p := range parseCommitPaths(out) {
			if p == "" || strings.Contains(p, "\x00") {
				t.Fatalf("bad path %q", p)
			}
		}
	})
}

// fixtureGit is the git of the fixtures: GIT_BIN when it is a path. exec looks
// a bare name up in the PATH of the test process, not in the child's.
func fixtureGit() string {
	if bin := os.Getenv("GIT_BIN"); bin != "" && bin != "git" {
		return bin
	}
	return "git"
}

func TestDiscoverAndResolve(t *testing.T) {
	f := newFixture(t)
	c := f.commit("one")
	dir, err := Discover(context.Background(), f.path, f.work)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(f.work, ".git"), dir)
	_, err = Discover(context.Background(), f.path, t.TempDir())
	assert.Error(t, err)
	r, err := Open(Env{Dir: dir, Path: f.path})
	require.NoError(t, err)
	defer func() { assert.NoError(t, r.Close()) }()
	got, err := r.ResolveCommit(context.Background(), "HEAD")
	require.NoError(t, err)
	assert.Equal(t, c, got)
	_, err = r.ResolveCommit(context.Background(), "--all")
	assert.Error(t, err)
	_, err = r.ResolveCommit(context.Background(), "nope")
	assert.ErrorIs(t, err, ErrMissing)
}
