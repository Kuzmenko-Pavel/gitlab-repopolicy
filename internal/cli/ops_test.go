package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
)

// fakeSystem runs root commands as the test user: chown is recorded, not done.
type fakeSystem struct {
	notRoot bool
	state   string
	chowns  []string
}

func (s *fakeSystem) IsRoot() bool { return !s.notRoot }

// LookupUser: "self" is the test user, any other user an id after it, so
// that install-hook has owners to change.
func (s *fakeSystem) LookupUser(name string) (int, int, error) {
	switch name {
	case "no-such-user":
		return 0, 0, errors.New("unknown user " + name)
	case "self":
		return os.Getuid(), os.Getgid(), nil
	}
	return os.Getuid() + 1, os.Getgid() + 1, nil
}

func (s *fakeSystem) CurrentUser() string { return "root" }

func (s *fakeSystem) Chown(path string, uid, gid int) error {
	s.chowns = append(s.chowns, fmt.Sprintf("%s %d:%d", path, uid, gid))
	return nil
}

func (s *fakeSystem) TrustedUID() int { return os.Getuid() }

func (s *fakeSystem) StateDir() string { return s.state }

// stand is a server config with a token, an audit log, a break-glass
// directory and a fake GitLab, all in a temporary directory.
type stand struct {
	t      *testing.T
	api    *gitlabtest.Server
	sys    *fakeSystem
	dir    string
	config string
	token  string
	audit  string
}

func newStand(t *testing.T, extra string) *stand {
	t.Helper()
	dir := t.TempDir()
	s := &stand{t: t, api: gitlabtest.NewUnix(t), sys: &fakeSystem{state: filepath.Join(dir, "state")}, dir: dir,
		config: filepath.Join(dir, "config.yaml"), token: filepath.Join(dir, "token"), audit: filepath.Join(dir, "audit.log")}
	require.NoError(t, os.WriteFile(s.token, []byte("glpat-test\n"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "breakglass"), 0o755))
	s.writeConfig(extra)
	return s
}

func (s *stand) writeConfig(extra string) {
	body := fmt.Sprintf("gitlab_url: %s\ntoken_file: %s\naudit: {file: %s}\nbreakglass_dir: %s\n%s",
		s.api.URL(), s.token, s.audit, filepath.Join(s.dir, "breakglass"), extra)
	require.NoError(s.t, os.WriteFile(s.config, []byte(body), 0o600))
}

func (s *stand) run(env []string, args ...string) (code int, stdout, stderr string) {
	s.t.Helper()
	return s.runStdin("", env, args...)
}

func (s *stand) runStdin(stdin string, env []string, args ...string) (code int, stdout, stderr string) {
	s.t.Helper()
	var out, errOut bytes.Buffer
	app := App{Modules: modules.Default, System: s.sys}
	code = app.Run(context.Background(), args, env, Stdio{In: strings.NewReader(stdin), Out: &out, Err: &errOut})
	return code, out.String(), errOut.String()
}

// events returns the audit events of one kind.
func (s *stand) events(kind string) []map[string]any {
	s.t.Helper()
	f, err := os.Open(s.audit)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	require.NoError(s.t, err)
	defer func() { require.NoError(s.t, f.Close()) }()
	var events []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e map[string]any
		require.NoError(s.t, json.Unmarshal(sc.Bytes(), &e))
		if e["event"] == kind {
			events = append(events, e)
		}
	}
	require.NoError(s.t, sc.Err())
	return events
}

func TestInstallHook(t *testing.T) {
	s := newStand(t, "")
	hooks := filepath.Join(s.dir, "custom_hooks")
	require.NoError(t, os.Mkdir(hooks, 0o755))
	args := []string{"install-hook", "--hooks-dir", hooks, "--binary", "/usr/bin/grp", "--config", s.config}

	code, _, errOut := s.run(nil, "install-hook")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--hooks-dir is required")
	code, _, _ = s.run(nil, "install-hook", "extra")
	assert.Equal(t, ExitUsage, code)
	code, _, errOut = s.run(nil, "install-hook", "--hooks-dir", hooks, "--user", "no-such-user")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "--user: unknown user no-such-user")
	s.sys.notRoot = true
	code, _, errOut = s.run(nil, args...)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "must run as root")
	assert.NoDirExists(t, filepath.Join(hooks, "pre-receive.d"))
	s.sys.notRoot = false

	require.NoError(t, os.Chmod(s.dir, 0o700), "the umask does not decide the test")
	code, out, errOut := s.run(nil, args...)
	require.Equal(t, ExitOK, code, errOut)
	wrapper := filepath.Join(hooks, "pre-receive.d", "50-gitlab-repopolicy")
	assert.Contains(t, out, wrapper+": created\n")
	was := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	assert.Contains(t, out, fmt.Sprintf("%s: owner set to %d:%d (was %s)\n", s.config, os.Getuid(), os.Getgid()+1, was))
	assert.Contains(t, out, fmt.Sprintf("%s: owner set to %d:%d (was %s), mode unchanged (0700)\n", s.dir, os.Getuid()+1, os.Getgid()+1, was))
	assert.Contains(t, out, s.audit+": does not exist yet\n")
	assert.Contains(t, out, "Next: gitlab-repopolicy selftest\n")
	assert.Contains(t, s.sys.chowns, fmt.Sprintf("%s -1:%d", s.token, os.Getgid()+1), "the token gets the group of --user")
	data, err := os.ReadFile(filepath.Join(s.sys.state, HooksRecord))
	require.NoError(t, err)
	assert.Equal(t, hooks+"\n", string(data))

	// Owners that are right are not touched, a wide log is narrowed.
	require.NoError(t, os.Chmod(s.dir, 0o755))
	require.NoError(t, os.WriteFile(s.audit, nil, 0o644))
	s.sys.chowns = nil
	code, out, errOut = s.run(nil, append(args, "--user", "self")...)
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, wrapper+": unchanged\n")
	assert.Contains(t, out, s.config+": owner unchanged ("+was+")\n")
	assert.Contains(t, out, s.dir+": owner unchanged ("+was+"), mode set to 0750 (was 0755)\n")
	assert.Contains(t, out, s.audit+": owner unchanged ("+was+"), mode set to 0640 (was 0644)\n")
	info, err := os.Stat(s.audit)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o640), info.Mode().Perm())
	code, out, _ = s.run(nil, append(args, "--user", "self")...)
	require.Equal(t, ExitOK, code)
	assert.Contains(t, out, s.audit+": owner unchanged ("+was+"), mode unchanged (0640)\n")
	data, err = os.ReadFile(filepath.Join(s.sys.state, HooksRecord))
	require.NoError(t, err)
	assert.Equal(t, hooks+"\n", string(data), "a directory is recorded once")

	// The wrapper is installed, but without a readable config the hook could not work.
	missing := filepath.Join(s.dir, "missing.yaml")
	code, out, _ = s.run(nil, "install-hook", "--hooks-dir", hooks, "--config", missing)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, missing+": not read (")
	assert.Contains(t, out, "The hook cannot read its files")

	code, out, errOut = s.run(nil, "uninstall-hook", "--hooks-dir", hooks)
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, wrapper+": removed\n")
	data, err = os.ReadFile(filepath.Join(s.sys.state, HooksRecord))
	require.NoError(t, err)
	assert.Empty(t, string(data))
	code, out, _ = s.run(nil, "uninstall-hook", "--hooks-dir", hooks)
	require.Equal(t, ExitOK, code)
	assert.Equal(t, wrapper+": not installed\n", out)
}

// TestInstallHook_StateNotWritable: the hook works without the record, but
// removing the package will not remove it.
func TestInstallHook_StateNotWritable(t *testing.T) {
	s := newStand(t, "")
	hooks := filepath.Join(s.dir, "custom_hooks")
	require.NoError(t, os.Mkdir(hooks, 0o755))
	require.NoError(t, os.WriteFile(s.sys.state, nil, 0o600))
	code, out, errOut := s.run(nil, "install-hook", "--hooks-dir", hooks, "--config", s.config)
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, ": removing the package will not remove the hook\n")

	code, _, errOut = s.run(nil, "uninstall-hook", "--hooks-dir", hooks)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "gitlab-repopolicy uninstall-hook: ")
}

func TestUninstallHook_Usage(t *testing.T) {
	s := newStand(t, "")
	code, _, errOut := s.run(nil, "uninstall-hook")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--hooks-dir is required")
	code, _, _ = s.run(nil, "uninstall-hook", "-x")
	assert.Equal(t, ExitUsage, code)
	s.sys.notRoot = true
	code, _, errOut = s.run(nil, "uninstall-hook", "--hooks-dir", s.dir)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "must run as root")
	s.sys.notRoot = false
	code, _, errOut = s.run(nil, "uninstall-hook", "--hooks-dir", filepath.Join(s.dir, "token"))
	assert.Equal(t, ExitError, code, "the hooks directory is a file")
	assert.Contains(t, errOut, "gitlab-repopolicy uninstall-hook: ")
}

func TestBreakglass(t *testing.T) {
	s := newStand(t, "")
	grant := []string{"breakglass", "grant", "--config", s.config, "--project-id", "1", "--ttl", "30m", "--reason", "INC-123"}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"breakglass"}, "expected grant, revoke or list"},
		{[]string{"breakglass", "drop"}, "expected grant, revoke or list"},
		{[]string{"breakglass", "grant", "--config", s.config, "--ttl", "30m", "--reason", "x"}, "exactly one of --project and --project-id"},
		{[]string{"breakglass", "revoke", "--project", "a/b", "--project-id", "1"}, "exactly one of --project and --project-id"},
		{[]string{"breakglass", "list", "--ttl", "1h"}, "flag provided but not defined"},
	} {
		code, _, errOut := s.run(nil, tc.args...)
		assert.Equal(t, ExitUsage, code, "%q", tc.args)
		assert.Contains(t, errOut, tc.want, "%q", tc.args)
	}
	s.sys.notRoot = true
	code, _, errOut := s.run(nil, grant...)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "must run as root")
	s.sys.notRoot = false
	code, _, errOut = s.run(nil, "breakglass", "list", "--config", filepath.Join(s.dir, "missing.yaml"))
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "gitlab-repopolicy breakglass list: ")

	code, out, _ := s.run(nil, "breakglass", "list", "--config", s.config)
	require.Equal(t, ExitOK, code)
	assert.Equal(t, "no break-glass grants\n", out)
	code, out, _ = s.run(nil, "breakglass", "revoke", "--config", s.config, "--project-id", "1")
	require.Equal(t, ExitOK, code)
	assert.Equal(t, "project 1: no break-glass grant\n", out)
	assert.Empty(t, s.events("breakglass"), "nothing was revoked")

	code, _, errOut = s.run(nil, "breakglass", "grant", "--config", s.config, "--project-id", "1", "--ttl", "25h", "--reason", "x")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "at most 24h")

	code, out, errOut = s.run([]string{"SUDO_USER=operator-1"}, grant...)
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, "project 1: break-glass active until ")
	ev := s.events("breakglass")
	require.Len(t, ev, 1)
	assert.Equal(t, "grant", ev[0]["action"])
	assert.Equal(t, "operator-1", ev[0]["username"])
	assert.Contains(t, ev[0]["reason"], "INC-123; operator operator-1; expires ")

	code, out, _ = s.run(nil, "breakglass", "list", "--config", s.config)
	require.Equal(t, ExitOK, code)
	assert.Contains(t, out, "1: active: INC-123; operator operator-1")

	// The by-path lookup names the project in the output and the event.
	s.api.Handle("GET", "/api/v4/projects/acme%2Fapp", gitlabtest.Status(502))
	code, _, errOut = s.run(nil, "breakglass", "revoke", "--config", s.config, "--project", "acme/app")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "give --project-id, the ID on the project overview page")
	s.api.Handle("GET", "/api/v4/projects/acme%2Fapp", gitlabtest.OK(map[string]any{"id": 1, "path_with_namespace": "acme/app"}))
	code, out, errOut = s.run(nil, "breakglass", "revoke", "--config", s.config, "--project", "acme/app")
	require.Equal(t, ExitOK, code, errOut)
	assert.Equal(t, "project 1 (acme/app): break-glass revoked\n", out)
	ev = s.events("breakglass")
	require.Len(t, ev, 2)
	assert.Equal(t, "revoke", ev[1]["action"])
	assert.Equal(t, "acme/app", ev[1]["project_path"])
	assert.Equal(t, "root", ev[1]["username"], "without sudo the operator is the current user")

	// Grants that the hook ignores are listed with the reason.
	expired, err := json.Marshal(map[string]any{"project_id": 2, "granted_at": time.Now().Add(-2 * time.Hour).UTC(),
		"expires_at": time.Now().Add(-time.Hour).UTC(), "reason": "old", "operator": "root"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(s.dir, "breakglass", "2"), expired, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(s.dir, "breakglass", "3"), []byte("{"), 0o600))
	code, out, _ = s.run(nil, "breakglass", "list", "--config", s.config)
	require.Equal(t, ExitOK, code)
	assert.Contains(t, out, "2: expired: old")
	assert.Contains(t, out, "3: ignored: ")
}

// TestBreakglass_AuditNotWritable: a grant that cannot be audited fails.
func TestBreakglass_AuditNotWritable(t *testing.T) {
	s := newStand(t, "")
	require.NoError(t, os.Mkdir(s.audit, 0o700))
	code, _, errOut := s.run(nil, "breakglass", "grant", "--config", s.config, "--project-id", "1", "--ttl", "30m", "--reason", "x")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "gitlab-repopolicy breakglass grant: ")
}

// TestPrepareAuditFile: root creates a missing log with the owner of its directory.
func TestPrepareAuditFile(t *testing.T) {
	s := newStand(t, "")
	app := App{System: s.sys}
	require.NoError(t, app.prepareAuditFile(""))
	require.NoError(t, app.prepareAuditFile(s.audit))
	info, err := os.Stat(s.audit)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o640), info.Mode().Perm())
	assert.Equal(t, []string{fmt.Sprintf("%s %d:%d", s.audit, os.Getuid(), os.Getgid())}, s.sys.chowns)
	require.NoError(t, app.prepareAuditFile(s.audit), "an existing log is kept")
	assert.Len(t, s.sys.chowns, 1)
	assert.ErrorContains(t, app.prepareAuditFile(filepath.Join(s.dir, "missing", "audit.log")), "audit: ")

	s.sys.notRoot = true
	other := filepath.Join(s.dir, "other.log")
	require.NoError(t, app.prepareAuditFile(other))
	assert.NoFileExists(t, other, "an ordinary user creates the log by writing to it")
}

const rotatePath = "/api/v4/personal_access_tokens/self/rotate"

func tokenSelf(expires time.Time) map[string]any {
	return map[string]any{"id": 1, "scopes": []string{"self_rotate", "read_api"}, "active": true,
		"expires_at": expires.Format(time.DateOnly), "user_id": 7}
}

func TestTokenRotate(t *testing.T) {
	s := newStand(t, "")
	for _, args := range [][]string{{"token"}, {"token", "renew"}} {
		code, _, errOut := s.run(nil, args...)
		assert.Equal(t, ExitUsage, code)
		assert.Contains(t, errOut, "expected rotate")
	}
	code, _, _ := s.run(nil, "token", "rotate", "--before", "soon")
	assert.Equal(t, ExitUsage, code)
	s.sys.notRoot = true
	code, _, errOut := s.run(nil, "token", "rotate", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "must run as root")
	s.sys.notRoot = false
	code, _, errOut = s.run(nil, "token", "rotate", "--config", filepath.Join(s.dir, "missing.yaml"))
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "gitlab-repopolicy token rotate: ")

	s.api.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(tokenSelf(time.Now().AddDate(0, 0, 100))))
	code, out, errOut := s.run(nil, "token", "rotate", "--config", s.config)
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, "more than 30d from now: not rotated")
	assert.Zero(t, s.api.Calls("POST", rotatePath))

	s.api.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.OK(tokenSelf(time.Now().AddDate(0, 0, 10))))
	s.api.Handle("POST", rotatePath, gitlabtest.Response{Status: 400, Body: map[string]any{"message": "400 Bad request - Expiration date"}})
	code, _, errOut = s.run(nil, "token", "rotate", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "the current token stays valid")
	ev := s.events("internal_error")
	require.Len(t, ev, 1)
	assert.Equal(t, "token_rotate", ev[0]["action"])
	assert.Contains(t, ev[0]["reason"], "Expiration date")

	s.api.Handle("POST", rotatePath, gitlabtest.OK(map[string]any{"id": 2, "token": "glpat-rotated", "expires_at": "2027-10-13"}))
	code, out, errOut = s.run(nil, "token", "rotate", "--config", s.config, "--before", "30d")
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, "the token is rotated: it expires on ")
	assert.Contains(t, errOut, `level=INFO msg="token rotated"`)
	data, err := os.ReadFile(s.token)
	require.NoError(t, err)
	assert.Equal(t, "glpat-rotated\n", string(data))
	assert.NotEmpty(t, s.sys.chowns, "the new token file gets the owner of the old one")
}

// TestTokenRotate_AuditNotWritable: a failed rotation without audit is still exit code 1.
func TestTokenRotate_AuditNotWritable(t *testing.T) {
	s := newStand(t, "")
	require.NoError(t, os.Mkdir(s.audit, 0o700))
	s.api.Handle("GET", "/api/v4/personal_access_tokens/self", gitlabtest.Status(401))
	code, _, errOut := s.run(nil, "token", "rotate", "--config", s.config)
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, `level=ERROR msg=audit`)
}
