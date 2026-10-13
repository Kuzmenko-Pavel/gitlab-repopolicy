//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	guardAll      = "rules:\n  - name: ci\n    paths: [ci.yml, deploy/]\n    allow: [alice]\n"
	guardDefault  = "rules:\n  - name: ci\n    paths: [ci.yml, deploy/]\n    allow: [alice]\n    refs: default\n"
	guardRelease  = "rules:\n  - name: release\n    paths: [ci.yml]\n    allow: [alice]\n    refs: [refs/heads/release/*]\n"
	guardNobody   = "rules:\n  - name: ci\n    paths: [ci.yml]\n    allow: [nobody]\n"
	guardWeak     = "rules:\n  - name: other\n    paths: [other.txt]\n    allow: [bob]\n"
	noteAll       = "rules:\n  - name: docs\n    paths: [docs/]\n    allow: [alice]\n"
	zero          = "0000000000000000000000000000000000000000"
	deniedPath    = "PROTECTED"
	rejectedLine  = "! "
	pushRejection = "GL-HOOK-ERR: gitlab-repopolicy:"
)

// mainWith seeds main with the files.
func mainWith(t *testing.T, files map[string]string, opts ...Option) (*Stand, string) {
	t.Helper()
	s := NewStand(t, opts...)
	c := s.Commit("base", files)
	s.Seed("HEAD:refs/heads/main")
	return s, c
}

func assertDenied(t *testing.T, r Result, contains ...string) {
	t.Helper()
	assert.False(t, r.OK, "push must be rejected:\n%s", r.Output)
	assert.Contains(t, r.Output, pushRejection)
	for _, c := range contains {
		assert.Contains(t, r.Output, c)
	}
}

func assertAllowed(t *testing.T, r Result) {
	t.Helper()
	assert.True(t, r.OK, "push must be accepted:\n%s", r.Output)
}

func TestFastPath_NoPolicies(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"a.txt": "a"})
	s.Commit("change", map[string]string{"ci.yml": "x"})
	r := s.Push(bob, "HEAD:refs/heads/main")
	assertAllowed(t, r)
	assert.Equal(t, expectedGitVersion(t), r.HookGit, "the hook runs the git of GIT_BIN")
	assert.Empty(t, s.Audit(), "a push that touches no rule is not audited")
	assert.Zero(t, s.API.TotalCalls())
}

func TestTrust_PolicyAndGuardedChangeSamePush_UsesAnchor(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "v1"})
	s.Commit("allow myself and change ci", map[string]string{
		"GUARDPOLICY": strings.Replace(guardAll, "[alice]", "[alice, bob]", 1), "ci.yml": "v2"})
	r := s.Push(bob, "HEAD:refs/heads/main")
	assertDenied(t, r, "GUARDPOLICY [ci] refs/heads/main: modification denied", "ci.yml")
	assert.Equal(t, "", s.RemoteRev("refs/heads/main^{/allow myself}"), "main is unchanged")
}

func TestAcceptance_InvalidNewVersionOnFeatureBranch(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Commit("policy for nobody", map[string]string{"GUARDPOLICY": guardNobody})
	r := s.Push(alice, "HEAD:refs/heads/feature")
	assertDenied(t, r, "GUARDPOLICY refs/heads/feature: new policy version is invalid", "user nobody does not exist")
	ev := Events(s.Audit(), "decision")
	require.Len(t, ev, 1)
	assert.Equal(t, "invalid_policy_update", ev[0]["code"])

	s.Commit("broken yaml", map[string]string{"GUARDPOLICY": "rules: [\n"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature2"), "new policy version is invalid")
}

func TestTrust_WeakPolicyOnBranchIgnored(t *testing.T) {
	s, base := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.git(s.Work, "checkout", "-q", "-b", "weak")
	s.Commit("weaken on a branch", map[string]string{"GUARDPOLICY": guardWeak})
	s.Seed("HEAD:refs/heads/weak")
	s.Protected(nil, nil)
	s.Commit("change ci on the weak branch", map[string]string{"ci.yml": "x"})
	r := s.Push(bob, "HEAD:refs/heads/weak")
	assertDenied(t, r, "GUARDPOLICY [ci] refs/heads/weak")
	_ = base
}

func TestTrust_InvalidAnchorPolicyFailMode(t *testing.T) {
	t.Run("FailClosed", func(t *testing.T) {
		s, _ := mainWith(t, map[string]string{"GUARDPOLICY": "rules: []\n"})
		s.Commit("any change", map[string]string{"a.txt": "a"})
		assertDenied(t, s.Push(alice, "HEAD:refs/heads/main"), "GUARDPOLICY refs/heads/main: policy is invalid")
		assert.Len(t, Events(s.Audit(), "invalid_policy"), 1)
	})
	t.Run("FailOpen", func(t *testing.T) {
		s, _ := mainWith(t, map[string]string{"NOTEPOLICY": "rules: []\n"})
		s.Commit("any change", map[string]string{"a.txt": "a"})
		r := s.Push(alice, "HEAD:refs/heads/main")
		assertAllowed(t, r)
		assert.Contains(t, r.Output, "NOTEPOLICY on the default branch is invalid")
	})
}

func TestTrust_NoAnchor(t *testing.T) {
	s := NewStand(t)
	s.Commit("first", map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "x"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
	ev := Events(s.Audit(), "policy_change")
	require.Len(t, ev, 1)
	assert.Equal(t, "create", ev[0]["action"])

	empty := NewStand(t)
	empty.Commit("first", map[string]string{"GUARDPOLICY": guardNobody})
	assertDenied(t, empty.Push(alice, "HEAD:refs/heads/main"), "new policy version is invalid")
}

func TestEntry_NonProjectRepositoryAllowedSilently(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Repo, s.Project = "wiki-1", "acme/platform/app.wiki"
	s.Commit("change", map[string]string{"ci.yml": "x"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
	assert.Empty(t, s.Audit())
}

func TestAcceptance_PolicyFileKinds(t *testing.T) {
	cases := map[string]func(s *Stand){
		"DirectoryInPlaceOfPolicy": func(s *Stand) {
			s.Commit("dir", map[string]string{"GUARDPOLICY": "", "GUARDPOLICY/x": guardAll})
		},
		"SymlinkInPlaceOfPolicy": func(s *Stand) {
			require.NoError(t, os.WriteFile(filepath.Join(s.Work, "real"), []byte(guardAll), 0o644))
			require.NoError(t, os.Remove(filepath.Join(s.Work, "GUARDPOLICY")))
			require.NoError(t, os.Symlink("real", filepath.Join(s.Work, "GUARDPOLICY")))
			s.Commit("symlink", nil)
		},
		"GitlinkInPlaceOfPolicy": func(s *Stand) {
			s.git(s.Work, "rm", "-q", "GUARDPOLICY")
			s.git(s.Work, "update-index", "--add", "--cacheinfo", "160000,"+s.Rev("HEAD")+",GUARDPOLICY")
			s.git(s.Work, "commit", "-q", "-m", "gitlink")
		},
		"OversizedPolicy": func(s *Stand) {
			s.Commit("big", map[string]string{"GUARDPOLICY": guardAll + "# " + strings.Repeat("x", 64<<10) + "\n"})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
			change(s)
			assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "new policy version is invalid")
		})
	}
}

func TestMode_Audit(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll}, WithMode("audit"))
	s.Commit("change", map[string]string{"ci.yml": "x"})
	r := s.Push(bob, "HEAD:refs/heads/main")
	assertAllowed(t, r)
	assert.NotContains(t, r.Output, "GL-HOOK-ERR")
	ev := Events(s.Audit(), "decision")
	require.Len(t, ev, 1)
	assert.Equal(t, "deny", ev[0]["decision"])
	assert.Equal(t, false, ev[0]["enforced"])
	assert.NotEmpty(t, ev[0]["git"], "events carry the git version of the hook")
	assert.NotEmpty(t, ev[0]["version"])
}

func TestMode_Off(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll}, WithMode("off"))
	s.Commit("change", map[string]string{"ci.yml": "x"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
	assert.Empty(t, s.Audit())
}

func TestGitUnavailable(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll})
	s.Commit("change", map[string]string{"a.txt": "x"})
	s.HookPath = t.TempDir()
	r := s.Push(alice, "HEAD:refs/heads/main")
	assertDenied(t, r, "git is not available to the hook")
	assert.Equal(t, "", s.RemoteRev("refs/heads/main^{/change}"))
}
