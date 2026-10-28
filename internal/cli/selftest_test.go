package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
)

var serviceAccount = map[string]any{"id": 7, "username": "repopolicy", "bot": true, "external": true}

// healthyAPI scripts the answers of a service account that passes selftest.
func healthyAPI(s *stand) {
	s.api.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount))
	s.api.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(tokenSelf(time.Now().AddDate(1, 0, 0))))
	s.api.Handle("GET", "/api/v4/groups?min_access_level=40&per_page=100", gitlabtest.OK([]any{map[string]any{"id": 3, "full_path": "acme"}}))
}

func TestSelftest(t *testing.T) {
	s := newStand(t, "")
	healthyAPI(s)
	code, out, _ := s.run(nil, "selftest", "--config", s.config)
	require.Equal(t, ExitOK, code, out)
	for _, re := range []string{
		`(?m)^info version +gitlab-repopolicy `,
		`(?m)^ok +config +.*config.yaml, mode enforce$`,
		`(?m)^ok +modules +enabled: `,
		`(?m)^ok +audit +.*audit.log does not exist yet; the directory belongs to `,
		`(?m)^ok +breakglass +.*breakglass, no active grants$`,
		`(?m)^ok +api +http\+unix://.* reachable as repopolicy \(id 7\)$`,
		`(?m)^ok +account +repopolicy is a service account$`,
		`(?m)^ok +groups +Maintainer of 1 groups;`,
		`(?m)^ok +token +scopes read_api, self_rotate, expires on `,
		`(?m)^info git +`,
	} {
		assert.Regexp(t, re, out)
	}

	code, _, _ = s.run(nil, "selftest", "extra")
	assert.Equal(t, ExitUsage, code)
	code, out, _ = s.run(nil, "selftest", "--config", filepath.Join(s.dir, "missing.yaml"))
	assert.Equal(t, ExitError, code)
	assert.Regexp(t, `(?m)^FAIL config +.*missing.yaml: `, out)
}

func TestSelftest_Account(t *testing.T) {
	inYear := time.Now().AddDate(1, 0, 0)
	for name, tc := range map[string]struct {
		user, token map[string]any
		groups      []any
		want        string
		code        int
	}{
		"administrator": {user: map[string]any{"id": 1, "username": "root", "bot": true, "external": true, "is_admin": true},
			want: `(?m)^FAIL account +root is an administrator`, code: ExitError},
		"person":       {user: map[string]any{"id": 2, "username": "alice", "external": true}, want: `(?m)^FAIL account +alice is not a service account`, code: ExitError},
		"not external": {user: map[string]any{"id": 7, "username": "repopolicy", "bot": true}, want: `(?m)^FAIL account +repopolicy is not external`, code: ExitError},
		"no groups":    {groups: []any{}, want: `(?m)^FAIL groups +repopolicy is a Maintainer of no group`, code: ExitError},
		"100 groups":   {groups: make([]any, 100), want: `(?m)^ok +groups +Maintainer of 100 or more groups`, code: ExitOK},
		"extra scope": {token: map[string]any{"scopes": []string{"api", "read_api", "self_rotate"}, "active": true, "expires_at": inYear.Format(time.DateOnly)},
			want: `(?m)^FAIL token +scopes api, read_api, self_rotate; exactly read_api, self_rotate are required`, code: ExitError},
		"revoked": {token: map[string]any{"scopes": []string{"read_api", "self_rotate"}, "active": true, "revoked": true},
			want: `(?m)^FAIL token +the token is not active`, code: ExitError},
		"no expiry": {token: map[string]any{"scopes": []string{"read_api", "self_rotate"}, "active": true},
			want: `(?m)^FAIL token +no expiry date \(""\)`, code: ExitError},
		"expires soon": {token: tokenSelf(time.Now().AddDate(0, 0, 10)),
			want: `(?m)^warn token +expires on .*: run gitlab-repopolicy token rotate`, code: ExitOK},
	} {
		t.Run(name, func(t *testing.T) {
			s := newStand(t, "")
			healthyAPI(s)
			if tc.user != nil {
				s.api.Handle("GET", "/api/v4/user", gitlabtest.OK(tc.user))
			}
			if tc.token != nil {
				s.api.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(tc.token))
			}
			if tc.groups != nil {
				s.api.Handle("GET", "/api/v4/groups?min_access_level=40&per_page=100", gitlabtest.OK(tc.groups))
			}
			code, out, _ := s.run(nil, "selftest", "--config", s.config)
			assert.Equal(t, tc.code, code, out)
			assert.Regexp(t, tc.want, out)
		})
	}
}

func TestSelftest_APIFailures(t *testing.T) {
	s := newStand(t, "")
	healthyAPI(s)
	s.api.Handle("GET", "/api/v4/groups?min_access_level=40&per_page=100", gitlabtest.Status(502))
	s.api.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.Status(502))
	code, out, _ := s.run(nil, "selftest", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Regexp(t, `(?m)^FAIL groups +GET /groups: `, out)
	assert.Regexp(t, `(?m)^FAIL token +GET /personal_access_tokens/self: `, out)

	s.api.Handle("GET", "/api/v4/user", gitlabtest.Status(401))
	code, out, _ = s.run(nil, "selftest", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Regexp(t, `(?m)^FAIL api +GET /user: `, out)
	assert.NotContains(t, out, "account", "nothing is asked after the API fails")
}

func TestSelftest_Files(t *testing.T) {
	s := newStand(t, "")
	healthyAPI(s)
	require.NoError(t, os.Chmod(s.token, 0o644))
	require.NoError(t, os.WriteFile(s.audit, nil, 0o644))
	require.NoError(t, os.RemoveAll(filepath.Join(s.dir, "breakglass")))
	code, out, _ := s.run(nil, "selftest", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Regexp(t, `(?m)^FAIL token +.*token is accessible to others \(-rw-r--r--\); expected root:<git-user> 0440`, out)
	assert.Regexp(t, `(?m)^warn audit +.*audit.log is readable by everyone \(-rw-r--r--\)`, out)
	assert.Regexp(t, `(?m)^warn breakglass .*: break-glass cannot be granted`, out)

	// Root writes to a file whatever its mode.
	if os.Geteuid() != 0 {
		require.NoError(t, os.Chmod(s.audit, 0o440))
		_, out, _ = s.run(nil, "selftest", "--config", s.config)
		assert.Regexp(t, `(?m)^FAIL audit +.*audit.log is not writable: `, out)
	} else {
		t.Log("running as root: a read-only audit log is writable, not checked")
	}

	require.NoError(t, os.Remove(s.audit))
	require.NoError(t, os.Mkdir(s.audit, 0o700))
	_, out, _ = s.run(nil, "selftest", "--config", s.config)
	assert.Regexp(t, `(?m)^FAIL audit +.*audit.log is not a regular file`, out)

	s.audit = filepath.Join(s.dir, "missing", "audit.log")
	s.writeConfig("")
	_, out, _ = s.run(nil, "selftest", "--config", s.config)
	assert.Regexp(t, `(?m)^FAIL audit +log directory: `, out)

	require.NoError(t, os.Remove(s.token))
	_, out, _ = s.run(nil, "selftest", "--config", s.config)
	assert.Regexp(t, `(?m)^FAIL token +.*no such file`, out)
}

func TestSelftest_Modules(t *testing.T) {
	s := newStand(t, "modules: {pathpolicy: disabled, commitpolicy: disabled, tagpolicy: disabled, branchpolicy: disabled}\n")
	healthyAPI(s)
	_, out, _ := s.run(nil, "selftest", "--config", s.config)
	assert.Regexp(t, `(?m)^warn modules +all modules are disabled`, out)

	s = newStand(t, "presets:\n  commitpolicy:\n    broken: {rules: {x: {parser: nope}}}\n")
	healthyAPI(s)
	code, out, _ := s.run(nil, "selftest", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Regexp(t, `(?m)^FAIL modules +.*broken`, out)
}

const memberProjects = "/api/v4/projects?membership=true&archived=false&order_by=id&sort=asc&per_page=100&id_after=0"

func TestSettingsCheck(t *testing.T) {
	s := newStand(t, "")
	code, _, _ := s.run(nil, "settings-check", "extra")
	assert.Equal(t, ExitUsage, code)
	code, _, errOut := s.run(nil, "settings-check", "--config", filepath.Join(s.dir, "missing.yaml"))
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "gitlab-repopolicy settings-check: ")

	s.api.Handle("GET", memberProjects, gitlabtest.OK([]any{}))
	code, out, errOut := s.run(nil, "settings-check", "--config", s.config)
	require.Equal(t, ExitOK, code, out+errOut)
	assert.Equal(t, "0 projects with policy files checked, no drift\n", out)
	assert.Contains(t, errOut, `level=INFO msg="settings-check done" checked=0 drifted=0`)

	// A project whose settings GitLab does not give is a drift: it is not verified.
	s.api.Handle("GET", memberProjects, gitlabtest.OK([]map[string]any{{"id": 1, "path_with_namespace": "acme/app", "default_branch": "main",
		"namespace": map[string]any{"kind": "group"}, "permissions": map[string]any{"group_access": map[string]any{"access_level": 40}}}}))
	s.api.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.Status(502))
	code, out, _ = s.run(nil, "settings-check", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, "acme/app: unavailable: ")
	ev := s.events("settings_drift")
	require.Len(t, ev, 1)
	assert.Equal(t, "acme/app", ev[0]["project_path"])

	s.api.Handle("GET", memberProjects, gitlabtest.Status(401))
	code, _, errOut = s.run(nil, "settings-check", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "list projects")
}

func TestSettingsCheck_AuditNotWritable(t *testing.T) {
	s := newStand(t, "")
	s.api.Handle("GET", memberProjects, gitlabtest.OK([]any{}))
	require.NoError(t, os.WriteFile(s.audit, nil, 0o444))
	code, out, _ := s.run(nil, "settings-check", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, "audit: "+s.audit+" is not writable by its owner, mode -r--r--r--: the hook loses its events\n")

	require.NoError(t, os.Remove(s.audit))
	require.NoError(t, os.Mkdir(s.audit, 0o700))
	code, out, errOut := s.run(nil, "settings-check", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, "audit: "+s.audit+" is not a regular file: the hook loses its events\n")
	assert.Contains(t, errOut, "level=ERROR msg=audit", "the alert itself cannot be written")
}

func TestCoverage(t *testing.T) {
	s := newStand(t, "")
	adminToken := filepath.Join(s.dir, "admin-token")
	require.NoError(t, os.WriteFile(adminToken, []byte("glpat-admin\n"), 0o600))
	admin := map[string]any{"id": 1, "username": "root", "is_admin": true}
	groups := "/api/v4/groups?top_level_only=true&all_available=true&order_by=id&sort=asc&per_page=100&page=1"

	code, _, errOut := s.run(nil, "coverage", "--config", s.config)
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--token-file is required")
	code, _, _ = s.run(nil, "coverage", "extra")
	assert.Equal(t, ExitUsage, code)
	code, _, _ = s.run(nil, "coverage", "--config", filepath.Join(s.dir, "missing.yaml"), "--token-file", adminToken)
	assert.Equal(t, ExitError, code)

	s.api.Handle("GET", "/api/v4/user", gitlabtest.Status(401))
	code, _, errOut = s.run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "the service account token: ")
	s.api.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount), gitlabtest.Status(401))
	code, _, errOut = s.run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "--token-file: ")
	s.api.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount), gitlabtest.OK(map[string]any{"id": 2, "username": "alice"}))
	code, _, errOut = s.run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "alice is not an administrator")

	s.api.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount), gitlabtest.OK(admin))
	s.api.Handle("GET", groups, gitlabtest.Status(502))
	code, _, errOut = s.run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "gitlab-repopolicy coverage: ")

	s.api.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount), gitlabtest.OK(admin))
	s.api.Handle("GET", groups, gitlabtest.OK([]map[string]any{{"id": 1, "full_path": "acme"}, {"id": 2, "full_path": "beta"}, {"id": 3, "full_path": "gamma"}}))
	s.api.Handle("GET", "/api/v4/groups/1/members/7", gitlabtest.OK(map[string]any{"access_level": 40, "state": "active"}))
	s.api.Handle("GET", "/api/v4/groups/3/members/7", gitlabtest.OK(map[string]any{"access_level": 30, "state": "active"}))
	s.api.Handle("GET", "/api/v4/groups/2/projects?include_subgroups=true&archived=false&order_by=id&sort=asc&per_page=100&page=1",
		gitlabtest.OK([]map[string]any{{"id": 5, "path_with_namespace": "beta/app", "default_branch": "main"}}))
	s.api.Handle("GET", "/api/v4/groups/3/projects?include_subgroups=true&archived=false&order_by=id&sort=asc&per_page=100&page=1",
		gitlabtest.OK([]any{}))
	s.api.Handle("HEAD", "/api/v4/projects/5/repository/files/TAGPOLICY?ref=main", gitlabtest.Status(200))
	code, out, errOut := s.run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, ExitError, code, errOut)
	assert.Equal(t, "beta: repopolicy is not a member, Maintainer is required\n"+
		"  projects with policy files, their protected changes are denied: beta/app\n"+
		"gamma: repopolicy is access level 30, Maintainer is required\n"+
		"2 of 3 top-level groups need repopolicy as a Maintainer\n", out)

	s.api.Handle("GET", "/api/v4/user", gitlabtest.OK(serviceAccount), gitlabtest.OK(admin))
	s.api.Handle("GET", groups, gitlabtest.OK([]map[string]any{{"id": 1, "full_path": "acme"}}))
	code, out, _ = s.run(nil, "coverage", "--config", s.config, "--token-file", adminToken)
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "repopolicy is a Maintainer in all 1 top-level groups\n", out)
}
