//go:build integration

package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
)

const zeroOID = "0000000000000000000000000000000000000000"

var (
	serviceAccount = map[string]any{"id": 7, "username": "repopolicy", "bot": true, "external": true}
	goodToken      = func(expires time.Time) map[string]any {
		return map[string]any{"id": 1, "scopes": []string{"self_rotate", "read_api"}, "active": true,
			"expires_at": expires.Format(time.DateOnly), "user_id": 7}
	}
)

// TestSelftest_NoToken: without a token file selftest fails.
func TestSelftest_NoToken(t *testing.T) {
	s := NewStand(t)
	require.NoError(t, os.Remove(filepath.Join(s.Dir, "token")))
	r := s.Run(nil, "selftest", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	assert.Regexp(t, `(?m)^FAIL token +.*no such file`, r.Stdout)
	assert.Zero(t, s.API.TotalCalls())
}

// TestSelftest_WrongScopes: extra scopes, a person instead of a service
// account and an administrator are errors; the right account passes.
func TestSelftest_WrongScopes(t *testing.T) {
	s := NewStand(t)
	inYear := time.Now().AddDate(1, 0, 0)
	s.API.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount))
	s.API.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(goodToken(inYear)))
	s.API.Handle("GET", "/api/v4/groups?min_access_level=40&per_page=100", gitlabtest.OK([]any{map[string]any{"id": 3, "full_path": "acme"}}))
	r := s.Run(nil, "selftest", "--config", s.config)
	require.Equal(t, 0, r.Code, r.Stdout)
	assert.Regexp(t, `(?m)^ok +account +repopolicy is a service account`, r.Stdout)
	assert.Regexp(t, `(?m)^ok +groups +Maintainer of 1 groups; gitlab-repopolicy coverage checks every top-level group`, r.Stdout)
	assert.Regexp(t, `(?m)^ok +token +scopes read_api, self_rotate, expires on `, r.Stdout)
	assert.Regexp(t, `(?m)^warn breakglass `, r.Stdout, "no break-glass directory is a warning")
	assert.Regexp(t, `(?m)^info git +git version `, r.Stdout)

	for name, tc := range map[string]struct {
		user, token map[string]any
		want        string
	}{
		"extra scope": {serviceAccount, map[string]any{"scopes": []string{"api", "read_api", "self_rotate"}, "active": true,
			"expires_at": inYear.Format(time.DateOnly)}, `(?m)^FAIL token +scopes api, read_api, self_rotate; exactly read_api, self_rotate`},
		"missing scope": {serviceAccount, map[string]any{"scopes": []string{"read_api"}, "active": true,
			"expires_at": inYear.Format(time.DateOnly)}, `(?m)^FAIL token +scopes read_api;`},
		"not a bot":    {map[string]any{"id": 2, "username": "alice", "external": true}, goodToken(inYear), `(?m)^FAIL account +alice is not a service account`},
		"admin":        {map[string]any{"id": 1, "username": "root", "bot": true, "external": true, "is_admin": true}, goodToken(inYear), `(?m)^FAIL account +root is an administrator`},
		"not external": {map[string]any{"id": 7, "username": "repopolicy", "bot": true}, goodToken(inYear), `(?m)^FAIL account +repopolicy is not external`},
		"no expiry": {serviceAccount, map[string]any{"scopes": []string{"read_api", "self_rotate"}, "active": true},
			`(?m)^FAIL token +no expiry date`},
	} {
		t.Run(name, func(t *testing.T) {
			s.API.Handle("GET", "/api/v4/user", gitlabtest.OK(tc.user))
			s.API.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(tc.token))
			r := s.Run(nil, "selftest", "--config", s.config)
			assert.Equal(t, 1, r.Code)
			assert.Regexp(t, tc.want, r.Stdout)
		})
	}

	s.API.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount))
	s.API.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(goodToken(time.Now().AddDate(0, 0, 10))))
	r = s.Run(nil, "selftest", "--config", s.config)
	assert.Equal(t, 0, r.Code, "an expiry within 30 days is a warning")
	assert.Regexp(t, `(?m)^warn token +expires on .* run gitlab-repopolicy token rotate`, r.Stdout)

	s.API.Handle("GET", "/api/v4/groups?min_access_level=40&per_page=100", gitlabtest.OK([]any{}))
	r = s.Run(nil, "selftest", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	assert.Regexp(t, `(?m)^FAIL groups +repopolicy is a Maintainer of no group, so the hook sees no project`, r.Stdout)

	s.API.Handle("GET", "/api/v4/user", gitlabtest.Status(401))
	r = s.Run(nil, "selftest", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	assert.Regexp(t, `(?m)^FAIL api +GET /user: .*unauthorized`, r.Stdout)
}

// TestTokenRotate: the new token replaces the file atomically with an explicit
// expires_at; a refusal keeps the old token and is an audit alert.
func TestTokenRotate(t *testing.T) {
	s := NewStand(t)
	tokenFile := filepath.Join(s.Dir, "token")
	require.NoError(t, os.Chmod(tokenFile, 0o440))
	chownLog := filepath.Join(s.Dir, "chown.log")
	env := make([]string, 0, 2)
	env = append(env, "REPOPOLICY_TEST_CHOWN_LOG="+chownLog)
	const rotatePath = "/api/v4/personal_access_tokens/self/rotate"

	r := s.Run(append(env, notRoot), "token", "rotate", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "must run as root")

	s.API.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(goodToken(time.Now().AddDate(0, 0, 100))))
	r = s.Run(env, "token", "rotate", "--config", s.config)
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "more than 30d from now: not rotated")
	assert.Zero(t, s.API.Calls("POST", rotatePath))

	s.API.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(goodToken(time.Now().AddDate(0, 0, 10))))
	s.API.Handle("POST", rotatePath, gitlabtest.Response{Status: 400,
		Body: map[string]any{"message": "400 Bad request - Expiration date must be before 2027-01-01"}})
	r = s.Run(env, "token", "rotate", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "the current token stays valid")
	data, err := os.ReadFile(tokenFile)
	require.NoError(t, err)
	assert.Equal(t, "glpat-test\n", string(data))
	ev := Events(s.Audit(), "internal_error")
	require.Len(t, ev, 1)
	assert.Equal(t, "token_rotate", ev[0]["action"])
	assert.Contains(t, ev[0]["reason"], "Expiration date must be before 2027-01-01")

	before := time.Now().UTC()
	s.API.Handle("POST", rotatePath, gitlabtest.OK(map[string]any{"id": 2, "token": "glpat-rotated", "expires_at": "2027-10-13"}))
	r = s.Run(env, "token", "rotate", "--config", s.config, "--before", "30d")
	require.Equal(t, 0, r.Code, r.Stderr)
	after := time.Now().UTC()
	assert.Contains(t, r.Stdout, "the token is rotated")
	assert.Contains(t, r.Stderr, "level=INFO msg=\"token rotated\"")
	data, err = os.ReadFile(tokenFile)
	require.NoError(t, err)
	assert.Equal(t, "glpat-rotated\n", string(data))
	info, err := os.Stat(tokenFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o440), info.Mode().Perm())
	bodies := s.API.Bodies("POST", rotatePath)
	require.Len(t, bodies, 2)
	lifetime := 364 * 24 * time.Hour
	assert.Contains(t, []string{
		fmt.Sprintf(`{"expires_at":"%s"}`, before.Add(lifetime).Format(time.DateOnly)),
		fmt.Sprintf(`{"expires_at":"%s"}`, after.Add(lifetime).Format(time.DateOnly)),
	}, bodies[1], "an explicit expires_at of today + token_lifetime")
	log, err := os.ReadFile(chownLog)
	require.NoError(t, err)
	assert.Contains(t, string(log), filepath.Join(s.Dir, ".token-"))
	assert.Contains(t, string(log), fmt.Sprintf(" %d:%d\n", os.Getuid(), os.Getgid()), "the owner of the old token file")
	entries, err := os.ReadDir(s.Dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".token-"), "no temporary token file is left: %s", e.Name())
	}
	assert.Equal(t, "glpat-test", s.API.Tokens()[len(s.API.Tokens())-1], "the old token asked for the rotation")
}

// settingsProject is project 1 as the projects API returns it to the service account.
func settingsProject(ciConfig any, jobTokenPush bool) []map[string]any {
	return []map[string]any{{"id": 1, "path_with_namespace": "acme/platform/app", "default_branch": "main",
		"ci_config_path": ciConfig, "ci_push_repository_for_job_token_allowed": jobTokenPush,
		"namespace":   map[string]any{"kind": "group"},
		"permissions": map[string]any{"group_access": map[string]any{"access_level": 40}}}}
}

const memberProjects = "/api/v4/projects?membership=true&archived=false&order_by=id&sort=asc&per_page=100&id_after=0"

// TestSettingsCheck: drifts of settings and principals are settings_drift
// events and exit code 1.
func TestSettingsCheck(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	s.Protected([]string{"main", "release/*"}, []string{"v*"})
	s.AddConfig("default_branch: [main]\nsettings_check:\n  protected_tags: [\"v*\"]\n  job_token_push: false\n")
	s.API.Handle("GET", memberProjects, gitlabtest.OK(settingsProject("ci.yml", false)))
	s.API.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.OK(pathPolicy))

	r := s.Run(nil, "settings-check", "--config", s.config)
	require.Equal(t, 0, r.Code, r.Stdout+r.Stderr)
	assert.Contains(t, r.Stdout, "1 projects with PATHPOLICY checked, no drift")
	assert.Contains(t, r.Stderr, "level=INFO msg=\"settings-check done\" checked=1 drifted=0")
	assert.Empty(t, s.Audit())

	// GitLab returns null for a ci_config_path that was never set: the default .gitlab-ci.yml.
	s.API.Handle("GET", memberProjects, gitlabtest.OK(settingsProject(nil, false)))
	r = s.Run(nil, "settings-check", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	assert.Equal(t, "acme/platform/app: ci_config_path: \".gitlab-ci.yml\" is not covered by a path rule\n", r.Stdout)
	require.Len(t, Events(s.Audit(), "settings_drift"), 1)

	s.API.Handle("GET", memberProjects, gitlabtest.OK(settingsProject(".gitlab-ci.yml@other/project", true)))
	s.API.Handle("POST", "/api/graphql", gitlabtest.OK(map[string]any{"data": map[string]any{"users": map[string]any{
		"nodes": []any{map[string]any{"id": "gid://gitlab/User/2", "username": "alice", "state": "active"}}}}}))
	s.Protected([]string{"release/*"}, []string{"v*"})
	r = s.Run(nil, "settings-check", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	for _, line := range []string{
		`acme/platform/app: ci_config_path: ".gitlab-ci.yml@other/project" is outside of the repository`,
		"acme/platform/app: protected_branches: not protected: main",
		"acme/platform/app: job_token_push: job token push is true, expected false",
		"acme/platform/app: principals: not active GitLab users: dev-lead",
	} {
		assert.Contains(t, r.Stdout, line)
	}
	ev := Events(s.Audit(), "settings_drift")
	require.Len(t, ev, 1)
	assert.Equal(t, "acme/platform/app", ev[0]["project_path"])
	assert.Equal(t, float64(1), ev[0]["project_id"])
	violations := ev[0]["violations"].([]any)
	rules := make([]string, 0, len(violations))
	for _, v := range violations {
		rules = append(rules, v.(map[string]any)["rule"].(string))
	}
	assert.ElementsMatch(t, []string{"ci_config_path", "protected_branches", "job_token_push", "principals"}, rules)

	s.API.Handle("GET", memberProjects, gitlabtest.Status(401))
	r = s.Run(nil, "settings-check", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "list projects")
}

// TestAuditNotWritable: the pusher learns that the audit log is broken, and
// settings-check reports a log the hook user cannot append to.
func TestAuditNotWritable(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	require.NoError(t, os.WriteFile(s.audit, nil, 0o444))
	s.Commit("feat: app", map[string]string{"app.go": "x"})
	r := s.Push(alice, "HEAD:refs/heads/feature")
	assertAllowed(t, r)
	assert.NotContains(t, r.Output, "audit log is not writable", "a push that touches no rule writes no event")

	s.Commit("feat: ci", map[string]string{"ci.yml": "x"})
	r = s.Push(alice, "HEAD:refs/heads/feature")
	assertAllowed(t, r)
	assert.Contains(t, r.Output, "the audit log is not writable, tell your GitLab administrators")

	s.API.Handle("GET", memberProjects, gitlabtest.OK([]any{}))
	c := s.Run(nil, "settings-check", "--config", s.config)
	assert.Equal(t, 1, c.Code)
	assert.Contains(t, c.Stdout, "audit: "+s.audit+" is not writable by its owner, mode -r--r--r--: the hook loses its events")
}

// TestCoverage: an administrator token finds top-level groups without the
// service account and the projects with policy there.
func TestCoverage(t *testing.T) {
	s := NewStand(t)
	adminToken := filepath.Join(s.Dir, "admin-token")
	require.NoError(t, os.WriteFile(adminToken, []byte("glpat-admin\n"), 0o600))
	admin := map[string]any{"id": 1, "username": "root", "is_admin": true}
	s.API.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount), gitlabtest.OK(admin))
	s.API.Handle("GET", "/api/v4/groups?top_level_only=true&all_available=true&order_by=id&sort=asc&per_page=100&page=1",
		gitlabtest.OK([]map[string]any{{"id": 1, "full_path": "acme"}, {"id": 2, "full_path": "beta"}}))
	s.API.Handle("GET", "/api/v4/groups/1/members/7", gitlabtest.OK(map[string]any{"access_level": 40, "state": "active"}))
	s.API.Handle("GET", "/api/v4/groups/2/projects?include_subgroups=true&archived=false&order_by=id&sort=asc&per_page=100&page=1",
		gitlabtest.OK([]map[string]any{{"id": 5, "path_with_namespace": "beta/sub/app", "default_branch": "main"},
			{"id": 6, "path_with_namespace": "beta/docs", "default_branch": "main"}}))
	s.API.Handle("HEAD", "/api/v4/projects/5/repository/files/TAGPOLICY?ref=main", gitlabtest.Status(200))

	r := s.Run(nil, "coverage", "--config", s.config)
	assert.Equal(t, 2, r.Code)
	r = s.Run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, 1, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "beta: repopolicy is not a member, Maintainer is required")
	assert.Contains(t, r.Stdout, "projects with policy files, their protected changes are denied: beta/sub/app\n")
	assert.Contains(t, r.Stdout, "1 of 2 top-level groups need repopolicy as a Maintainer")
	tokens := s.API.Tokens()
	assert.Equal(t, "glpat-test", tokens[0], "the service account is identified with its own token")
	for _, tok := range tokens[1:] {
		assert.Equal(t, "glpat-admin", tok, "everything else is asked with the administrator token")
	}
	data, err := os.ReadFile(adminToken)
	require.NoError(t, err)
	assert.Equal(t, "glpat-admin\n", string(data))

	s.API.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount), gitlabtest.OK(map[string]any{"id": 2, "username": "alice"}))
	r = s.Run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "alice is not an administrator")

	s.API.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount), gitlabtest.OK(admin))
	s.API.Handle("GET", "/api/v4/groups/2/members/7", gitlabtest.OK(map[string]any{"access_level": 40, "state": "active"}))
	r = s.Run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, 0, r.Code, r.Stdout+r.Stderr)
	assert.Contains(t, r.Stdout, "repopolicy is a Maintainer in all 2 top-level groups")
}

// projectAPI scripts the project lookup of explain, admins and validate --online.
func projectAPI(s *Stand, policy string) {
	s.API.Handle("GET", "/api/v4/projects/acme%2Fplatform%2Fapp", gitlabtest.OK(map[string]any{"id": 1,
		"path_with_namespace": "acme/platform/app", "default_branch": "main"}))
	s.API.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.OK(policy))
}

func TestCLI_Explain(t *testing.T) {
	s, _ := pathStand(t, pathRelease, nil)
	projectAPI(s, pathRelease)
	explain := func(user, path, ref string, code int) CLI {
		r := s.Run(nil, "explain", "--config", s.config, "--project", "acme/platform/app", "--user", user, "--path", path, "--ref", ref)
		require.Equal(t, code, r.Code, "0 allowed or no rule, 1 denied: %s", r.Stderr)
		return r
	}
	r := explain("bob", "ci.yml", "release/1", 1)
	assert.Contains(t, r.Stdout, `release-ci (match: [/^release\//]; from file): NOT allowed; allow: alice`)
	assert.Contains(t, r.Stdout, "result: bob may not change ci.yml on refs/heads/release/1")
	r = explain("alice", "ci.yml", "refs/heads/release/1", 0)
	assert.Contains(t, r.Stdout, "result: alice may change ci.yml")
	r = explain("bob", "ci.yml", "", 0)
	assert.Contains(t, r.Stdout, `release-ci (match: [/^release\//]; from file): does not apply to refs/heads/main`)
	r = explain("bob", "README.md", "", 0)
	assert.Contains(t, r.Stdout, "no path rule matches")
	r = explain("bob", "PATHPOLICY", "", 1)
	assert.Contains(t, r.Stdout, "admins of PATHPOLICY (match: all; from file): NOT allowed; allow: alice")

	r = s.Run(nil, "explain", "--config", s.config, "--project", "acme/platform/app")
	assert.Equal(t, 2, r.Code)
	s.API.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.Status(404))
	r = explain("bob", "ci.yml", "", 0)
	assert.Contains(t, r.Stdout, "has no PATHPOLICY on its default branch")
}

func TestCLI_Admins(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	projectAPI(s, pathPolicy)
	r := s.Run(nil, "admins", "--config", s.config, "--project", "acme/platform/app")
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Regexp(t, `(?m)^  alice +effective$`, r.Stdout)
	assert.Contains(t, r.Stdout, "acme/platform/app: configured 1, effective 1, missing 0\nrecovery not active\n")

	s.Member(2, 30)
	r = s.Run(nil, "admins", "--config", s.config, "--project", "acme/platform/app")
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "missing: access level 30, Maintainer (40) is required")
	assert.Contains(t, r.Stdout, "recovery active: an Owner of the project may change PATHPOLICY")

	s.API.Handle("POST", "/api/graphql", gitlabtest.Status(502))
	r = s.Run(nil, "admins", "--config", s.config, "--project", "acme/platform/app")
	assert.Equal(t, 1, r.Code, "a failure of GitLab is never zero admins")
	assert.Contains(t, r.Stderr, "GitLab did not answer")
}

func TestCLI_ValidateOnline(t *testing.T) {
	s, _ := pathStand(t, pathPolicy, nil)
	projectAPI(s, pathPolicy)
	ghost := strings.Replace(pathPolicy, "allow: [dev-lead]", "allow: [dev-lead, ghost]", 1)
	file := filepath.Join(s.Work, "PATHPOLICY")
	require.NoError(t, os.WriteFile(file, []byte(ghost), 0o600))

	r := s.Run(nil, "validate", "-f", file)
	require.Equal(t, 0, r.Code, "offline the username is only checked for format: %s", r.Stdout)
	r = s.Run(nil, "validate", "-f", file, "--online")
	assert.Equal(t, 2, r.Code, "--online needs --config")

	r = s.Run(nil, "validate", "-f", file, "--online", "--config", s.config)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stdout, `error: "ghost" is not an active GitLab user`)
	assert.Contains(t, r.Stdout, "not checked: admins cannot be checked: --project is not given")

	r = s.Run(nil, "validate", "-f", file, "--online", "--config", s.config, "--project", "acme/platform/app")
	assert.Equal(t, 1, r.Code)
	assert.NotContains(t, r.Stdout, "not checked")

	require.NoError(t, os.WriteFile(file, []byte(pathPolicy), 0o600))
	r = s.Run(nil, "validate", "-f", file, "--online", "--config", s.config, "--project", "acme/platform/app")
	assert.Equal(t, 0, r.Code, r.Stdout)
	assert.Contains(t, r.Stdout, "PATHPOLICY: valid")

	// bob is a Developer: the file is valid while alice is an effective admin.
	require.NoError(t, os.WriteFile(file, []byte(strings.Replace(pathPolicy, "admins: [alice]", "admins: [alice, bob]", 1)), 0o600))
	s.API.Handle("GET", "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=2&user_ids%5B%5D=3", gitlabtest.OK([]any{
		map[string]any{"id": 2, "access_level": 40, "state": "active"}, map[string]any{"id": 3, "access_level": 30, "state": "active"}}))
	r = s.Run(nil, "validate", "-f", file, "--online", "--config", s.config, "--project", "acme/platform/app")
	assert.Equal(t, 0, r.Code, r.Stdout)
	assert.Contains(t, r.Stdout, "PATHPOLICY: valid\n")
	assert.Contains(t, r.Stdout, `warning: admin "bob" is not an active user with Maintainer or higher in this project`)
	assert.Contains(t, r.Stdout, "warning: 1 of 2 admins work")
}

// TestCLI_Test: gitlab-repopolicy test decides like the hook without a push.
func TestCLI_Test(t *testing.T) {
	s, base := pathStand(t, pathPolicy, map[string]string{"ci.yml": "v1"})
	head := s.Commit("change ci", map[string]string{"ci.yml": "v2"})
	run := func(args ...string) CLI {
		return s.Run(nil, append([]string{"test", "--config", s.config, "--repo", s.Bare, "--project", "acme/platform/app"}, args...)...)
	}
	s.git(s.Work, "push", "-q", s.Bare, "HEAD:refs/heads/candidate")

	r := run("--old", base, "--new", head, "--ref", "refs/heads/main", "--user", "bob", "--gl-id", "user-3")
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "GL-HOOK-ERR")
	assert.Contains(t, r.Stderr, "ci.yml")
	assert.Empty(t, r.Stdout, "audit.stdout is off: the events go to the file")
	ev := Events(s.Audit(), "decision")
	require.Len(t, ev, 1)
	assert.Equal(t, "deny", ev[0]["decision"])
	assert.Equal(t, "bob", ev[0]["username"])

	s.API.Handle("GET", "/api/v4/users?username=alice", gitlabtest.OK([]map[string]any{{"id": 2, "username": "alice", "state": "active"}}))
	r = run("--old", base, "--new", "candidate", "--ref", "refs/heads/main", "--user", "alice")
	assert.Equal(t, 0, r.Code, r.Stderr)
	assert.Equal(t, base, s.RemoteRev("refs/heads/main"), "test never changes refs")
	assert.Contains(t, r.Stderr, "allowed: the rules allow the change, 0 new commits", "candidate is already a branch")

	r = run("--old", base, "--new", head, "--ref", "refs/heads/main", "--user", "bob", "--gl-id", "user-3", "--project-id", "1")
	require.Equal(t, 1, r.Code)
	app := s.Commit("app", map[string]string{"app.go": "x"})
	// The objects of a push are in the repository before any ref points to them.
	s.git(s.Work, "push", "-q", s.Bare, app+":refs/heads/incoming")
	s.git(s.Bare, "update-ref", "-d", "refs/heads/incoming")
	r = run("--old", head, "--new", app, "--ref", "refs/heads/candidate", "--user", "bob", "--gl-id", "user-3")
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stderr, "allowed: no rule was touched, 1 new commits")
	r = run("--old", zeroOID, "--new", "candidate", "--ref", "refs/heads/copy", "--user", "bob", "--gl-id", "user-3")
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stderr, "run test in a mirror copy of the project", "a new branch whose commits are on other branches")

	r = run("--old", base, "--new", head, "--ref", "refs/heads/main", "--user", "nobody")
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "cannot be looked up")
	r = run("--old", base, "--ref", "refs/heads/main", "--user", "bob")
	assert.Equal(t, 2, r.Code)
}

// TestCLI_Test_StdoutOnlyAudit: with audit.stdout a warning of an allowed
// update goes to stderr, so that stdout holds only audit events.
func TestCLI_Test_StdoutOnlyAudit(t *testing.T) {
	s, base := mainWith(t, map[string]string{"NOTEPOLICY": "guard ci ci.yml allow alice\n", "ci.yml": "x"})
	head := s.Commit("change", map[string]string{"ci.yml": "y"})
	cfg := filepath.Join(s.Dir, "stdout.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(fmt.Sprintf("mode: enforce\ngitlab_url: %s\ntoken_file: %s\naudit:\n  file: \"\"\n  stdout: true\n",
		s.API.URL(), filepath.Join(s.Dir, "token"))), 0o600))
	r := s.Run(nil, "test", "--config", cfg, "--repo", s.Work, "--old", base, "--new", head, "--ref", "refs/heads/main",
		"--user", "bob", "--gl-id", "user-3")
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stderr, "NOTEPOLICY", "the warning is printed")
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	require.NotEmpty(t, lines)
	for _, line := range lines {
		var e map[string]any
		assert.NoError(t, json.Unmarshal([]byte(line), &e), "stdout holds only audit events: %q", line)
	}
}

func TestCLI_Version(t *testing.T) {
	s := NewStand(t)
	r := s.Run(nil, "version")
	require.Equal(t, 0, r.Code)
	assert.Regexp(t, `^gitlab-repopolicy \S+\ncommit: .*\ndate: .*\ngo: go1\.`, r.Stdout)
	r = s.Run(nil, "help")
	require.Equal(t, 0, r.Code)
	for _, cmd := range []string{"hook", "install-hook", "uninstall-hook", "validate", "explain", "lint-commit", "test", "admins",
		"selftest", "settings-check", "breakglass", "version", "coverage", "token"} {
		assert.Regexp(t, `(?m)^  `+cmd+` `, r.Stdout)
	}
}

// TestHookOutputOnlyReport: in hook mode the pusher sees only the report, no
// diagnostic log.
func TestHookOutputOnlyReport(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "x"})
	s.Commit("readme", map[string]string{"README.md": "x"})
	r := s.Push(bob, "HEAD:refs/heads/main")
	assertAllowed(t, r)
	assert.NotContains(t, r.Output, "remote:", "a push without touched rules prints nothing")

	s.Commit("change", map[string]string{"ci.yml": "y"})
	r = s.Push(bob, "HEAD:refs/heads/main")
	assertDenied(t, r)
	sc := bufio.NewScanner(strings.NewReader(r.Output))
	for sc.Scan() {
		line := sc.Text()
		assert.NotRegexp(t, `level=|time=|msg=`, line, "slog in hook mode")
	}

	// A broken config is also reported to the pusher as text only.
	require.NoError(t, os.WriteFile(s.config, []byte("mode: bogus\n"), 0o600))
	r = s.Push(bob, "HEAD:refs/heads/main")
	assertDenied(t, r, "the hook configuration on the server is invalid")
	assert.NotRegexp(t, `level=|time=|msg=|config:`, r.Output)
}

// releaseVerify runs gitlab-repopolicy test as make release-verify-tag does.
func releaseVerify(t *testing.T, s *Stand, tag, oid string) (CLI, []map[string]any) {
	t.Helper()
	cfg, err := filepath.Abs("../../config/release-verify.yaml")
	require.NoError(t, err)
	r := s.Run(nil, "test", "--repo", s.Work, "--old", zeroOID, "--new", oid, "--ref", "refs/tags/"+tag,
		"--user", "release", "--config", cfg)
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &e), "stdout holds only audit events: %q", line)
		events = append(events, e)
	}
	return r, events
}

// TestReleaseVerify_Signed: the allow decision of tagpolicy is on stdout.
func TestReleaseVerify_Signed(t *testing.T) {
	s, base, lead, _ := tagStand(t)
	oid := s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", &lead)
	r, ev := releaseVerify(t, s, "v1.0.0-prod", oid)
	require.Equal(t, 0, r.Code, r.Stderr)
	require.Len(t, ev, 1)
	assert.Equal(t, "tagpolicy", ev[0]["module"])
	assert.Equal(t, "allow", ev[0]["decision"])
	assert.Equal(t, "refs/tags/v1.0.0-prod", ev[0]["ref"])
	assert.Equal(t, lead.fpr, ev[0]["fingerprint"])
	assert.Zero(t, s.API.TotalCalls(), "release-verify does not use the API")
}

// TestReleaseVerify_Unsigned: an unsigned tag fails the release.
func TestReleaseVerify_Unsigned(t *testing.T) {
	s, base, _, _ := tagStand(t)
	oid := s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", nil)
	r, ev := releaseVerify(t, s, "v1.0.0-prod", oid)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "GL-HOOK-ERR")
	require.Len(t, ev, 1)
	assert.Equal(t, "deny", ev[0]["decision"])
	assert.Equal(t, "tag_not_signed", ev[0]["violations"].([]any)[0].(map[string]any)["code"])

	lw := s.Rev("HEAD")
	r, _ = releaseVerify(t, s, "v1.0.1-prod", lw)
	assert.Equal(t, 1, r.Code, "a lightweight tag fails too")
}

// TestReleaseVerify_ForeignKey: a tag signed by a key that TAGPOLICY does not
// hold fails the release.
func TestReleaseVerify_ForeignKey(t *testing.T) {
	s, base, _, _ := tagStand(t)
	stranger := pgpSigner(t)
	oid := s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", &stranger)
	r, ev := releaseVerify(t, s, "v1.0.0-prod", oid)
	assert.Equal(t, 1, r.Code)
	require.Len(t, ev, 1)
	assert.Equal(t, "deny", ev[0]["decision"])
	assert.Equal(t, "tag_signer_not_allowed", ev[0]["violations"].([]any)[0].(map[string]any)["code"])
}
