//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Access levels of the GitLab API.
const (
	accessDeveloper  = 30
	accessMaintainer = 40
	accessOwner      = 50
)

const (
	rootTokenName  = "e2e-bootstrap"
	userTokenName  = "e2e"
	serviceAccount = "repopolicy-hook"
	topGroup       = "acme"
	subGroup       = "acme/platform"
	groupProject   = "acme/platform/app"
	personalProj   = "alice/scratch"
)

// Users of the stand and their role in the top-level group; carol is blocked,
// owen is not a member and becomes an Owner of single projects in tests.
var standUsers = []struct {
	username string
	access   int
	blocked  bool
}{
	{"alice", accessMaintainer, false},
	{"bob", accessDeveloper, false},
	{"carol", accessDeveloper, true},
	{"olga", accessOwner, false},
	{"owen", 0, false},
}

// bootstrap brings the stand to the state the tests expect. Every step checks
// what already exists, so a repeated run against a live stand is fast.
func (s *stand) bootstrap(ctx context.Context) error {
	steps := []struct {
		name string
		run  func(context.Context) error
	}{
		{"root token", s.ensureRootToken},
		{"application settings", s.ensureSettings},
		{"users", s.ensureUsers},
		{"service account", s.ensureServiceAccount},
		{"groups", s.ensureGroups},
		{"projects", s.ensureProjects},
		{"memberships", s.ensureMemberships},
		{"SSH keys", s.ensureSSHKeys},
		{"project access token", s.ensureProjectToken},
		{"runner", s.ensureRunner},
		{"hook", s.ensureHook},
	}
	for _, step := range steps {
		err := step.run(ctx)
		if saveErr := s.saveState(); saveErr != nil && err == nil {
			err = saveErr
		}
		if err != nil {
			return fmt.Errorf("bootstrap %s: %w", step.name, err)
		}
	}
	return nil
}

// rootTokenScript creates the administrator token with the value from E2E_ROOT_TOKEN.
const rootTokenScript = `
user = User.find_by_username!('root')
user.personal_access_tokens.active.where(name: '` + rootTokenName + `').each(&:revoke!)
attrs = { name: '` + rootTokenName + `', scopes: %w[api sudo], expires_at: 364.days.from_now }
attrs[:organization] = Organizations::Organization.first if PersonalAccessToken.column_names.include?('organization_id')
token = user.personal_access_tokens.build(attrs)
token.set_token(ENV.fetch('E2E_ROOT_TOKEN'))
token.save!
`

// ensureRootToken creates the administrator token through gitlab-rails runner;
// it is used only to prepare the stand.
func (s *stand) ensureRootToken(ctx context.Context) error {
	if s.tokenActive(ctx, s.state.RootToken) {
		return nil
	}
	token := "glpat-" + randomString(20)
	if _, err := s.compose(ctx, []string{"E2E_ROOT_TOKEN=" + token}, nil,
		"exec", "-T", "-e", "E2E_ROOT_TOKEN", "gitlab", "gitlab-rails", "runner", rootTokenScript); err != nil {
		return err
	}
	s.state.RootToken = token
	if !s.tokenActive(ctx, token) {
		return errors.New("the created root token is not accepted by the API")
	}
	return nil
}

// ensureSettings turns off outgoing traffic and self sign-up.
func (s *stand) ensureSettings(ctx context.Context) error {
	return s.root(ctx, http.MethodPut, "/application/settings", map[string]any{
		"version_check_enabled": false,
		"usage_ping_enabled":    false,
		"signup_enabled":        false,
	}, nil)
}

type apiUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	State    string `json:"state"`
}

func (s *stand) findUser(ctx context.Context, username string) (*apiUser, error) {
	var users []apiUser
	if err := s.root(ctx, http.MethodGet, "/users?username="+username, nil, &users); err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].Username == username {
			return &users[i], nil
		}
	}
	return nil, nil
}

func (s *stand) ensureUsers(ctx context.Context) error {
	if s.state.Users == nil {
		s.state.Users = map[string]*standUser{}
	}
	for _, u := range standUsers {
		su := s.state.Users[u.username]
		if su == nil {
			su = &standUser{Username: u.username}
			s.state.Users[u.username] = su
		}
		found, err := s.findUser(ctx, u.username)
		if err != nil {
			return err
		}
		if found == nil {
			su.Password = randomString(24)
			var created apiUser
			if err := s.root(ctx, http.MethodPost, "/users", map[string]any{
				"username":          u.username,
				"name":              u.username,
				"email":             u.username + "@e2e.invalid",
				"password":          su.Password,
				"skip_confirmation": true,
			}, &created); err != nil {
				return err
			}
			found = &created
		}
		su.ID = found.ID
		if u.blocked {
			if found.State != "blocked" {
				if err := s.root(ctx, http.MethodPost, fmt.Sprintf("/users/%d/block", su.ID), nil, nil); err != nil {
					return err
				}
			}
			continue
		}
		if err := s.ensureUserToken(ctx, su, []string{"api", "write_repository"}); err != nil {
			return err
		}
	}
	return nil
}

// ensureUserToken issues a personal access token for the user as administrator.
func (s *stand) ensureUserToken(ctx context.Context, u *standUser, scopes []string) error {
	if s.tokenActive(ctx, u.Token) {
		return nil
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := s.root(ctx, http.MethodPost, fmt.Sprintf("/users/%d/personal_access_tokens", u.ID), map[string]any{
		"name":       userTokenName,
		"scopes":     scopes,
		"expires_at": expiresAt(),
	}, &created); err != nil {
		return err
	}
	u.Token = created.Token
	return nil
}

// ensureServiceAccount creates the service account of the hook:
// an instance service account with a read_api + self_rotate token.
func (s *stand) ensureServiceAccount(ctx context.Context) error {
	sa := s.state.ServiceAccount
	if sa == nil {
		sa = &standUser{Username: serviceAccount}
		s.state.ServiceAccount = sa
	}
	found, err := s.findUser(ctx, serviceAccount)
	if err != nil {
		return err
	}
	if found == nil {
		var created apiUser
		if err := s.root(ctx, http.MethodPost, "/service_accounts", map[string]any{
			"name":     "gitlab-repopolicy hook",
			"username": serviceAccount,
		}, &created); err != nil {
			return err
		}
		found = &created
	}
	sa.ID = found.ID
	return s.ensureUserToken(ctx, sa, []string{"read_api", "self_rotate"})
}

type apiNamespaced struct {
	ID int `json:"id"`
}

func (s *stand) ensureGroups(ctx context.Context) error {
	if s.state.Groups == nil {
		s.state.Groups = map[string]int{}
	}
	for _, path := range []string{topGroup, subGroup} {
		var g apiNamespaced
		err := s.root(ctx, http.MethodGet, "/groups/"+pathEscape(path), nil, &g)
		if isStatus(err, http.StatusNotFound) {
			body := map[string]any{"name": filepath.Base(path), "path": filepath.Base(path), "visibility": "private"}
			if parent := filepath.Dir(path); parent != "." {
				body["parent_id"] = s.state.Groups[parent]
			}
			err = s.root(ctx, http.MethodPost, "/groups", body, &g)
		}
		if err != nil {
			return err
		}
		s.state.Groups[path] = g.ID
	}
	return nil
}

func (s *stand) ensureProjects(ctx context.Context) error {
	if s.state.Projects == nil {
		s.state.Projects = map[string]*standProject{}
	}
	for _, path := range []string{groupProject, personalProj} {
		sp := s.state.Projects[path]
		if sp == nil {
			sp = &standProject{Path: path}
			s.state.Projects[path] = sp
		}
		var p apiNamespaced
		err := s.root(ctx, http.MethodGet, "/projects/"+pathEscape(path), nil, &p)
		if isStatus(err, http.StatusNotFound) {
			body := map[string]any{
				"name":                   filepath.Base(path),
				"path":                   filepath.Base(path),
				"visibility":             "private",
				"initialize_with_readme": true,
				"wiki_access_level":      "enabled",
			}
			owner := filepath.Dir(path)
			if id, ok := s.state.Groups[owner]; ok {
				body["namespace_id"] = id
				err = s.root(ctx, http.MethodPost, "/projects", body, &p)
			} else {
				err = s.root(ctx, http.MethodPost, fmt.Sprintf("/projects/user/%d", s.state.Users[owner].ID), body, &p)
			}
		}
		if err != nil {
			return err
		}
		sp.ID = p.ID
	}
	return nil
}

// ensureMemberships gives users and the service account their roles in the top-level group.
func (s *stand) ensureMemberships(ctx context.Context) error {
	members := map[int]int{s.state.ServiceAccount.ID: accessMaintainer}
	for _, u := range standUsers {
		if u.access > 0 {
			members[s.state.Users[u.username].ID] = u.access
		}
	}
	group := s.state.Groups[topGroup]
	for userID, access := range members {
		var m struct {
			AccessLevel int `json:"access_level"`
		}
		path := fmt.Sprintf("/groups/%d/members/%d", group, userID)
		err := s.root(ctx, http.MethodGet, path, nil, &m)
		switch {
		case isStatus(err, http.StatusNotFound):
			err = s.root(ctx, http.MethodPost, fmt.Sprintf("/groups/%d/members", group),
				map[string]any{"user_id": userID, "access_level": access}, nil)
		case err == nil && m.AccessLevel != access:
			err = s.root(ctx, http.MethodPut, path, map[string]any{"access_level": access}, nil)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

type apiKey struct {
	ID  int    `json:"id"`
	Key string `json:"key"`
}

// ensureSSHKeys registers a personal key of alice and a deploy key with push access to the group project.
func (s *stand) ensureSSHKeys(ctx context.Context) error {
	alice := s.state.Users["alice"]
	pub, priv, err := s.sshKey(ctx, "alice")
	if err != nil {
		return err
	}
	alice.SSHKey = priv
	var keys []apiKey
	if err := s.root(ctx, http.MethodGet, fmt.Sprintf("/users/%d/keys", alice.ID), nil, &keys); err != nil {
		return err
	}
	if !hasKey(keys, pub) {
		if err := s.root(ctx, http.MethodPost, fmt.Sprintf("/users/%d/keys", alice.ID),
			map[string]any{"title": "e2e", "key": pub}, nil); err != nil {
			return err
		}
	}

	project := s.state.Projects[groupProject]
	pub, priv, err = s.sshKey(ctx, "deploy")
	if err != nil {
		return err
	}
	project.DeployKey = priv
	keys = nil
	if err := s.root(ctx, http.MethodGet, fmt.Sprintf("/projects/%d/deploy_keys", project.ID), nil, &keys); err != nil {
		return err
	}
	if hasKey(keys, pub) {
		return nil
	}
	// The deploy key is created by alice: GL_USERNAME of a deploy key push is its creator.
	return s.api(ctx, alice.Token, http.MethodPost, fmt.Sprintf("/projects/%d/deploy_keys", project.ID),
		map[string]any{"title": "e2e deploy", "key": pub, "can_push": true}, nil)
}

// sshKey returns the public key and the private key path of an ed25519 key pair in the state directory.
func (s *stand) sshKey(ctx context.Context, name string) (string, string, error) {
	priv := filepath.Join(s.stateDir, "ssh", name)
	if _, err := os.Stat(priv); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(priv), 0o700); err != nil {
			return "", "", err
		}
		out, err := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", name+"@e2e", "-f", priv).CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("ssh-keygen: %w: %s", err, out)
		}
	}
	pub, err := os.ReadFile(priv + ".pub")
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(pub)), priv, nil
}

// hasKey compares keys by type and body, without the comment.
func hasKey(keys []apiKey, pub string) bool {
	want := strings.Fields(pub)[:2]
	for _, k := range keys {
		f := strings.Fields(k.Key)
		if len(f) >= 2 && f[0] == want[0] && f[1] == want[1] {
			return true
		}
	}
	return false
}

// ensureProjectToken issues a project access token with push access to the group project.
func (s *stand) ensureProjectToken(ctx context.Context) error {
	project := s.state.Projects[groupProject]
	if s.tokenActive(ctx, project.AccessToken) {
		return nil
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := s.root(ctx, http.MethodPost, fmt.Sprintf("/projects/%d/access_tokens", project.ID), map[string]any{
		"name":         "e2e",
		"scopes":       []string{"api", "write_repository"},
		"access_level": accessMaintainer,
		"expires_at":   expiresAt(),
	}, &created); err != nil {
		return err
	}
	project.AccessToken = created.Token
	return nil
}

// ensureRunner registers the runner container as an instance runner with the shell executor.
func (s *stand) ensureRunner(ctx context.Context) error {
	cfg, err := s.compose(ctx, nil, nil, "exec", "-T", "runner", "cat", "/etc/gitlab-runner/config.toml")
	if err == nil && s.state.RunnerToken != "" && strings.Contains(cfg, s.state.RunnerToken) {
		return nil
	}
	if _, err := s.compose(ctx, nil, nil, "exec", "-T", "runner", "gitlab-runner", "unregister", "--all-runners"); err != nil {
		return err
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := s.root(ctx, http.MethodPost, "/user/runners", map[string]any{
		"runner_type":  "instance_type",
		"description":  "e2e shell",
		"run_untagged": true,
	}, &created); err != nil {
		return err
	}
	// Inside the compose network GitLab is https://gitlab:<port>; the certificate covers this name.
	internal := strings.Replace(s.baseURL, "localhost", "gitlab", 1)
	if _, err := s.compose(ctx, []string{"E2E_RUNNER_TOKEN=" + created.Token}, nil,
		"exec", "-T", "-e", "E2E_RUNNER_TOKEN", "runner", "sh", "-c",
		`gitlab-runner register --non-interactive --executor shell --name e2e-shell `+
			`--url "$0" --clone-url "$0" --tls-ca-file /etc/gitlab-stand/ca.crt --token "$E2E_RUNNER_TOKEN"`,
		internal); err != nil {
		return err
	}
	s.state.RunnerToken = created.Token
	return nil
}

// Paths of the hook installation in the GitLab container.
const (
	hooksDir   = "/var/opt/gitlab/gitaly/custom_hooks"
	hookConfig = "/etc/gitlab-repopolicy/config.yaml"
	hookToken  = "/etc/gitlab-repopolicy/token"
	auditLog   = "/var/log/gitlab-repopolicy/audit.log"
)

// hookFilesScript lays out the files as the package and an administrator do:
// owned by root, then install-hook gives the git user what the hook reads.
const hookFilesScript = `set -eu
umask 027
mkdir -p /etc/gitlab-repopolicy /var/log/gitlab-repopolicy
chmod 755 /etc/gitlab-repopolicy
chmod 750 /var/log/gitlab-repopolicy
cat > ` + hookConfig + `
printf '%s' "$E2E_HOOK_TOKEN" > ` + hookToken + `
chown root:root ` + hookConfig + ` ` + hookToken + ` /var/log/gitlab-repopolicy
`

// ensureHook writes the server config and the service account token, then
// installs the hook with install-hook as in production.
func (s *stand) ensureHook(ctx context.Context) error {
	cfg, err := os.ReadFile(filepath.Join("testdata", "config.yaml"))
	if err != nil {
		return err
	}
	if _, err := s.compose(ctx, []string{"E2E_HOOK_TOKEN=" + s.state.ServiceAccount.Token}, bytes.NewReader(cfg),
		"exec", "-T", "-e", "E2E_HOOK_TOKEN", "gitlab", "sh", "-c", hookFilesScript); err != nil {
		return err
	}
	_, err = s.compose(ctx, nil, nil, "exec", "-T", "gitlab",
		"gitlab-repopolicy", "install-hook", "--hooks-dir", hooksDir, "--user", "git")
	return err
}

func expiresAt() string { return time.Now().AddDate(0, 0, 300).Format(time.DateOnly) }

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)[:n]
}
