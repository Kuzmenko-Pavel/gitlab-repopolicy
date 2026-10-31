package cli

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
)

//go:embed testdata/pathpolicy-ci.yaml
var ciPolicy string

// repo is a repository whose main holds PATHPOLICY and ci.yml (base), and a
// commit that changes ci.yml and that no ref holds yet (change), as in a push.
type repo struct {
	dir, base, change string
	env               []string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{dir: t.TempDir(), env: append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com")}
	r.git(t, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, "PATHPOLICY"), []byte(ciPolicy), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, "ci.yml"), []byte("v1\n"), 0o600))
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "base")
	r.base = r.git(t, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, "ci.yml"), []byte("v2\n"), 0o600))
	r.git(t, "commit", "-q", "-am", "change ci")
	r.change = r.git(t, "rev-parse", "HEAD")
	r.git(t, "update-ref", "refs/heads/main", r.base)
	return r
}

func (r *repo) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir, cmd.Env = r.dir, r.env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// team scripts alice as a Maintainer and bob as a Developer of project 1.
func team(s *stand) {
	s.api.Handle("GET", "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=2",
		gitlabtest.OK([]any{map[string]any{"id": 2, "access_level": 40, "state": "active"}}))
	s.api.Handle("GET", "/api/v4/projects/1/members/all?per_page=100&user_ids%5B%5D=3",
		gitlabtest.OK([]any{map[string]any{"id": 3, "access_level": 30, "state": "active"}}))
	s.api.Handle("GET", "/api/v4/projects/1/protected_branches?per_page=100&page=1", gitlabtest.OK([]any{map[string]any{"name": "main"}}))
	s.api.Handle("GET", "/api/v4/projects/1/protected_tags?per_page=100&page=1", gitlabtest.OK([]any{}))
}

func TestRunTest(t *testing.T) {
	s := newStand(t, "")
	team(s)
	r := newRepo(t)
	env := []string{"PATH=" + os.Getenv("PATH")}
	test := func(args ...string) (int, string, string) {
		return s.run(env, append([]string{"test", "--config", s.config, "--repo", r.dir, "--project", "acme/platform/app"}, args...)...)
	}

	code, _, errOut := test("--old", r.base, "--new", r.change, "--ref", "refs/heads/main", "--user", "bob", "--gl-id", "user-3")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "GL-HOOK-ERR")
	assert.Contains(t, errOut, "CI belongs to alice.")
	ev := s.events("decision")
	require.Len(t, ev, 1)
	assert.Equal(t, "deny", ev[0]["decision"])
	assert.NotEmpty(t, ev[0]["git"], "the event names the version of git")

	s.api.Handle("GET", "/api/v4/users?username=alice", gitlabtest.OK([]map[string]any{{"id": 2, "username": "alice", "state": "active"}}))
	code, _, errOut = test("--old", r.base, "--new", r.change, "--ref", "refs/heads/main", "--user", "alice")
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, errOut, "allowed: the rules allow the change, 1 new commits\n")
	assert.Equal(t, r.base, r.git(t, "rev-parse", "main"), "test never changes refs")

	code, _, errOut = test("--old", strings.Repeat("0", 40), "--new", "main", "--ref", "refs/heads/copy", "--user", "bob", "--gl-id", "user-3")
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, errOut, "allowed: no rule was touched, 0 new commits\n")
	assert.Contains(t, errOut, "run test in a mirror copy of the project")
	code, _, errOut = test("--old", r.base, "--new", strings.Repeat("0", 40), "--ref", "refs/heads/topic", "--user", "bob", "--gl-id", "user-3")
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, errOut, "allowed: the ref is deleted\n")

	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--old", r.base, "--ref", "refs/heads/main", "--user", "bob"}, ExitUsage, "are required"},
		{[]string{"--old", r.base, "--new", r.change, "--ref", "refs/heads/main", "--user", "nobody"}, ExitError, "user nobody cannot be looked up"},
		{[]string{"--old", "0", "--new", "0", "--ref", "refs/heads/main", "--user", "bob", "--gl-id", "user-3"}, ExitError, "--old and --new are both zeros"},
		{[]string{"--old", "-x", "--new", r.change, "--ref", "refs/heads/main", "--user", "bob", "--gl-id", "user-3"}, ExitError, `invalid revision "-x"`},
		{[]string{"--old", r.base, "--new", "no-such-rev", "--ref", "refs/heads/main", "--user", "bob", "--gl-id", "user-3"}, ExitError, "no-such-rev: "},
	} {
		code, _, errOut := test(tc.args...)
		assert.Equal(t, tc.code, code, "%q: %s", tc.args, errOut)
		assert.Contains(t, errOut, tc.want, "%q", tc.args)
	}
	code, _, errOut = s.run(env, "test", "--config", s.config, "--repo", t.TempDir(), "--old", r.base, "--new", r.change,
		"--ref", "refs/heads/main", "--user", "bob")
	assert.Equal(t, ExitError, code, "not a repository")
	assert.Contains(t, errOut, "gitlab-repopolicy test: ")
	code, _, _ = s.run(env, "test", "--config", filepath.Join(s.dir, "missing.yaml"), "--repo", r.dir, "--old", r.base, "--new", r.change,
		"--ref", "refs/heads/main", "--user", "bob")
	assert.Equal(t, ExitError, code)
}

// TestHook_Decides runs the hook as Gitaly does: the environment of the
// push, the ref updates on stdin.
func TestHook_Decides(t *testing.T) {
	s := newStand(t, "")
	team(s)
	r := newRepo(t)
	env := func(user, glID string) []string {
		return []string{"GL_ID=" + glID, "GL_USERNAME=" + user, "GL_PROTOCOL=ssh", "GL_REPOSITORY=project-1",
			"GL_PROJECT_PATH=acme/platform/app", "GIT_DIR=" + filepath.Join(r.dir, ".git"), "PATH=" + os.Getenv("PATH")}
	}
	stdin := r.base + " " + r.change + " refs/heads/main\n"

	code, out, errOut := s.runStdin(stdin, env("bob", "user-3"), "hook", "pre-receive", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out+errOut, "CI belongs to alice.")
	require.Len(t, s.events("decision"), 1)

	code, _, errOut = s.runStdin(stdin, env("alice", "user-2"), "hook", "pre-receive", "--config", s.config)
	assert.Equal(t, ExitOK, code, errOut)

	// A wiki or a snippet is not a project: no policy applies.
	wiki := append(env("bob", "user-3"), "GL_REPOSITORY=wiki-1")
	code, _, _ = s.runStdin(stdin, wiki, "hook", "pre-receive", "--config", s.config)
	assert.Equal(t, ExitOK, code)

	s.writeConfig("mode: off\n")
	code, out, errOut = s.runStdin(stdin, env("bob", "user-3"), "hook", "pre-receive", "--config", s.config)
	assert.Equal(t, ExitOK, code)
	assert.Empty(t, out+errOut, "mode off does nothing")
}

// TestHook_AuditNotWritable: the decision stands and the pusher learns that
// the log is broken.
func TestHook_AuditNotWritable(t *testing.T) {
	s := newStand(t, "")
	team(s)
	r := newRepo(t)
	require.NoError(t, os.Mkdir(s.audit, 0o700))
	env := []string{"GL_ID=user-3", "GL_USERNAME=bob", "GL_PROTOCOL=ssh", "GL_REPOSITORY=project-1",
		"GL_PROJECT_PATH=acme/platform/app", "GIT_DIR=" + filepath.Join(r.dir, ".git"), "PATH=" + os.Getenv("PATH")}
	code, _, errOut := s.runStdin(r.base+" "+r.change+" refs/heads/main\n", env, "hook", "pre-receive", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "the audit log is not writable, tell your GitLab administrators")
}
