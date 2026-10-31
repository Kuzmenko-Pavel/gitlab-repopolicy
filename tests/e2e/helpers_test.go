//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ciPolicy protects ci/ for alice, who is also the only admin.
const ciPolicy = `version: 1
admins: [alice]
rules:
  ci:
    match: all
    paths: ["ci/**"]
    allow: [alice]
    message: CI belongs to alice.
`

// ciIncomingPolicy is ciPolicy that counts every commit entering a branch, so
// that a merge of a branch already in the repository is checked.
var ciIncomingPolicy = strings.Replace(ciPolicy, "    match: all\n", "    match: all\n    check_history: incoming\n", 1)

// user returns a user created by the bootstrap.
func user(name string) *standUser { return gl.state.Users[name] }

// uniq makes a name unique on a stand that lives between runs.
func uniq(prefix string) string { return prefix + "-" + randomString(8) }

// project is a project created for one test.
type project struct {
	ID   int
	Path string
}

// newProject creates a private project with a README on main in acme/platform.
func newProject(t *testing.T, name string) *project {
	t.Helper()
	return createProject(t, name, true)
}

// newEmptyProject creates a project without commits.
func newEmptyProject(t *testing.T, name string) *project {
	t.Helper()
	return createProject(t, name, false)
}

func createProject(t *testing.T, name string, readme bool) *project {
	t.Helper()
	path := uniq(name)
	var p struct {
		ID   int    `json:"id"`
		Path string `json:"path_with_namespace"`
	}
	require.NoError(t, gl.root(t.Context(), http.MethodPost, "/projects", map[string]any{
		"name":                   path,
		"path":                   path,
		"namespace_id":           gl.state.Groups[subGroup],
		"visibility":             "private",
		"initialize_with_readme": readme,
		"wiki_access_level":      "enabled",
	}, &p))
	require.NoError(t, gl.waitMemberAccess(t.Context(), p.ID))
	return &project{ID: p.ID, Path: p.Path}
}

func (p *project) api(path string) string { return fmt.Sprintf("/projects/%d%s", p.ID, path) }

// newUser creates a user with an API token; it is deleted after the test.
func newUser(t *testing.T, prefix string) *standUser {
	t.Helper()
	ctx := t.Context()
	u := &standUser{Username: uniq(prefix)}
	var created apiUser
	require.NoError(t, gl.root(ctx, http.MethodPost, "/users", map[string]any{
		"username":          u.Username,
		"name":              u.Username,
		"email":             u.Username + "@e2e.invalid",
		"password":          randomString(24),
		"skip_confirmation": true,
	}, &created))
	u.ID = created.ID
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = gl.root(ctx, http.MethodDelete, fmt.Sprintf("/users/%d?hard_delete=true", u.ID), nil, nil)
	})
	require.NoError(t, gl.ensureUserToken(ctx, u, []string{"api", "write_repository"}))
	return u
}

// addMember adds u to the project and waits until GitLab grants the access.
func addMember(t *testing.T, p *project, u *standUser, access int) {
	t.Helper()
	require.NoError(t, gl.root(t.Context(), http.MethodPost, p.api("/members"),
		map[string]any{"user_id": u.ID, "access_level": access}, nil))
	waitUntil(t, 5*time.Minute, u.Username+" sees "+p.Path, func() bool {
		return gl.api(t.Context(), u.Token, http.MethodGet, p.api(""), nil, nil) == nil
	})
}

// createBranch creates branch from ref as u.
func createBranch(t *testing.T, u *standUser, p *project, branch, ref string) {
	t.Helper()
	require.NoError(t, gl.api(t.Context(), u.Token, http.MethodPost,
		p.api("/repository/branches?branch="+url.QueryEscape(branch)+"&ref="+url.QueryEscape(ref)), nil, nil))
}

// fileAction is one change of a commit made through the commits API.
type fileAction struct {
	Action  string `json:"action"`
	Path    string `json:"file_path"`
	Content string `json:"content,omitempty"`
}

func create(path, content string) fileAction { return fileAction{"create", path, content} }
func update(path, content string) fileAction { return fileAction{"update", path, content} }

// commit creates a commit through the commits API as u and returns its SHA;
// a non-empty start creates branch from it.
func commit(t *testing.T, u *standUser, p *project, branch, start, msg string, actions ...fileAction) (string, error) {
	t.Helper()
	body := map[string]any{"branch": branch, "commit_message": msg, "actions": actions}
	if start != "" {
		body["start_branch"] = start
	}
	var c struct {
		ID string `json:"id"`
	}
	err := gl.api(t.Context(), u.Token, http.MethodPost, p.api("/repository/commits"), body, &c)
	return c.ID, err
}

// mustCommit is commit that must succeed.
func mustCommit(t *testing.T, u *standUser, p *project, branch, start, msg string, actions ...fileAction) string {
	t.Helper()
	sha, err := commit(t, u, p, branch, start, msg, actions...)
	require.NoError(t, err)
	return sha
}

// requireHookDenied checks that the API refused an operation with the
// GL-HOOK-ERR line of the hook (FACTS: the message is the line without the prefix).
func requireHookDenied(t *testing.T, err error, want string) {
	t.Helper()
	var ae *apiError
	require.ErrorAs(t, err, &ae)
	require.Contains(t, ae.Body, want, "%s %s: %d", ae.Method, ae.Path, ae.Status)
}

func branchExists(t *testing.T, p *project, branch string) bool {
	t.Helper()
	err := gl.root(t.Context(), http.MethodGet, p.api("/repository/branches/"+url.PathEscape(branch)), nil, nil)
	if isStatus(err, http.StatusNotFound) {
		return false
	}
	require.NoError(t, err)
	return true
}

func tagExists(t *testing.T, p *project, tag string) bool {
	t.Helper()
	err := gl.root(t.Context(), http.MethodGet, p.api("/repository/tags/"+url.PathEscape(tag)), nil, nil)
	if isStatus(err, http.StatusNotFound) {
		return false
	}
	require.NoError(t, err)
	return true
}

// raw is a GET of the REST API that returns the body as text.
func (s *stand) raw(ctx context.Context, token, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/v4"+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", &apiError{Method: http.MethodGet, Path: path, Status: resp.StatusCode, Body: string(data)}
	}
	return string(data), nil
}

// graphql runs a query with the token of u and decodes data into out.
func graphql(t *testing.T, u *standUser, query string, vars map[string]any, out any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	require.NoError(t, err)
	postGraphQL(t, u, "application/json", bytes.NewReader(body), out)
}

func postGraphQL(t *testing.T, u *standUser, contentType string, body io.Reader, out any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, gl.baseURL+"/api/graphql", body)
	require.NoError(t, err)
	req.Header.Set("PRIVATE-TOKEN", u.Token)
	req.Header.Set("Content-Type", contentType)
	resp, err := gl.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(data))
	var r struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(data, &r))
	require.Empty(t, r.Errors, string(data))
	require.NoError(t, json.Unmarshal(r.Data, out))
}

// waitUntil polls cond until it holds or timeout passes.
func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			require.FailNow(t, "timed out waiting for "+what)
		}
		select {
		case <-t.Context().Done():
			require.FailNow(t, "test context done waiting for "+what)
		case <-time.After(2 * time.Second):
		}
	}
}

// containerExec runs a command in the GitLab container as the system user
// login, root when empty, and returns its combined output and exit code.
func (s *stand) containerExec(ctx context.Context, login string, stdin io.Reader, args ...string) (string, int, error) {
	a := []string{"compose", "-f", s.composeFile, "exec", "-T"}
	if login != "" {
		a = append(a, "-u", login)
	}
	a = append(a, "gitlab")
	cmd := exec.CommandContext(ctx, "docker", append(a, args...)...)
	cmd.Stdin = stdin
	out, err := cmd.CombinedOutput()
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		return string(out), ee.ExitCode(), nil
	}
	return string(out), 0, err
}

// inContainer runs a shell script as root in the GitLab container; it must succeed.
func inContainer(t *testing.T, script string) string {
	t.Helper()
	out, code, err := gl.containerExec(t.Context(), "", nil, "sh", "-c", script)
	require.NoError(t, err)
	require.Zero(t, code, out)
	return out
}

// auditEvent is the part of an audit event the tests check.
type auditEvent struct {
	Event       string `json:"event"`
	Module      string `json:"module"`
	Decision    string `json:"decision"`
	Enforced    bool   `json:"enforced"`
	Code        string `json:"code"`
	ProjectPath string `json:"project_path"`
	Username    string `json:"username"`
	GLID        string `json:"gl_id"`
	Protocol    string `json:"protocol"`
	Ref         string `json:"ref"`
	Signer      string `json:"signer"`
	Violations  []struct {
		Code   string `json:"code"`
		Reason string `json:"reason"`
	} `json:"violations"`
	DurationMS int64 `json:"duration_ms"`
}

// auditEvents returns the events of the audit log for a GL_PROJECT_PATH.
func auditEvents(t *testing.T, projectPath string) []auditEvent {
	t.Helper()
	out := inContainer(t, "cat "+auditLog+" 2>/dev/null || true")
	var events []auditEvent
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for sc.Scan() {
		var e auditEvent
		require.NoError(t, json.Unmarshal(sc.Bytes(), &e), sc.Text())
		if e.ProjectPath == projectPath {
			events = append(events, e)
		}
	}
	require.NoError(t, sc.Err())
	return events
}

// requireEvent waits for the event of a project that match accepts: some
// operations reach the hook from Sidekiq after the API call returns.
func requireEvent(t *testing.T, p *project, what string, match func(auditEvent) bool) auditEvent {
	t.Helper()
	var found *auditEvent
	waitUntil(t, 2*time.Minute, "audit event "+what, func() bool {
		for _, e := range auditEvents(t, p.Path) {
			if match(e) {
				found = &e
				return true
			}
		}
		return false
	})
	return *found
}

func decisionOn(ref string) func(auditEvent) bool {
	return func(e auditEvent) bool { return e.Event == "decision" && e.Ref == ref }
}

// gitClient is a working copy on the host for pushes to the stand.
type gitClient struct {
	t   *testing.T
	dir string
}

// clone clones a repository of the stand over HTTPS with token.
func clone(t *testing.T, token, repoPath string) *gitClient {
	t.Helper()
	g := &gitClient{t: t, dir: t.TempDir()}
	g.must("clone", "-q", httpsURL(token, repoPath), ".")
	return g
}

func httpsURL(token, repoPath string) string {
	return "https://oauth2:" + token + "@" + strings.TrimPrefix(gl.baseURL, "https://") + "/" + repoPath + ".git"
}

func sshURL(repoPath string) string {
	return "ssh://git@localhost:" + gl.sshPort + "/" + repoPath + ".git"
}

// sshCommand makes git use only key and the known_hosts file of the stand.
func sshCommand(key string) string {
	return strings.Join([]string{"ssh", "-F", "/dev/null", "-i", key, "-o", "IdentitiesOnly=yes",
		"-o", "UserKnownHostsFile=" + filepath.Join(gl.stateDir, "ssh", "known_hosts"),
		"-o", "GlobalKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=accept-new", "-p", gl.sshPort}, " ")
}

// run runs git in the working copy with an isolated configuration.
func (g *gitClient) run(env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(g.t.Context(), "git", append([]string{"-c", "http.sslCAInfo=" + filepath.Join(gl.stateDir, "tls", "ca.crt")}, args...)...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@e2e.invalid", "GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@e2e.invalid")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (g *gitClient) must(args ...string) string {
	g.t.Helper()
	out, err := g.run(nil, args...)
	require.NoError(g.t, err, out)
	return out
}

// commitFile writes a file and commits it.
func (g *gitClient) commitFile(path, content, msg string) {
	g.t.Helper()
	full := filepath.Join(g.dir, path)
	require.NoError(g.t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(g.t, os.WriteFile(full, []byte(content), 0o600))
	g.must("add", path)
	g.must("commit", "-q", "-m", msg)
}

// push pushes refspecs to url; key selects an SSH key for ssh URLs.
func (g *gitClient) push(remote, key string, refspecs ...string) (string, error) {
	var env []string
	if key != "" {
		env = append(env, "GIT_SSH_COMMAND="+sshCommand(key))
	}
	return g.run(env, append([]string{"push", remote}, refspecs...)...)
}

// requirePushDenied checks a push refused by the hook with the GL-HOOK-ERR line.
func requirePushDenied(t *testing.T, out string, err error, want string) {
	t.Helper()
	require.Error(t, err, out)
	require.Contains(t, out, "GL-HOOK-ERR: "+want)
	require.Contains(t, out, "(pre-receive hook declined)")
}

// mergeRequest is the part of a merge request the tests check.
type mergeRequest struct {
	IID                 int    `json:"iid"`
	State               string `json:"state"`
	MergeError          string `json:"merge_error"`
	DetailedMergeStatus string `json:"detailed_merge_status"`
	HeadPipeline        *struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
	} `json:"head_pipeline"`
}

func openMR(t *testing.T, u *standUser, p *project, source, target string) *mergeRequest {
	t.Helper()
	// The merge requests API can answer that a branch just created through the
	// commits API does not exist, even after the branches API returns it.
	var mr mergeRequest
	waitUntil(t, time.Minute, "merge request from "+source, func() bool {
		err := gl.api(t.Context(), u.Token, http.MethodPost, p.api("/merge_requests"), map[string]any{
			"source_branch": source, "target_branch": target, "title": "feat: merge " + source,
		}, &mr)
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusBadRequest && strings.Contains(ae.Body, "does not exist") {
			return false
		}
		require.NoError(t, err)
		return true
	})
	return &mr
}

func getMR(t *testing.T, p *project, iid int) *mergeRequest {
	t.Helper()
	var mr mergeRequest
	require.NoError(t, gl.root(t.Context(), http.MethodGet, p.api(fmt.Sprintf("/merge_requests/%d", iid)), nil, &mr))
	return &mr
}

func waitMergeable(t *testing.T, p *project, iid int) {
	t.Helper()
	waitUntil(t, 2*time.Minute, fmt.Sprintf("!%d mergeable", iid), func() bool {
		return getMR(t, p, iid).DetailedMergeStatus == "mergeable"
	})
}

// pipeline is a pipeline of a ref with the trace of its first job.
type pipeline struct {
	ID     int
	Status string
	Trace  string
}

// waitPipeline waits for the first pipeline of ref to finish.
func waitPipeline(t *testing.T, p *project, ref string) pipeline {
	t.Helper()
	var pl pipeline
	waitUntil(t, 5*time.Minute, "pipeline of "+ref, func() bool {
		var list []struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		}
		require.NoError(t, gl.root(t.Context(), http.MethodGet, p.api("/pipelines?ref="+url.QueryEscape(ref)), nil, &list))
		if len(list) == 0 {
			return false
		}
		pl.ID, pl.Status = list[len(list)-1].ID, list[len(list)-1].Status
		switch pl.Status {
		case "success", "failed", "canceled", "skipped":
			return true
		}
		return false
	})
	var jobs []struct {
		ID int `json:"id"`
	}
	require.NoError(t, gl.root(t.Context(), http.MethodGet, p.api(fmt.Sprintf("/pipelines/%d/jobs", pl.ID)), nil, &jobs))
	if len(jobs) == 0 {
		var detail map[string]any
		_ = gl.root(t.Context(), http.MethodGet, p.api(fmt.Sprintf("/pipelines/%d", pl.ID)), nil, &detail)
		require.FailNow(t, "pipeline without jobs", "%v", detail)
	}
	trace, err := gl.raw(t.Context(), gl.state.RootToken, p.api(fmt.Sprintf("/jobs/%d/trace", jobs[0].ID)))
	require.NoError(t, err)
	pl.Trace = trace
	return pl
}
