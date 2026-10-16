//go:build integration

package integration

import (
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

const notRoot = "REPOPOLICY_TEST_NOT_ROOT=1"

// TestInstallHook installs the wrapper, pushes through it, and removes it.
func TestInstallHook(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "x"})
	hooks := filepath.Join(s.Dir, "custom_hooks")
	require.NoError(t, os.Mkdir(hooks, 0o755))
	chownLog := filepath.Join(s.Dir, "chown.log")
	env := make([]string, 0, 2)
	env = append(env, "REPOPOLICY_TEST_CHOWN_LOG="+chownLog)
	args := []string{"install-hook", "--hooks-dir", hooks, "--user", "git", "--binary", hookBin, "--config", s.config}

	r := s.Run(append(env, notRoot), args...)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "must run as root")
	assert.NoDirExists(t, filepath.Join(hooks, "pre-receive.d"))

	r = s.Run(env, "install-hook", "--hooks-dir", hooks, "--user", "no-such-user")
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "--user")
	r = s.Run(env, "install-hook")
	assert.Equal(t, 2, r.Code)

	r = s.Run(env, args...)
	require.Equal(t, 0, r.Code, r.Stderr)
	wrapper := filepath.Join(hooks, "pre-receive.d", "50-gitlab-repopolicy")
	assert.Contains(t, r.Stdout, wrapper+": created")
	data, err := os.ReadFile(wrapper)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("#!/bin/sh\nexec %s hook pre-receive --config %s\n", hookBin, s.config), string(data))
	log, err := os.ReadFile(chownLog)
	require.NoError(t, err)
	ids := fmt.Sprintf(" %d:%d\n", os.Getuid(), os.Getgid())
	group := fmt.Sprintf(" -1:%d\n", os.Getgid())
	assert.Equal(t, 3, strings.Count(string(log), ids), "the directory, the wrapper and the log directory belong to --user:\n%s", log)
	assert.Contains(t, string(log), filepath.Join(hooks, "pre-receive.d")+ids)
	assert.Contains(t, string(log), s.Dir+ids, "the audit log directory")
	assert.Contains(t, string(log), s.config+group, "config.yaml gets the group of --user")
	assert.Contains(t, string(log), filepath.Join(s.Dir, "token")+group)
	assert.Contains(t, r.Stdout, s.config+": owner set")

	r = s.Run(env, args...)
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, ": unchanged")

	// Without a readable config the wrapper is installed, but the hook could not work.
	broken := append(args[:len(args)-1:len(args)-1], filepath.Join(s.Dir, "missing.yaml"))
	r = s.Run(env, broken...)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stdout, "missing.yaml: not read")
	assert.Contains(t, r.Stdout, "The hook cannot read its files")
	r = s.Run(env, args...)
	require.Equal(t, 0, r.Code, r.Stderr)

	// The installed wrapper is the hook: Gitaly runs it with the GL_* environment.
	s.Wrapper = wrapper
	s.Commit("change", map[string]string{"ci.yml": "y"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "GUARDPOLICY")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))

	other := filepath.Join(hooks, "pre-receive.d", "10-other")
	require.NoError(t, os.WriteFile(other, []byte("#!/bin/sh\n"), 0o600))
	r = s.Run(env, "uninstall-hook", "--hooks-dir", hooks)
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, wrapper+": removed")
	assert.NoFileExists(t, wrapper)
	assert.FileExists(t, other)
	r = s.Run(env, "uninstall-hook", "--hooks-dir", hooks)
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "not installed")
}

// breakglassStand is a project whose guarded change bob may not push.
func breakglassStand(t *testing.T) *Stand {
	t.Helper()
	s, _ := mainWith(t, map[string]string{"GUARDPOLICY": guardAll, "ci.yml": "x"})
	require.NoError(t, os.Mkdir(filepath.Join(s.Dir, "breakglass"), 0o755))
	s.Commit("change", map[string]string{"ci.yml": "y"})
	return s
}

// TestBreakglass_Valid: a grant lets the push through with a warning and an
// audit event; revoke ends it; the TTL is at most 24h.
func TestBreakglass_Valid(t *testing.T) {
	s := breakglassStand(t)
	grant := []string{"breakglass", "grant", "--config", s.config, "--project-id", "1", "--ttl", "30m", "--reason", "INC-123 policy lockout"}

	r := s.Run([]string{notRoot}, grant...)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "must run as root")
	r = s.Run(nil, "breakglass", "grant", "--config", s.config, "--project-id", "1", "--ttl", "25h", "--reason", "x")
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "at most 24h")
	r = s.Run(nil, "breakglass", "grant", "--config", s.config, "--project-id", "1", "--ttl", "1h")
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "reason")

	r = s.Run([]string{"SUDO_USER=operator-1"}, grant...)
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "project 1: break-glass active until")
	ev := Events(s.Audit(), "breakglass")
	require.Len(t, ev, 1)
	assert.Equal(t, "grant", ev[0]["action"])
	assert.Equal(t, "operator-1", ev[0]["username"])
	assert.Contains(t, ev[0]["reason"], "INC-123 policy lockout; operator operator-1; expires ")

	r = s.Run(nil, "breakglass", "list", "--config", s.config)
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "1: active: INC-123 policy lockout")

	res := s.Push(bob, "HEAD:refs/heads/main")
	assertAllowed(t, res)
	assert.Contains(t, res.Output, "break-glass is active for this project")
	ev = Events(s.Audit(), "breakglass")
	require.Len(t, ev, 1)
	assert.Equal(t, "allow", ev[0]["decision"])
	assert.Equal(t, "bob", ev[0]["username"])
	assert.Contains(t, ev[0]["reason"], "INC-123")

	r = s.Run(nil, "breakglass", "revoke", "--config", s.config, "--project-id", "1")
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "break-glass revoked")
	assert.Equal(t, "revoke", Events(s.Audit(), "breakglass")[0]["action"])
	s.Commit("change again", map[string]string{"ci.yml": "z"})
	assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "GUARDPOLICY")
}

// TestBreakglass_ByPath: --project looks the ID up; without an answer of
// GitLab the error points to --project-id.
func TestBreakglass_ByPath(t *testing.T) {
	s := breakglassStand(t)
	byPath := []string{"breakglass", "grant", "--config", s.config, "--project", "acme/platform/app", "--ttl", "30m", "--reason", "INC-7"}
	r := s.Run(nil, "breakglass", "grant", "--config", s.config, "--project", "acme/platform/app", "--project-id", "1", "--ttl", "30m", "--reason", "INC-7")
	assert.Equal(t, 2, r.Code, "one of --project and --project-id")

	s.API.Handle("GET", "/api/v4/projects/acme%2Fplatform%2Fapp", gitlabtest.Status(502))
	r = s.Run(nil, byPath...)
	assert.Equal(t, 1, r.Code)
	assert.Contains(t, r.Stderr, "give --project-id, the ID on the project overview page")

	s.API.Handle("GET", "/api/v4/projects/acme%2Fplatform%2Fapp", gitlabtest.OK(map[string]any{"id": 1, "path_with_namespace": "acme/platform/app"}))
	r = s.Run(nil, byPath...)
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "project 1 (acme/platform/app): break-glass active until")
	ev := Events(s.Audit(), "breakglass")
	require.Len(t, ev, 1)
	assert.Equal(t, "acme/platform/app", ev[0]["project_path"])
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))

	r = s.Run(nil, "breakglass", "revoke", "--config", s.config, "--project", "acme/platform/app")
	require.Equal(t, 0, r.Code, r.Stderr)
	assert.Contains(t, r.Stdout, "project 1 (acme/platform/app): break-glass revoked")
}

// TestBreakglass_ExpiredIgnored: an expired grant, one longer than 24h and one
// that others may write are ignored.
func TestBreakglass_ExpiredIgnored(t *testing.T) {
	now := time.Now().UTC()
	for name, tc := range map[string]struct {
		granted, expires time.Time
		mode             os.FileMode
	}{
		"expired":         {now.Add(-2 * time.Hour), now.Add(-time.Hour), 0o644},
		"longer than 24h": {now.Add(-time.Hour), now.Add(47 * time.Hour), 0o644},
		"world writable":  {now.Add(-time.Minute), now.Add(time.Hour), 0o666},
	} {
		t.Run(name, func(t *testing.T) {
			s := breakglassStand(t)
			data, err := json.Marshal(map[string]any{"project_id": 1, "granted_at": tc.granted, "expires_at": tc.expires,
				"reason": "old", "operator": "root"})
			require.NoError(t, err)
			p := filepath.Join(s.Dir, "breakglass", "1")
			require.NoError(t, os.WriteFile(p, data, 0o600))
			require.NoError(t, os.Chmod(p, tc.mode))

			assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), "GUARDPOLICY")
			assert.Empty(t, Events(s.Audit(), "breakglass"))
			r := s.Run(nil, "breakglass", "list", "--config", s.config)
			require.Equal(t, 0, r.Code, r.Stderr)
			assert.Regexp(t, `1: (expired|ignored)`, r.Stdout)
		})
	}
}
