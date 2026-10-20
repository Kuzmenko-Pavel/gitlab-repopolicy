//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const branchPolicy = `version: 1
extends: team
rules:
  no-master:
    match: [master]
    priority: 10
    deny: true
    message: This project uses main; master must not be created.
`

const branchPresets = `presets:
  branchpolicy:
    team:
      rules:
        release:
          match: ['/^release\/v[0-9]+\.[0-9]+\.[0-9]+$/']
          priority: 100
          protected: true
          message: Release branches look like release/v1.4.0. Protect release/* in Protected branches.
        release-bad:
          match: ['/^release\//']
          priority: 110
          deny: true
          message: Release branches look like release/v1.4.0.
        work:
          match: ['/^[a-z0-9.]+\/[a-z0-9._\/-]+$/']
          priority: 900
          message: Work branches are <prefix>/<name>.
        other:
          match: all
          priority: 1000
          deny: true
          message: Use a name of the branch standard, for example kpn/disable_auth.
`

func branchStand(t *testing.T) (*Stand, string) {
	t.Helper()
	s, base := mainWith(t, map[string]string{"BRANCHPOLICY": branchPolicy})
	s.AddConfig(branchPresets)
	return s, base
}

func TestBranchPolicy_Names(t *testing.T) {
	s, _ := branchStand(t)
	s.Protected([]string{"main", "release/*"}, nil)
	s.Commit("feat: x", map[string]string{"a": "1"})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/kpn/disable_auth"))
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/Fix_Login"), "creating this branch is forbidden",
		"Use a name of the branch standard",
		"GL-HOOK-ERR: gitlab-repopolicy: branch Fix_Login is forbidden by BRANCHPOLICY rule other")
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/release/foo"), "BRANCHPOLICY rule \"release-bad\", branch release/foo",
		"Release branches look like release/v1.4.0")
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/master"), "creating this branch is forbidden",
		"GL-HOOK-ERR: gitlab-repopolicy: branch master is forbidden by BRANCHPOLICY rule no-master")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/release/v1.4.0"))
	ev := Events(s.Audit(), "decision")
	require.NotEmpty(t, ev)
	assert.Equal(t, "branch_name_violation", ev[0]["code"])
}

func TestBranchPolicy_Protected(t *testing.T) {
	s, _ := branchStand(t)
	s.Protected([]string{"main"}, nil)
	s.Commit("feat: x", map[string]string{"a": "1"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/release/v1.4.0"), "the branch must be protected, but the project does not protect it",
		"Release branches look like release/v1.4.0. Protect release/* in Protected branches.",
		"GL-HOOK-ERR: gitlab-repopolicy: branch release/v1.4.0 must be protected: a Maintainer protects it in Settings > Repository > Protected branches")
	s.Protected([]string{"main", "release/*"}, nil)
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/release/v1.4.0"))
}

// TestBranchPolicy_ExistingBranches: a branch that exists is not checked,
// whatever its name: only new branches follow the standard.
func TestBranchPolicy_ExistingBranches(t *testing.T) {
	s, _ := branchStand(t)
	s.Seed("main:refs/heads/Legacy_Branch")
	s.Commit("feat: x", map[string]string{"a": "1"})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/Legacy_Branch"))
	assertAllowed(t, s.Push(alice, ":refs/heads/Legacy_Branch"))
}

func TestBranchPolicy_InvalidAnchorSkipped(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"BRANCHPOLICY": "version: 1\nunknown: 1\n"})
	s.Commit("feat: x", map[string]string{"a": "1"})
	r := s.Push(alice, "HEAD:refs/heads/Any_Name")
	assertAllowed(t, r)
	assert.Contains(t, r.Output, "BRANCHPOLICY in main cannot be used, so names of new branches are not checked until it is fixed")
	assert.Len(t, Events(s.Audit(), "invalid_branch_policy"), 1)
	assert.Empty(t, Events(s.Audit(), "invalid_policy"))
}

// TestBranchPolicy_AuditSourceAndPresets: a decision names the layers of the
// rule and the presets of the file with their digests.
func TestBranchPolicy_AuditSourceAndPresets(t *testing.T) {
	s, _ := branchStand(t)
	s.Commit("feat: x", map[string]string{"a": "1"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/Fix_Login"), "BRANCHPOLICY rule \"other\"")
	ev := Events(s.Audit(), "decision")
	require.Len(t, ev, 1)
	vs := ev[0]["violations"].([]any)
	require.Len(t, vs, 1)
	assert.Equal(t, []any{"preset:team"}, vs[0].(map[string]any)["source"])
	presets := ev[0]["presets"].([]any)
	require.Len(t, presets, 1)
	p := presets[0].(map[string]any)
	assert.Equal(t, "team", p["name"])
	assert.Regexp(t, `^[0-9a-f]{64}$`, p["sha256"])

	assertDenied(t, s.Push(alice, "HEAD:refs/heads/master"), "BRANCHPOLICY rule \"no-master\"")
	ev = Events(s.Audit(), "decision")
	require.Len(t, ev, 1)
	assert.Equal(t, []any{"file"}, ev[0]["violations"].([]any)[0].(map[string]any)["source"])
	assert.Equal(t, p["sha256"], ev[0]["presets"].([]any)[0].(map[string]any)["sha256"], "the digest does not change between pushes")
}
