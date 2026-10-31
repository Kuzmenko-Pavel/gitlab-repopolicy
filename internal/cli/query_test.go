package cli

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
)

//go:embed testdata/pathpolicy-extends.yaml
var explainPolicy string

// TestExplain_PresetFields: a rule from a preset names the layer of each of
// its fields, a rule of the file alone does not.
func TestExplain_PresetFields(t *testing.T) {
	api := gitlabtest.NewUnix(t)
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(token, []byte("glpat-test\n"), 0o600))
	policy := explainPolicy
	config := writeConfig(t, fmt.Sprintf(`gitlab_url: %s
token_file: %s
audit: {file: /tmp/x.log}
presets:
  pathpolicy:
    org-ci:
      rules:
        ci: {match: all, paths: [ci.yml, docs/**], allow: [alice], message: CI belongs to alice.}
`, api.URL(), token))
	api.Handle("GET", "/api/v4/projects/acme%2Fapp", gitlabtest.OK(map[string]any{"id": 1, "path_with_namespace": "acme/app", "default_branch": "main"}))
	api.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.OK(policy))

	code, out, errOut := run(t, "explain", "--config", config, "--project", "acme/app", "--user", "bob", "--path", "docs/a.md")
	require.Equal(t, ExitError, code, errOut)
	assert.Contains(t, out, "  ci (match: all; from preset:org-ci, file): allowed; allow: alice, bob\n"+
		"    fields from: allow file; match preset:org-ci; message preset:org-ci; paths preset:org-ci\n")
	assert.Contains(t, out, "  docs (match: all; from file): NOT allowed; allow: alice\nresult: bob may not change docs/a.md")
}

// projectAPI scripts project acme/app with the PATHPOLICY on its default branch.
func projectAPI(s *stand, policy string) {
	s.api.Handle("GET", "/api/v4/projects/acme%2Fapp", gitlabtest.OK(map[string]any{"id": 1, "path_with_namespace": "acme/app", "default_branch": "main"}))
	s.api.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.OK(policy))
}

func TestExplain(t *testing.T) {
	s := newStand(t, "")
	team(s)
	projectAPI(s, ciPolicy)
	explain := func(args ...string) (int, string, string) {
		return s.run(nil, append([]string{"explain", "--config", s.config, "--project", "acme/app"}, args...)...)
	}

	code, out, errOut := explain("--user", "bob", "--path", "README.md")
	require.Equal(t, ExitOK, code, errOut)
	assert.Equal(t, "acme/app, PATHPOLICY of main; path README.md, ref refs/heads/main, user bob\n"+
		"no path rule matches: anyone who may push to the ref may change it\n", out)
	code, out, _ = explain("--user", "alice", "--path", "/ci.yml", "--ref", "refs/heads/topic")
	require.Equal(t, ExitOK, code)
	assert.Contains(t, out, "result: alice may change /ci.yml on refs/heads/topic (as a person: a deploy key never may)\n")

	code, _, errOut = explain("--user", "bob")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--project, --user and --path are required")
	code, _, _ = s.run(nil, "explain", "--config", filepath.Join(s.dir, "missing.yaml"), "--project", "a", "--user", "b", "--path", "c")
	assert.Equal(t, ExitError, code)

	s.api.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.OK("version: 1\nrules: [\n"))
	code, _, errOut = explain("--user", "bob", "--path", "ci.yml")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "PATHPOLICY on main is invalid: ")
	s.api.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.Status(502))
	code, _, _ = explain("--user", "bob", "--path", "ci.yml")
	assert.Equal(t, ExitError, code)
	s.api.Handle("GET", "/api/v4/projects/acme%2Fapp", gitlabtest.OK(map[string]any{"id": 1, "path_with_namespace": "acme/app"}))
	code, out, _ = explain("--user", "bob", "--path", "ci.yml")
	require.Equal(t, ExitOK, code)
	assert.Equal(t, "acme/app has no PATHPOLICY on its default branch: no path is protected\n", out, "an empty project")
	s.writeConfig("modules: {pathpolicy: disabled}\n")
	code, _, errOut = explain("--user", "bob", "--path", "ci.yml")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "the PATHPOLICY module is disabled in the server config")
	s.api.Handle("GET", "/api/v4/projects/acme%2Fapp", gitlabtest.Status(404))
	code, _, errOut = explain("--user", "bob", "--path", "ci.yml")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "project acme/app: ")
}

func TestAdmins(t *testing.T) {
	s := newStand(t, "")
	policy := strings.Replace(ciPolicy, "admins: [alice]", "admins: [alice, bob, ghost, carol]", 1)
	projectAPI(s, policy)
	users := func(nodes ...any) {
		s.api.Handle("POST", "/api/graphql", gitlabtest.OK(map[string]any{"data": map[string]any{"users": map[string]any{"nodes": nodes}}}))
	}
	users(map[string]any{"id": "gid://gitlab/User/2", "username": "alice", "state": "active"},
		map[string]any{"id": "gid://gitlab/User/3", "username": "bob", "state": "active"},
		map[string]any{"id": "gid://gitlab/User/4", "username": "carol", "state": "deactivated"})
	s.api.Handle("GET", "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=2&user_ids%5B%5D=3", gitlabtest.OK([]any{
		map[string]any{"id": 2, "access_level": 40, "state": "active"}, map[string]any{"id": 3, "access_level": 30, "state": "active"}}))

	code, out, errOut := s.run(nil, "admins", "--config", s.config, "--project", "acme/app")
	require.Equal(t, ExitOK, code, errOut)
	assert.Regexp(t, `(?m)^  alice +effective$`, out)
	assert.Regexp(t, `(?m)^  bob +missing: access level 30, Maintainer \(40\) is required$`, out)
	assert.Regexp(t, `(?m)^  ghost +missing: no such user, or blocked or banned$`, out)
	assert.Regexp(t, `(?m)^  carol +missing: not active$`, out)
	assert.Contains(t, out, "acme/app: configured 4, effective 1, missing 3\nrecovery not active\n")

	s.api.Handle("GET", "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=2&user_ids%5B%5D=3", gitlabtest.OK([]any{
		map[string]any{"id": 2, "access_level": 30, "state": "active"}, map[string]any{"id": 3, "access_level": 30, "state": "active"}}))
	code, out, _ = s.run(nil, "admins", "--config", s.config, "--project", "acme/app")
	require.Equal(t, ExitOK, code)
	assert.Contains(t, out, "recovery active: an Owner of the project may change PATHPOLICY\n")

	s.api.Handle("POST", "/api/graphql", gitlabtest.Status(502))
	code, _, errOut = s.run(nil, "admins", "--config", s.config, "--project", "acme/app")
	assert.Equal(t, ExitError, code, "a failure of GitLab is never zero admins")
	assert.Contains(t, errOut, "GitLab did not answer: ")

	s.api.Handle("GET", "/api/v4/projects/1/repository/files/PATHPOLICY/raw?ref=main", gitlabtest.Status(404))
	code, out, _ = s.run(nil, "admins", "--config", s.config, "--project", "acme/app")
	require.Equal(t, ExitOK, code)
	assert.Equal(t, "acme/app has no PATHPOLICY on its default branch: a Maintainer may add the first one (bootstrap)\n", out)

	code, _, errOut = s.run(nil, "admins", "--config", s.config)
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--project is required")
	code, _, _ = s.run(nil, "admins", "--config", filepath.Join(s.dir, "missing.yaml"), "--project", "acme/app")
	assert.Equal(t, ExitError, code)
	s.api.Handle("GET", "/api/v4/projects/acme%2Fapp", gitlabtest.Status(502))
	code, _, _ = s.run(nil, "admins", "--config", s.config, "--project", "acme/app")
	assert.Equal(t, ExitError, code)
}
