//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The e2e part of PATHPOLICY: principals, admins and Owner recovery against
// the real members API.

const adminDenied = "gitlab-repopolicy: PATHPOLICY policy change denied. See push output."

// policyWithAdmins is ciPolicy with other admins.
func policyWithAdmins(admins ...string) string {
	return strings.Replace(ciPolicy, "admins: [alice]", "admins: ["+strings.Join(admins, ", ")+"]", 1)
}

// changePolicy commits a new PATHPOLICY to main as u.
func changePolicy(t *testing.T, u *standUser, p *project, policy string) error {
	t.Helper()
	_, err := commit(t, u, p, "main", "", "chore: change path policy", update("PATHPOLICY", policy))
	return err
}

// TestAcceptance_UnknownUsername: a username that does not
// exist makes the new policy version invalid.
func TestAcceptance_UnknownUsername(t *testing.T) {
	t.Parallel()
	p := newProject(t, "unknown-user")
	ghost := uniq("ghost")
	_, err := commit(t, user("alice"), p, "main", "", "chore: add path policy",
		create("PATHPOLICY", strings.Replace(ciPolicy, "allow: [alice]", "allow: [alice, "+ghost+"]", 1)))
	requireHookDenied(t, err, "gitlab-repopolicy: PATHPOLICY new policy version is invalid. See push output.")
	e := requireEvent(t, p, "deny of the policy", decisionOn("refs/heads/main"))
	assert.Equal(t, "invalid_policy_update", e.Code)
	require.Len(t, e.Violations, 1)
	assert.Contains(t, e.Violations[0].Reason, ghost)
}

// TestAdmins_DeveloperIsNotAdmin: bob is listed in admins but
// has the Developer role.
func TestAdmins_DeveloperIsNotAdmin(t *testing.T) {
	t.Parallel()
	p := newProject(t, "developer-admin")
	alice, bob := user("alice"), user("bob")
	mustCommit(t, alice, p, "main", "", "chore: add path policy", create("PATHPOLICY", policyWithAdmins("alice", "bob")))
	_, err := commit(t, bob, p, "bob-policy", "main", "chore: change path policy", update("PATHPOLICY", policyWithAdmins("alice")))
	requireHookDenied(t, err, adminDenied)
	e := requireEvent(t, p, "deny of bob", decisionOn("refs/heads/bob-policy"))
	assert.Equal(t, "policy_admin_violation", e.Code)
	assert.Equal(t, "bob", e.Username)
	mustCommit(t, alice, p, "alice-policy", "main", "chore: change path policy", update("PATHPOLICY", policyWithAdmins("alice", "olga")))
}

// lostAdmin creates a project whose only admin is a new Maintainer, then lets
// lose apply to that admin: remove the membership or block the user.
func lostAdmin(t *testing.T, name string, lose func(p *project, admin *standUser)) *project {
	t.Helper()
	p := newProject(t, name)
	admin := newUser(t, "admin")
	addMember(t, p, admin, accessMaintainer)
	mustCommit(t, user("alice"), p, "main", "", "chore: add path policy", create("PATHPOLICY", policyWithAdmins(admin.Username)))
	lose(p, admin)
	return p
}

// removeMember removes the admin from the project.
func removeMember(t *testing.T) func(*project, *standUser) {
	return func(p *project, admin *standUser) {
		require.NoError(t, gl.root(t.Context(), http.MethodDelete, p.api(fmt.Sprintf("/members/%d", admin.ID)), nil, nil))
	}
}

// requireRecovery checks an Owner recovery by u.
func requireRecovery(t *testing.T, p *project, u *standUser) {
	t.Helper()
	require.NoError(t, changePolicy(t, u, p, policyWithAdmins("alice")))
	e := requireEvent(t, p, "policy_recovery", func(e auditEvent) bool { return e.Event == "policy_recovery" })
	assert.Equal(t, "allow", e.Decision)
	assert.Equal(t, u.Username, e.Username)
	assert.Equal(t, "refs/heads/main", e.Ref)
}

// owner adds owen as an Owner of the project.
func owner(t *testing.T, p *project) *standUser {
	t.Helper()
	owen := user("owen")
	addMember(t, p, owen, accessOwner)
	return owen
}
func TestRecovery_OwnerDeniedWhenAdminsAlive(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "admins-alive")
	err := changePolicy(t, owner(t, p), p, policyWithAdmins("alice", "olga"))
	requireHookDenied(t, err, adminDenied)
	e := requireEvent(t, p, "deny of the Owner", func(e auditEvent) bool { return e.Event == "decision" && e.Username == "owen" })
	assert.Equal(t, "policy_admin_violation", e.Code)
}

// TestRecovery_LastAdminRemoved: the only admin left the project.
func TestRecovery_LastAdminRemoved(t *testing.T) {
	t.Parallel()
	p := lostAdmin(t, "admin-removed", removeMember(t))
	requireRecovery(t, p, owner(t, p))
}
func TestRecovery_LastAdminBlocked(t *testing.T) {
	t.Parallel()
	p := lostAdmin(t, "admin-blocked", func(_ *project, admin *standUser) {
		require.NoError(t, gl.root(t.Context(), http.MethodPost, fmt.Sprintf("/users/%d/block", admin.ID), nil, nil))
	})
	requireRecovery(t, p, owner(t, p))
}

// TestRecovery_OwnerViaParentGroup: olga is an Owner of the
// top-level group and not a member of the project or its subgroup.
func TestRecovery_OwnerViaParentGroup(t *testing.T) {
	t.Parallel()
	p := lostAdmin(t, "parent-owner", removeMember(t))
	requireRecovery(t, p, user("olga"))
}

// TestRecovery_NonOwnerDenied: in recovery a Maintainer is
// refused, and the push output names the recovery condition.
func TestRecovery_NonOwnerDenied(t *testing.T) {
	t.Parallel()
	p := lostAdmin(t, "non-owner", removeMember(t))
	alice := user("alice")
	g := clone(t, alice.Token, p.Path)
	g.commitFile("PATHPOLICY", policyWithAdmins("alice"), "chore: change path policy")
	out, err := g.push(httpsURL(alice.Token, p.Path), "", "HEAD:refs/heads/main")
	requirePushDenied(t, out, err, adminDenied)
	assert.Contains(t, out, "Without an effective admin, an Owner of the project may change it (recovery).")
	e := requireEvent(t, p, "deny of alice", func(e auditEvent) bool { return e.Event == "decision" && e.Decision == "deny" })
	assert.Equal(t, "policy_admin_violation", e.Code)
	assert.Equal(t, "http", e.Protocol)
}
