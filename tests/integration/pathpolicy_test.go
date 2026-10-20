//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
)

const (
	pathPolicy = `version: 1
admins: [alice]
rules:
  ci:
    match: all
    paths: [ci.yml, "deploy/**"]
    allow: [alice]
    message: CI and deploy belong to alice.
  commit-policy:
    match: all
    paths: [COMMITPOLICY]
    allow: [dev-lead]
    message: COMMITPOLICY belongs to dev-lead.
`
	pathRelease = `version: 1
admins: [alice]
rules:
  release-ci:
    match: ['/^release\//']
    paths: [ci.yml]
    allow: [alice]
    message: CI of release branches belongs to alice.
`
	pathBroad = `version: 1
admins: [alice]
rules:
  everything:
    match: all
    paths: ["**"]
    allow: [alice, dev-lead]
    message: m
  commit-policy:
    match: all
    paths: [COMMITPOLICY]
    allow: [dev-lead]
    message: COMMITPOLICY belongs to dev-lead.
`
)

func pathStand(t *testing.T, policy string, files map[string]string) (*Stand, string) {
	t.Helper()
	all := map[string]string{"PATHPOLICY": policy}
	for k, v := range files {
		all[k] = v
	}
	s, base := mainWith(t, all)
	s.Team()
	return s, base
}

func TestPathPolicy_PolicyAndProtectedFileSamePush(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, map[string]string{"ci.yml": "v1"})
	s.Commit("feat: bob adds himself and changes ci", map[string]string{
		"PATHPOLICY": strings.Replace(pathPolicy, "allow: [alice]", "allow: [alice, bob]", 1), "ci.yml": "v2"})
	r := s.Push(bob, "HEAD:refs/heads/feature")
	assertDenied(t, r, "PATHPOLICY rule \"ci\", branch feature: you may not change these files", "PATHPOLICY, branch feature: only PATHPOLICY admins may change PATHPOLICY")
}

func TestPathPolicy_InvalidNewVersionOnFeatureBranch(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.Commit("feat: broken policy", map[string]string{"PATHPOLICY": strings.Replace(pathPolicy, "allow: [dev-lead]", "allow: []", 1)})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "PATHPOLICY, branch feature: the changed PATHPOLICY is invalid", "allow: 1 to 50 usernames")
}

func TestPathPolicy_WeakPolicyOnBranchIgnored(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.git(s.Work, "checkout", "-q", "-b", "weak")
	s.Commit("feat: weaken", map[string]string{"PATHPOLICY": "version: 1\nadmins: [alice]\n"})
	s.Seed("HEAD:refs/heads/weak")
	s.Commit("feat: ci on the weak branch", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/weak"), "PATHPOLICY rule \"ci\", branch weak")
}

func TestPathPolicy_InvalidAnchorBlocksAllButRecovery(t *testing.T) {
	s, base := pathStand(t, "version: 1\nadmins: []\n", nil)
	s.Commit("feat: anything", map[string]string{"src.txt": "x"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "PATHPOLICY, branch feature: PATHPOLICY in main is broken")
	assert.Len(t, Events(s.Audit(), "invalid_policy"), 1)
	s.Seed(base + ":refs/heads/old")
	assertAllowed(t, s.Push(bob, ":refs/heads/old"))

	s.git(s.Work, "reset", "-q", "--hard", base)
	s.Commit("fix: repair the policy", map[string]string{"PATHPOLICY": pathPolicy})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/main"), "is broken")
	r := s.Push(owner, "HEAD:refs/heads/main")
	assertAllowed(t, r)
	assert.Contains(t, r.Output, "PATHPOLICY is changed by Owner owner because")
	assert.Len(t, Events(s.Audit(), "policy_recovery"), 1)
}

func TestDelegation_OwnerModifiesCommitPolicy(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("chore: adjust commit rules", map[string]string{"COMMITPOLICY": strings.Replace(commitPolicy, "chore]", "chore, docs]", 1)})
	assertAllowed(t, s.Push(devLead, "HEAD:refs/heads/main"))
}

func TestDelegation_OwnerDeletesCommitPolicy(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("chore: drop commit rules", map[string]string{"COMMITPOLICY": ""})
	assertAllowed(t, s.Push(devLead, "HEAD:refs/heads/main"))
}

func TestDelegation_NonOwnerDenied(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("chore: adjust commit rules", map[string]string{"COMMITPOLICY": strings.Replace(commitPolicy, "chore]", "chore, docs]", 1)})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "PATHPOLICY rule \"commit-policy\", branch feature: you may not change these files")
	s.git(s.Work, "reset", "-q", "--hard", "HEAD~1")
	s.Commit("chore: drop commit rules", map[string]string{"COMMITPOLICY": ""})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "PATHPOLICY rule \"commit-policy\"")
}

func TestDelegation_DelegateCannotChangePathPolicy(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.Commit("chore: delegate more", map[string]string{"PATHPOLICY": pathPolicy + "  tags:\n    match: all\n    paths: [TAGPOLICY]\n    allow: [dev-lead]\n    message: m\n"})
	r := s.Push(devLead, "HEAD:refs/heads/feature")
	assertDenied(t, r, "PATHPOLICY, branch feature: only PATHPOLICY admins may change PATHPOLICY", "admins: alice")
	ev := Events(s.Audit(), "decision")
	require.NotEmpty(t, ev)
	assert.Equal(t, "policy_admin_violation", ev[len(ev)-1]["code"])
}

func TestDelegation_AdminDeletesPathPolicyThenBootstrap(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("chore: remove path policy", map[string]string{"PATHPOLICY": ""})
	r := s.Push(alice, "HEAD:refs/heads/main")
	assertAllowed(t, r)
	assert.Contains(t, r.Output, "anyone who can push may change COMMITPOLICY")
	s.Commit("chore: bring the policy back", map[string]string{"PATHPOLICY": pathPolicy})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "added to the default branch by a Maintainer or Owner")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	assert.Len(t, Events(s.Audit(), "policy_bootstrap"), 1)
}

func TestDelegation_BroadRuleAndDelegationAnd(t *testing.T) {
	s, _ := pathStand(t, pathBroad, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("chore: adjust", map[string]string{"COMMITPOLICY": strings.Replace(commitPolicy, "chore]", "chore, docs]", 1)})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "PATHPOLICY rule \"commit-policy\"")
	assertAllowed(t, s.Push(devLead, "HEAD:refs/heads/feature"))
}

func TestDelegation_UncoveredFileWarns(t *testing.T) {
	s, _ := pathStand(t, "version: 1\nadmins: [alice]\n", nil)
	s.Commit("chore: add commit rules", map[string]string{"COMMITPOLICY": commitPolicy})
	r := s.Push(bob, "HEAD:refs/heads/feature")
	assertAllowed(t, r)
	assert.Contains(t, r.Output, "no PATHPOLICY rule covers COMMITPOLICY: anyone who can push may change it")
}

func TestDelegation_RenamePolicyFile(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("chore: move the commit rules", map[string]string{"COMMITPOLICY": "", "docs/COMMITPOLICY": commitPolicy})
	// The removal of the old path is a protected change.
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "PATHPOLICY rule \"commit-policy\"")
	r := s.Push(devLead, "HEAD:refs/heads/feature")
	assertAllowed(t, r)
	assert.NotContains(t, r.Output, "docs/COMMITPOLICY: new policy version", "a file under another path is not a policy")
}

func TestBootstrap_DeveloperDenied(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"a.txt": "a"})
	s.Team()
	s.Commit("chore: add path policy", map[string]string{"PATHPOLICY": pathPolicy})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "PATHPOLICY, branch main: the first PATHPOLICY of a project is added by a Maintainer or Owner")
}

func TestBootstrap_MaintainerAllowed(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"a.txt": "a"})
	s.Team()
	s.Commit("chore: add path policy", map[string]string{"PATHPOLICY": pathPolicy})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	ev := Events(s.Audit(), "policy_bootstrap")
	require.Len(t, ev, 1)
	assert.Equal(t, "pathpolicy", ev[0]["module"])
}

func TestBootstrap_NoEffectiveAdmin(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"a.txt": "a"})
	s.Team()
	s.Commit("chore: add path policy", map[string]string{"PATHPOLICY": strings.Replace(pathPolicy, "admins: [alice]", "admins: [bob]", 1)})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/main"), "the changed PATHPOLICY would have no working admin")
}

func TestPaths_NestedRulesAnd(t *testing.T) {
	policy := "version: 1\nadmins: [alice]\nrules:\n  deploy: {match: all, paths: [\"deploy/**\"], allow: [alice, bob], message: m}\n  prod: {match: all, paths: [\"deploy/prod/**\"], allow: [alice], message: m}\n"
	s, _ := pathStand(t, policy, nil)
	s.Commit("feat: stage", map[string]string{"deploy/stage/x": "1"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
	s.Commit("feat: prod", map[string]string{"deploy/prod/x": "1"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "PATHPOLICY rule \"prod\"")
}

func TestPathPolicy_PathKinds(t *testing.T) {
	cases := map[string]func(s *Stand){
		"rename-into": func(s *Stand) { s.Commit("feat: in", map[string]string{"src/conf.yml": "", "deploy/conf.yml": "x"}) },
		"rename-out":  func(s *Stand) { s.Commit("feat: out", map[string]string{"deploy/keep.yml": "", "src/keep.yml": "k"}) },
		"delete":      func(s *Stand) { s.Commit("feat: del", map[string]string{"deploy/keep.yml": ""}) },
		"gitlink": func(s *Stand) {
			s.git(s.Work, "update-index", "--add", "--cacheinfo", "160000,"+s.Rev("HEAD")+",deploy/sub")
			s.git(s.Work, "commit", "-q", "-m", "feat: submodule")
		},
		"chmod": func(s *Stand) {
			require.NoError(t, os.Chmod(filepath.Join(s.Work, "deploy/keep.yml"), 0o755))
			s.Commit("feat: chmod", nil)
		},
		"symlink": func(s *Stand) {
			require.NoError(t, os.Symlink("../src/conf.yml", filepath.Join(s.Work, "deploy/link.yml")))
			s.Commit("feat: link", nil)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := pathStand(t, pathPolicy, map[string]string{"src/conf.yml": "x", "deploy/keep.yml": "k"})
			change(s)
			assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "PATHPOLICY rule \"ci\"")
			assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
		})
	}
}

func TestPathPolicy_MergeIntoReleaseByNonOwner(t *testing.T) {
	s, base := pathStand(t, pathRelease, map[string]string{"ci.yml": "v1"})
	s.Seed("HEAD:refs/heads/release/1")
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("feat: alice changes ci", map[string]string{"ci.yml": "v2"})
	s.Seed("HEAD:refs/heads/feature")
	s.git(s.Work, "checkout", "-q", "-B", "rel", base)
	s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature' into 'release/1'", "feature")
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/release/1"), "PATHPOLICY rule \"release-ci\", branch release/1", "ci.yml")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/release/1"))
}

func TestIdentity_ProjectNotVisible(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.API.Handle("GET", "/api/v4/projects/1", gitlabtest.Status(404))
	s.Commit("feat: ci", map[string]string{"ci.yml": "x"})
	r := s.Push(alice, "HEAD:refs/heads/feature")
	assertDenied(t, r, "this project is not connected to gitlab-repopolicy correctly")
	assert.NotContains(t, r.Output, "project_not_visible", "API details stay in audit")
	ev := Events(s.Audit(), "authorization_backend_failure")
	require.NotEmpty(t, ev)
	assert.Equal(t, "project_not_visible", ev[0]["reason"])
}

// TestIdentity_TokenRejected: a token that GitLab no longer accepts denies the
// push and names the audit event, which the alert on an expired token watches.
func TestIdentity_TokenRejected(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.API.Handle("GET", "/api/v4/projects/1/protected_branches?per_page=100&page=1", gitlabtest.Status(401))
	s.Commit("feat: ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), "this project is not connected to gitlab-repopolicy correctly")
	ev := Events(s.Audit(), "authorization_backend_failure")
	require.Len(t, ev, 1)
	assert.Equal(t, "pathpolicy", ev[0]["module"])
	assert.Equal(t, "authorization_backend_failure", ev[0]["code"])
	assert.NotEmpty(t, ev[0]["receive_id"])
}

func TestIdentity_RoleBelowMaintainer(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.API.Handle("GET", "/api/v4/projects/1", gitlabtest.OK(map[string]any{"permissions": map[string]any{"group_access": map[string]any{"access_level": 30}}}))
	s.Commit("feat: ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "this project is not connected to gitlab-repopolicy correctly")
	ev := Events(s.Audit(), "authorization_backend_failure")
	require.NotEmpty(t, ev)
	assert.Equal(t, "role_below_maintainer", ev[0]["reason"])
}

func TestNamespace_PersonalAllowed(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.Project = "alice/scratch"
	s.API.Handle("GET", "/api/v4/users?username=alice", gitlabtest.OK([]any{map[string]any{"id": 2, "username": "alice"}}))
	s.Commit("feat: ci", map[string]string{"ci.yml": "x"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
	assert.Empty(t, s.Audit())
	assert.Zero(t, s.API.Calls("GET", "/api/v4/projects/1"), "no further API call for a personal project")
}

func TestNamespace_GroupTwoSegments(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.Project = "acme/app"
	s.API.Handle("GET", "/api/v4/users?username=acme", gitlabtest.OK([]any{}))
	s.Commit("feat: ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "PATHPOLICY rule \"ci\"")
	assert.Equal(t, 1, s.API.Calls("GET", "/api/v4/users?username=acme"))
}

func TestNamespace_GroupThreeSegments(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.Commit("feat: ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "PATHPOLICY rule \"ci\"")
	assert.Zero(t, s.API.Calls("GET", "/api/v4/users?username=acme"), "three segments are always a group")
}

func TestNamespace_LookupFailure(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.Project = "acme/app"
	s.API.Handle("GET", "/api/v4/users?username=acme", gitlabtest.Status(500))
	s.Commit("feat: ci", map[string]string{"ci.yml": "x"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "PATHPOLICY rule \"ci\"")
}

func TestLimits_NewCommits(t *testing.T) {
	s, base := pathStand(t, pathPolicy, nil)
	s.fastImport(base, "refs/heads/many", 50_001)
	assertDenied(t, s.Push(alice, "refs/heads/many:refs/heads/many"), "the check did not finish within the server limits")
	ev := Events(s.Audit(), "decision")
	require.NotEmpty(t, ev)
	assert.Equal(t, "limit_exceeded", ev[len(ev)-1]["code"])
}

// changeAndRevert commits a change of ci.yml and its revert: the push has no
// net diff of ci.yml.
func changeAndRevert(s *Stand) {
	s.Commit("ci: evil", map[string]string{"ci.yml": "evil"})
	s.Commit("ci: revert", map[string]string{"ci.yml": "v1"})
}

// TestPathPolicy_ChangedAndReverted: a protected file changed and reverted in
// one push is still a change of the default branch and of a branch of a
// match list, also when the commits are in another branch first.
func TestPathPolicy_ChangedAndReverted(t *testing.T) {
	t.Run("main", func(t *testing.T) {
		s, _ := pathStand(t, pathPolicy, map[string]string{"ci.yml": "v1"})
		changeAndRevert(s)
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), `PATHPOLICY rule "ci", branch main`, "ci.yml")
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	})
	t.Run("branch of a match list", func(t *testing.T) {
		s, base := pathStand(t, pathRelease, map[string]string{"ci.yml": "v1"})
		s.Seed(base + ":refs/heads/release/1")
		changeAndRevert(s)
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/release/1"), `PATHPOLICY rule "release-ci", branch release/1`)
	})
	t.Run("fast-forward from another branch", func(t *testing.T) {
		s, base := pathStand(t, pathRelease, map[string]string{"ci.yml": "v1"})
		s.Seed(base + ":refs/heads/release/1")
		changeAndRevert(s)
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature")) // the list of the rule does not have feature
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/release/1"), `PATHPOLICY rule "release-ci", branch release/1`)
	})
	t.Run("merge into main", func(t *testing.T) {
		s, base := pathStand(t, pathPolicy, map[string]string{"ci.yml": "v1"})
		s.git(s.Work, "checkout", "-q", "-b", "feature")
		changeAndRevert(s)
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
		s.git(s.Work, "checkout", "-q", "-B", "main", base)
		s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature' into 'main'", "feature")
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), `PATHPOLICY rule "ci", branch main`)
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	})
	t.Run("rollback by a force push", func(t *testing.T) {
		s, base := pathStand(t, pathPolicy, map[string]string{"ci.yml": "v1"})
		s.Commit("ci: v2", map[string]string{"ci.yml": "v2"})
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
		assertDenied(t, s.Push(bob, "+"+base+":refs/heads/main"), `PATHPOLICY rule "ci", branch main`)
	})
}

// TestLimits_DefaultBranch: more than 50 000 commits entering the default
// branch fail closed, because every one of them is checked.
func TestLimits_DefaultBranch(t *testing.T) {
	s, base := pathStand(t, pathPolicy, nil)
	s.fastImport(base, "refs/heads/many", 50_001)
	assertDenied(t, s.Push(alice, "refs/heads/many:refs/heads/main"), "the check did not finish within the server limits")
}

// aliceBranch: alice pushes ci.yml v2 to feature, then main moves on without
// ci.yml. The work clone is on feature.
func aliceBranch(t *testing.T) (*Stand, string) {
	t.Helper()
	s, base := pathStand(t, pathPolicy, map[string]string{"ci.yml": "v1", "app.go": "a"})
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("ci: v2", map[string]string{"ci.yml": "v2"})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
	s.git(s.Work, "checkout", "-q", "main")
	s.Commit("feat: app", map[string]string{"app.go": "b"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
	s.git(s.Work, "checkout", "-q", "feature")
	return s, base
}

// TestPathPolicy_SharedBranch: in an ordinary branch a new commit changes a
// protected file only when it brings content that neither the branch nor the
// default branch had. Rewritten commits of an allowed person pass.
func TestPathPolicy_SharedBranch(t *testing.T) {
	t.Run("bob rebases a commit of alice onto a fresh main", func(t *testing.T) {
		s, _ := aliceBranch(t)
		s.git(s.Work, "rebase", "-q", "main")
		assertAllowed(t, s.Push(bob, "+HEAD:refs/heads/feature"))
	})
	t.Run("bob rebases two edits of alice", func(t *testing.T) {
		s, _ := aliceBranch(t)
		s.Commit("ci: v3", map[string]string{"ci.yml": "v3"})
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
		s.git(s.Work, "rebase", "-q", "main")
		assertAllowed(t, s.Push(bob, "+HEAD:refs/heads/feature"))
	})
	t.Run("bob squashes the branch", func(t *testing.T) {
		s, base := aliceBranch(t)
		s.Commit("feat: bob", map[string]string{"src.go": "x"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
		s.git(s.Work, "reset", "-q", "--soft", base)
		s.git(s.Work, "commit", "-q", "-m", "feat: all of it")
		assertAllowed(t, s.Push(bob, "+HEAD:refs/heads/feature"))
	})
	t.Run("bob rewords a commit of alice", func(t *testing.T) {
		s, _ := aliceBranch(t)
		s.git(s.Work, "commit", "-q", "--amend", "-m", "ci: version 2")
		assertAllowed(t, s.Push(bob, "+HEAD:refs/heads/feature"))
	})
	t.Run("bob cherry-picks a CI commit of alice from main", func(t *testing.T) {
		s, base := pathStand(t, pathPolicy, map[string]string{"ci.yml": "v1"})
		s.Commit("ci: v2", map[string]string{"ci.yml": "v2"})
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
		s.git(s.Work, "checkout", "-q", "-b", "feature", base)
		s.Commit("feat: bob", map[string]string{"src.go": "x"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
		s.git(s.Work, "cherry-pick", "main")
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
	})
	t.Run("bob merges main into feature or rebases feature on main", func(t *testing.T) {
		s, base := pathStand(t, pathPolicy, map[string]string{"ci.yml": "v1"})
		s.Commit("ci: v2", map[string]string{"ci.yml": "v2"})
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
		s.git(s.Work, "checkout", "-q", "-b", "feature", base)
		feature := s.Commit("feat: bob", map[string]string{"src.go": "x"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
		s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'main' into feature", "main")
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
		s.git(s.Work, "reset", "-q", "--hard", feature)
		s.git(s.Work, "rebase", "-q", "main")
		assertAllowed(t, s.Push(bob, "+HEAD:refs/heads/feature"))
	})
	t.Run("bob pushes his own content", func(t *testing.T) {
		s, _ := aliceBranch(t)
		own := s.Commit("ci: bob", map[string]string{"ci.yml": "bob"})
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), `PATHPOLICY rule "ci", branch feature`,
			"the content of ci.yml in commit "+own[:9]+" differs both from this branch and from main")
	})
	t.Run("conflicting rebase gives merged content", func(t *testing.T) {
		s, _ := aliceBranch(t)
		s.git(s.Work, "checkout", "-q", "main")
		s.Commit("ci: v3", map[string]string{"ci.yml": "v3"})
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
		// The result of resolving the conflict of the rebase of feature.
		s.git(s.Work, "checkout", "-q", "-B", "feature", "main")
		s.Commit("ci: v2", map[string]string{"ci.yml": "v2\nv3"})
		assertDenied(t, s.Push(bob, "+HEAD:refs/heads/feature"), `PATHPOLICY rule "ci", branch feature`)
	})
	t.Run("bob pushes the rewritten history to a new branch", func(t *testing.T) {
		s, _ := aliceBranch(t)
		s.git(s.Work, "rebase", "-q", "main")
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature-2"), `PATHPOLICY rule "ci", branch feature-2`)
	})
	t.Run("bob changes the mode of the same content", func(t *testing.T) {
		s, _ := aliceBranch(t)
		s.git(s.Work, "update-index", "--chmod=+x", "ci.yml")
		s.git(s.Work, "commit", "-q", "-m", "ci: executable")
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), `PATHPOLICY rule "ci", branch feature`)
	})
	t.Run("bob brings back v2 of alice from the branch history", func(t *testing.T) {
		s, _ := aliceBranch(t)
		s.Commit("ci: v3", map[string]string{"ci.yml": "v3"})
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
		s.Commit("ci: back to v2", map[string]string{"ci.yml": "v2"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
	})
	t.Run("bob rebases a change of PATHPOLICY", func(t *testing.T) {
		s, _ := pathStand(t, pathPolicy, map[string]string{"app.go": "a"})
		s.git(s.Work, "checkout", "-q", "-b", "feature")
		s.Commit("chore: policy", map[string]string{"PATHPOLICY": pathPolicy + "# reviewed\n"})
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
		s.git(s.Work, "checkout", "-q", "main")
		s.Commit("feat: app", map[string]string{"app.go": "b"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
		s.git(s.Work, "checkout", "-q", "feature")
		s.git(s.Work, "rebase", "-q", "main")
		assertDenied(t, s.Push(bob, "+HEAD:refs/heads/feature"), "PATHPOLICY")
	})
	for _, squash := range []bool{false, true} {
		name := "merge"
		if squash {
			name = "squash-merge"
		}
		t.Run(name+" into main", func(t *testing.T) {
			s, _ := aliceBranch(t)
			s.git(s.Work, "checkout", "-q", "main")
			if squash {
				s.git(s.Work, "merge", "-q", "--squash", "feature")
				s.git(s.Work, "commit", "-q", "-m", "ci: v2")
			} else {
				s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature' into 'main'", "feature")
			}
			assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), `PATHPOLICY rule "ci", branch main`)
			assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
		})
	}
	t.Run("more commits than the limit", func(t *testing.T) {
		s, _ := aliceBranch(t)
		s.fastImport(s.Rev("HEAD"), "refs/heads/many", 50_001)
		assertDenied(t, s.Push(bob, "refs/heads/many:refs/heads/feature"), "the check did not finish within the server limits")
	})
}
