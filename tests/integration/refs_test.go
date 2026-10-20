//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// featureStand has GUARDPOLICY on main and a feature branch; no branch is protected in GitLab.
func featureStand(t *testing.T, policy string, files map[string]string) *Stand {
	t.Helper()
	all := map[string]string{"GUARDPOLICY": policy}
	for k, v := range files {
		all[k] = v
	}
	s, _ := mainWith(t, all)
	s.Seed("HEAD:refs/heads/feature")
	s.Protected(nil, nil)
	return s
}

func TestPaths(t *testing.T) {
	cases := map[string]func(s *Stand){
		"RenameInto": func(s *Stand) { s.Commit("move in", map[string]string{"src/conf.yml": "", "deploy/conf.yml": "x"}) },
		"RenameOut":  func(s *Stand) { s.Commit("move out", map[string]string{"deploy/conf.yml": "", "src/conf.yml": "x"}) },
		"Delete":     func(s *Stand) { s.Commit("delete", map[string]string{"deploy/conf.yml": ""}) },
		"Chmod": func(s *Stand) {
			require.NoError(t, os.Chmod(filepath.Join(s.Work, "deploy/conf.yml"), 0o755))
			s.Commit("chmod", nil)
		},
		"Symlink": func(s *Stand) {
			require.NoError(t, os.Symlink("../src/conf.yml", filepath.Join(s.Work, "deploy/link.yml")))
			s.Commit("symlink", nil)
		},
		"Gitlink": func(s *Stand) {
			s.git(s.Work, "update-index", "--add", "--cacheinfo", "160000,"+s.Rev("HEAD")+",deploy/sub")
			s.git(s.Work, "commit", "-q", "-m", "gitlink")
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{"src/conf.yml": "x"}
			if name != "RenameInto" {
				files = map[string]string{"deploy/conf.yml": "x"}
			}
			s := featureStand(t, guardAll, files)
			s.git(s.Work, "checkout", "-q", "-B", "feature")
			change(s)
			assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "GUARDPOLICY rule \"ci\", branch feature")
			assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
		})
	}
}

func TestRefs_FastForward(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Commit("app", map[string]string{"app.go": "x"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
	s.Commit("ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "ci.yml")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
}

func TestRefs_ForcePushRollbackDefault(t *testing.T) {
	s, base := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "v1"})
	s.Commit("ci v2", map[string]string{"ci.yml": "v2"})
	s.Seed("HEAD:refs/heads/main")
	s.git(s.Work, "reset", "-q", "--hard", base)
	r := s.Push(bob, "HEAD:refs/heads/main")
	assertDenied(t, r, "GUARDPOLICY rule \"ci\", branch main", "ci.yml")
}

func TestRefs_ForcePushRollbackProtected(t *testing.T) {
	s, base := mainWith(t, map[string]string{"GUARDPOLICY": guardRelease, "ci.yml": "v1"})
	s.Commit("ci v2", map[string]string{"ci.yml": "v2"})
	s.Seed("HEAD:refs/heads/release/1")
	s.git(s.Work, "reset", "-q", "--hard", base)
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/release/1"), "GUARDPOLICY rule \"release\", branch release/1")
}

func TestRefs_RebaseFeatureOnMain(t *testing.T) {
	s := featureStand(t, guardAll, map[string]string{"app.go": "a", "ci.yml": "v1"})
	s.git(s.Work, "checkout", "-q", "-B", "feature")
	s.Commit("mine", map[string]string{"app.go": "b"})
	s.Seed("HEAD:refs/heads/feature")
	s.git(s.Work, "checkout", "-q", "main")
	s.Commit("devops", map[string]string{"ci.yml": "v2"})
	s.Seed("HEAD:refs/heads/main")
	s.git(s.Work, "checkout", "-q", "feature")
	s.git(s.Work, "rebase", "-q", "main")
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
}

func TestRefs_MergeIntoListedRefNetDiff(t *testing.T) {
	s, base := mainWith(t, map[string]string{"GUARDPOLICY": guardRelease, "ci.yml": "v1"})
	s.Seed("HEAD:refs/heads/release/1")
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("alice changes ci", map[string]string{"ci.yml": "v2"})
	s.Seed("HEAD:refs/heads/feature")
	s.git(s.Work, "checkout", "-q", "-B", "rel", base)
	s.git(s.Work, "merge", "-q", "--no-ff", "-m", "merge", "feature")
	r := s.Push(bob, "HEAD:refs/heads/release/1")
	assertDenied(t, r, "GUARDPOLICY rule \"release\", branch release/1", "ci.yml")
	assert.Zero(t, s.ProtectedCalls(), "a ref from the rule's own list needs no API")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/release/1"))
}

func TestRefs_CreateProtectedFromFeature(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardRelease})
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("ci on feature", map[string]string{"ci.yml": "x"})
	s.Seed("HEAD:refs/heads/feature")
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/release/2"), "GUARDPOLICY rule \"release\", branch release/2", "ci.yml")
}

func TestRefs_BranchFromOldCommit(t *testing.T) {
	s, base := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "v1"})
	s.Commit("ci v2", map[string]string{"ci.yml": "v2"})
	s.Seed("HEAD:refs/heads/main")
	s.Protected(nil, nil)
	assertAllowed(t, s.Push(bob, base+":refs/heads/old"))
	assert.Zero(t, s.ProtectedCalls(), "no candidate paths, no API")
}

func TestRefs_BranchFromMergeRequestRef(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.git(s.Work, "checkout", "-q", "-b", "fork")
	c := s.Commit("from a fork", map[string]string{"ci.yml": "x"})
	s.Seed("HEAD:refs/merge-requests/1/head")
	s.Protected(nil, nil)
	assertDenied(t, s.Push(bob, c+":refs/heads/taken"), "GUARDPOLICY rule \"ci\", branch taken", "ci.yml")
}

// TestRefs_AnnotatedTag: the commit of a tag is checked as part of its
// branch, so a tag on a commit of a branch passes whoever pushes it.
func TestRefs_AnnotatedTag(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Protected(nil, []string{"v*"})
	s.Commit("ci", map[string]string{"ci.yml": "x"})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	s.git(s.Work, "tag", "-a", "-m", "release", "v1")
	assertAllowed(t, s.Push(bob, "refs/tags/v1"))
}

func TestRefs_SignedTag(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Protected(nil, nil)
	c := s.Commit("ci", map[string]string{"ci.yml": "x"})
	// A tag object with a signature block, written by git mktag: the core
	// treats it as any annotated tag; signatures are TAGPOLICY's business.
	obj := fmt.Sprintf("object %s\ntype commit\ntag v2\ntagger dev <dev@example.com> 1760000000 +0000\n\nsigned\n"+
		"-----BEGIN PGP SIGNATURE-----\n\nAAAA\n-----END PGP SIGNATURE-----\n", c)
	tag := s.gitStdin(obj, s.Work, "mktag")
	s.git(s.Work, "update-ref", "refs/tags/v2", tag)
	assertDenied(t, s.Push(bob, "refs/tags/v2"), "tag v2: the tag points to a commit that is in no branch")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	assertAllowed(t, s.Push(bob, "refs/tags/v2"))
}

func TestRefs_ReplaceRefsIgnored(t *testing.T) {
	s, base := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "v1"})
	evil := s.Commit("change ci", map[string]string{"ci.yml": "v2"})
	s.git(s.Work, "reset", "-q", "--hard", base)
	innocent := s.Commit("innocent", map[string]string{"app.go": "x"})
	// A replace ref that makes the evil commit look like the innocent one.
	s.git(s.Work, "replace", evil, innocent)
	s.Seed("refs/replace/" + evil)
	s.git(s.Work, "replace", "-d", evil)
	s.git(s.Work, "reset", "-q", "--hard", evil)
	r := s.Push(bob, "HEAD:refs/heads/main")
	assertDenied(t, r, "ci.yml")
}

func TestRefs_MultipleRefs(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Protected(nil, nil)
	s.Commit("app", map[string]string{"app.go": "x"})
	s.git(s.Work, "branch", "fine")
	s.Commit("ci", map[string]string{"ci.yml": "x"})
	r := s.Push(bob, "fine:refs/heads/fine", "HEAD:refs/heads/bad")
	assertDenied(t, r, "refs/heads/bad")
	assert.Equal(t, "", s.RemoteRev("refs/heads/fine"), "one rejected ref rejects the whole push")
}

func TestRefs_DeleteWithoutQuarantine(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "x"})
	s.Seed("HEAD:refs/heads/old")
	assertAllowed(t, s.Push(bob, ":refs/heads/old"))
	assert.Equal(t, "", s.RemoteRev("refs/heads/old"))
}

func TestRefs_EvilMerge(t *testing.T) {
	s := featureStand(t, guardAll, map[string]string{"a.txt": "a"})
	s.git(s.Work, "checkout", "-q", "-b", "side")
	s.Commit("side", map[string]string{"b.txt": "b"})
	s.Seed("HEAD:refs/heads/side")
	s.git(s.Work, "checkout", "-q", "-B", "feature", "main")
	s.git(s.Work, "merge", "-q", "--no-ff", "--no-commit", "side")
	require.NoError(t, os.WriteFile(filepath.Join(s.Work, "ci.yml"), []byte("evil"), 0o644))
	s.git(s.Work, "add", "-A")
	s.git(s.Work, "commit", "-q", "-m", "innocent merge")
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "ci.yml")
}

func TestRefs_SHA256Repository(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll}, WithObjectFormat("sha256"))
	require.Len(t, s.Rev("HEAD"), 64)
	s.Protected(nil, nil)
	s.Commit("ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "ci.yml")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
	assertAllowed(t, s.Push(bob, ":refs/heads/feature"))
}

func TestRefs_EmptyRepository(t *testing.T) {
	s := NewStand(t)
	s.Commit("first", map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "x"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
	s.Commit("second", map[string]string{"ci.yml": "y"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "ci.yml")
}

func TestRefs_ProtectedClassLazy(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Protected([]string{"release/*"}, nil)
	s.Commit("app", map[string]string{"app.go": "x"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
	assert.Zero(t, s.ProtectedCalls(), "no protected path touched: no API call")
	s.Commit("ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/release/1", "HEAD:refs/heads/feature"), "refs/heads/release/1", "refs/heads/feature")
	assert.Equal(t, 1, s.API.Calls("GET", "/api/v4/projects/1/protected_branches?per_page=100&page=1"), "one request per receive")
}

func TestRefs_ProtectedClassAPIFailure(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Commit("ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "could not check permissions")
	assert.NotContains(t, s.Push(alice, "HEAD:refs/heads/feature").Output, "HTTP 404", "API details stay in audit")
}

func TestRefs_TagToNonCommit(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	tree := s.Rev("HEAD^{tree}")
	s.git(s.Work, "tag", "-a", "-m", "tree", "vt", tree)
	assertDenied(t, s.Push(bob, "refs/tags/vt"), "tag vt: the tag does not point to a commit")
	assert.Equal(t, module.CodeTagObjectInvalid, s.Audit()[0]["code"])
}

// TestTags_OnlyOnBranches: a created or moved tag points to a commit that a
// branch holds after the push, in every project of a group, with or without
// policy files.
func TestTags_OnlyOnBranches(t *testing.T) {
	t.Run("tag on a commit of main", func(t *testing.T) {
		s, base := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
		assertAllowed(t, s.Push(bob, base+":refs/tags/v1"))
	})
	t.Run("tag on a local commit", func(t *testing.T) {
		s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
		c := s.Commit("feat: local", map[string]string{"app.go": "x"})
		assertDenied(t, s.Push(bob, c+":refs/tags/v1"), "tag v1: the tag points to a commit that is in no branch",
			"git push origin <branch> v1")
		ev := s.Audit()
		require.Len(t, ev, 1)
		assert.Equal(t, module.CodeTagNotOnBranch, ev[0]["code"])
	})
	t.Run("branch and tag in one push", func(t *testing.T) {
		s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
		c := s.Commit("feat: app", map[string]string{"app.go": "x"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature", c+":refs/tags/v1"))
	})
	t.Run("branch deleted and tag on its commit in one push", func(t *testing.T) {
		s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
		c := s.Commit("feat: app", map[string]string{"app.go": "x"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
		assertDenied(t, s.Push(bob, ":refs/heads/feature", c+":refs/tags/v1"), "is in no branch")
	})
	t.Run("tag on a new commit that changes ci.yml", func(t *testing.T) {
		s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "v1"})
		c := s.Commit("ci: evil", map[string]string{"ci.yml": "evil"})
		assertDenied(t, s.Push(bob, c+":refs/tags/v1"), "tag v1: the tag points to a commit that is in no branch")
	})
	t.Run("group project without policy files", func(t *testing.T) {
		s, _ := mainWith(t, map[string]string{"app.go": "a"})
		c := s.Commit("feat: local", map[string]string{"app.go": "x"})
		assertDenied(t, s.Push(bob, c+":refs/tags/v1"), "is in no branch")
	})
	t.Run("personal namespace", func(t *testing.T) {
		s, _ := mainWith(t, map[string]string{"app.go": "a"})
		s.Project = "alice/scratch"
		s.API.Handle("GET", "/api/v4/users?username=alice", gitlabtest.OK([]any{map[string]any{"id": 2, "username": "alice"}}))
		c := s.Commit("feat: local", map[string]string{"app.go": "x"})
		assertAllowed(t, s.Push(alice, c+":refs/tags/v1"))
	})
}

func TestRefs_OtherNamespacesIgnored(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Commit("ci", map[string]string{"ci.yml": "x"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/notes/x", "HEAD:refs/keep-around/y"))
	assert.Empty(t, s.Audit())
	assert.True(t, strings.HasPrefix(s.RemoteRev("refs/notes/x"), s.Rev("HEAD")[:7]))
}
