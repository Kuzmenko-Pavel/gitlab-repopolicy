//go:build e2e

package e2e

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GitLab operations other than git push: each
// operation reaches the hook with the identity of the person behind it.

// ciDenied is the GL-HOOK-ERR line of a change of ci/ by someone not in allow.
const ciDenied = "gitlab-repopolicy: you may not change ci/"

// withCIPolicy creates a project protected by ciPolicy.
func withCIPolicy(t *testing.T, name string) *project {
	t.Helper()
	p := newProject(t, name)
	mustCommit(t, user("alice"), p, "main", "", "chore: add path policy", create("PATHPOLICY", ciPolicy))
	return p
}

// TestOps_HTTPSPush: a user is matched by username
// in any case; bob is refused, alice is allowed.
func TestOps_HTTPSPush(t *testing.T) {
	t.Parallel()
	p := newProject(t, "https")
	mustCommit(t, user("alice"), p, "main", "", "chore: add path policy",
		create("PATHPOLICY", strings.Replace(ciPolicy, "allow: [alice]", "allow: [ALICE]", 1)))
	g := clone(t, user("bob").Token, p.Path)
	g.commitFile("ci/build.yml", "build", "ci: change the build")

	out, err := g.push(httpsURL(user("bob").Token, p.Path), "", "HEAD:refs/heads/bob-ci")
	requirePushDenied(t, out, err, ciDenied)
	assert.Contains(t, out, "may change them: ALICE")
	assert.False(t, branchExists(t, p, "bob-ci"))
	deny := requireEvent(t, p, "deny of bob", decisionOn("refs/heads/bob-ci"))
	assert.Equal(t, "deny", deny.Decision)
	assert.Equal(t, "protected_path_violation", deny.Code)
	assert.Equal(t, "bob", deny.Username)
	assert.Equal(t, "http", deny.Protocol)

	out, err = g.push(httpsURL(user("alice").Token, p.Path), "", "HEAD:refs/heads/alice-ci")
	require.NoError(t, err, out)
	allow := requireEvent(t, p, "allow of alice", decisionOn("refs/heads/alice-ci"))
	assert.Equal(t, "allow", allow.Decision)
	assert.Equal(t, "alice", allow.Username)
	assert.Equal(t, fmt.Sprintf("user-%d", user("alice").ID), allow.GLID)
	assert.Equal(t, "http", allow.Protocol)
	assert.True(t, allow.Enforced)
}
func TestOps_SSHPersonalKey(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "ssh")
	g := clone(t, user("alice").Token, p.Path)
	g.commitFile("ci/build.yml", "build", "ci: change the build")
	out, err := g.push(sshURL(p.Path), user("alice").SSHKey, "HEAD:refs/heads/alice-ssh")
	require.NoError(t, err, out)
	e := requireEvent(t, p, "allow over SSH", decisionOn("refs/heads/alice-ssh"))
	assert.Equal(t, "allow", e.Decision)
	assert.Equal(t, "alice", e.Username)
	assert.Equal(t, "ssh", e.Protocol)
	assert.True(t, strings.HasPrefix(e.GLID, "key-"), e.GLID)
}

// TestOps_SSHDeployKey: GL_USERNAME of a deploy key
// is the admin who created it, and the push is still refused.
func TestOps_SSHDeployKey(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "deploy")
	var keys []apiKey
	require.NoError(t, gl.root(t.Context(), http.MethodGet,
		fmt.Sprintf("/projects/%d/deploy_keys", gl.state.Projects[groupProject].ID), nil, &keys))
	require.Len(t, keys, 1)
	alice := user("alice")
	require.NoError(t, gl.api(t.Context(), alice.Token, http.MethodPost, p.api(fmt.Sprintf("/deploy_keys/%d/enable", keys[0].ID)), nil, nil))
	require.NoError(t, gl.api(t.Context(), alice.Token, http.MethodPut, p.api(fmt.Sprintf("/deploy_keys/%d", keys[0].ID)),
		map[string]any{"can_push": true}, nil))

	g := clone(t, alice.Token, p.Path)
	g.commitFile("ci/build.yml", "build", "ci: change the build")
	out, err := g.push(sshURL(p.Path), gl.state.Projects[groupProject].DeployKey, "HEAD:refs/heads/deploy-ci")
	requirePushDenied(t, out, err, "gitlab-repopolicy: ci/")
	require.Contains(t, out, "only people listed in PATHPOLICY rule ci may change it")
	assert.False(t, branchExists(t, p, "deploy-ci"))
	e := requireEvent(t, p, "deny of the deploy key", decisionOn("refs/heads/deploy-ci"))
	assert.Equal(t, "unknown_principal", e.Code)
	assert.Equal(t, "alice", e.Username)
	assert.Equal(t, fmt.Sprintf("key-%d", keys[0].ID), e.GLID)
	requireEvent(t, p, "unknown_principal event", func(e auditEvent) bool {
		return e.Event == "unknown_principal" && e.Ref == "refs/heads/deploy-ci"
	})
}

// TestOps_ProjectAccessToken: the bot of a project
// access token is allowed by its username.
func TestOps_ProjectAccessToken(t *testing.T) {
	t.Parallel()
	p := newProject(t, "bot")
	var token struct {
		Token  string `json:"token"`
		UserID int    `json:"user_id"`
	}
	require.NoError(t, gl.root(t.Context(), http.MethodPost, p.api("/access_tokens"), map[string]any{
		"name": "e2e", "scopes": []string{"api", "write_repository"}, "access_level": accessMaintainer, "expires_at": expiresAt(),
	}, &token))
	var bot apiUser
	require.NoError(t, gl.root(t.Context(), http.MethodGet, fmt.Sprintf("/users/%d", token.UserID), nil, &bot))
	waitUntil(t, time.Minute, "access of the bot", func() bool {
		return gl.api(t.Context(), token.Token, http.MethodGet, p.api(""), nil, nil) == nil
	})
	mustCommit(t, user("alice"), p, "main", "", "chore: add path policy",
		create("PATHPOLICY", strings.Replace(ciPolicy, "allow: [alice]", "allow: [alice, "+bot.Username+"]", 1)))

	g := clone(t, token.Token, p.Path)
	g.commitFile("ci/build.yml", "build", "ci: change the build")
	out, err := g.push(httpsURL(token.Token, p.Path), "", "HEAD:refs/heads/bot-ci")
	require.NoError(t, err, out)
	e := requireEvent(t, p, "allow of the bot", decisionOn("refs/heads/bot-ci"))
	assert.Equal(t, "allow", e.Decision)
	assert.Equal(t, bot.Username, e.Username)
}

// protectedRelease creates release/1, protected so that Developers may merge
// into it. The hook checks a protected ref by the net diff, so a merge needs
// the right of whoever presses merge; a merge into a
// feature branch brings no new commits and is not checked.
func protectedRelease(t *testing.T, p *project) string {
	t.Helper()
	createBranch(t, user("alice"), p, "release/1", "main")
	require.NoError(t, gl.root(t.Context(), http.MethodPost, p.api("/protected_branches"), map[string]any{
		"name": "release/*", "push_access_level": accessMaintainer, "merge_access_level": accessDeveloper,
	}, nil))
	return "release/1"
}

// TestOps_MergeButton: the hook sees who pressed
// merge, and GitLab shows its GL-HOOK-ERR line in the merge request.
func TestOps_MergeButton(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "merge")
	alice, bob := user("alice"), user("bob")
	release := protectedRelease(t, p)
	mustCommit(t, alice, p, "feature", "main", "ci: change the build", create("ci/build.yml", "build"))
	mr := openMR(t, alice, p, "feature", release)
	waitMergeable(t, p, mr.IID)

	err := gl.api(t.Context(), bob.Token, http.MethodPut, p.api(fmt.Sprintf("/merge_requests/%d/merge", mr.IID)), nil, nil)
	require.Error(t, err)
	// The refresh that Sidekiq runs after the push to feature clears merge_error
	// and may come late, so the message is taken from the answer that has it.
	var got *mergeRequest
	waitUntil(t, 3*time.Minute, "merge_error", func() bool { got = getMR(t, p, mr.IID); return got.MergeError != "" })
	assert.Equal(t, "opened", got.State)
	assert.Contains(t, got.MergeError, ciDenied)
	assert.NotContains(t, got.MergeError, "Allowed to change", "only GL-HOOK-ERR lines reach the UI")
	deny := requireEvent(t, p, "deny of bob", func(e auditEvent) bool { return e.Event == "decision" && e.Username == "bob" })
	assert.Equal(t, "refs/heads/"+release, deny.Ref)
	assert.Equal(t, "web", deny.Protocol)

	waitMergeable(t, p, mr.IID)
	require.NoError(t, gl.api(t.Context(), alice.Token, http.MethodPut, p.api(fmt.Sprintf("/merge_requests/%d/merge", mr.IID)), nil, nil))
	waitUntil(t, 3*time.Minute, "merge by alice", func() bool { return getMR(t, p, mr.IID).State == "merged" })
	allow := requireEvent(t, p, "allow of alice", func(e auditEvent) bool {
		return e.Event == "decision" && e.Ref == "refs/heads/"+release && e.Username == "alice"
	})
	assert.Equal(t, "allow", allow.Decision)
	assert.Equal(t, "web", allow.Protocol)
}

// sleepCI keeps a pipeline running long enough to set auto-merge.
const sleepCI = `wait:
  script:
    - sleep 20
`

// TestOps_AutoMerge: the merge made by Sidekiq after the
// pipeline carries the identity of who set auto-merge.
func TestOps_AutoMerge(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "automerge")
	alice, bob := user("alice"), user("bob")
	mustCommit(t, alice, p, "main", "", "ci: add the pipeline", create(".gitlab-ci.yml", sleepCI))
	release := protectedRelease(t, p)

	autoMerge := func(u *standUser, branch string) *mergeRequest {
		mustCommit(t, alice, p, branch, "main", "ci: change the build", create("ci/"+branch+".yml", "build"))
		mr := openMR(t, alice, p, branch, release)
		waitUntil(t, time.Minute, "head pipeline of "+branch, func() bool {
			hp := getMR(t, p, mr.IID).HeadPipeline
			return hp != nil && (hp.Status == "running" || hp.Status == "pending" || hp.Status == "created")
		})
		require.NoError(t, gl.api(t.Context(), u.Token, http.MethodPut, p.api(fmt.Sprintf("/merge_requests/%d/merge", mr.IID)),
			map[string]any{"auto_merge": true, "merge_when_pipeline_succeeds": true}, nil))
		return mr
	}

	mr := autoMerge(alice, "by-alice")
	waitUntil(t, 5*time.Minute, "auto-merge by alice", func() bool { return getMR(t, p, mr.IID).State == "merged" })
	e := requireEvent(t, p, "allow of the auto-merge", func(e auditEvent) bool {
		return e.Event == "decision" && e.Ref == "refs/heads/"+release && e.Username == "alice"
	})
	assert.Equal(t, "web", e.Protocol)

	mr = autoMerge(bob, "by-bob")
	e = requireEvent(t, p, "deny of the auto-merge by bob", func(e auditEvent) bool {
		return e.Event == "decision" && e.Ref == "refs/heads/"+release && e.Username == "bob"
	})
	assert.Equal(t, "deny", e.Decision)
	assert.Equal(t, "protected_path_violation", e.Code)
	assert.Equal(t, "opened", getMR(t, p, mr.IID).State)
}

// commitCreate is the GraphQL mutation the Web IDE commits with.
const commitCreate = `mutation($input: CommitCreateInput!) {
  commitCreate(input: $input) { errors commit { sha } }
}`

// TestOps_WebIDE: the Web IDE runs in a browser; its commit is
// the commitCreate mutation, which reaches Gitaly as UserCommitFiles.
func TestOps_WebIDE(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "webide")
	ideCommit := func(u *standUser, branch string) []string {
		var r struct {
			CommitCreate struct {
				Errors []string `json:"errors"`
			} `json:"commitCreate"`
		}
		graphql(t, u, commitCreate, map[string]any{"input": map[string]any{
			"projectPath": p.Path, "branch": branch, "startBranch": "main", "message": "ci: change in the Web IDE",
			"actions": []map[string]any{{"action": "CREATE", "filePath": "ci/ide.yml", "content": "ide"}},
		}}, &r)
		return r.CommitCreate.Errors
	}

	errs := ideCommit(user("bob"), "bob-ide")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, "\n"), ciDenied)
	assert.False(t, branchExists(t, p, "bob-ide"))
	require.Empty(t, ideCommit(user("alice"), "alice-ide"))
	e := requireEvent(t, p, "allow of alice", decisionOn("refs/heads/alice-ide"))
	assert.Equal(t, "allow", e.Decision)
	assert.Equal(t, "web", e.Protocol)
}
func TestOps_CommitsAPI(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "commits")
	_, err := commit(t, user("bob"), p, "bob-api", "main", "ci: change the build", create("ci/build.yml", "build"))
	requireHookDenied(t, err, ciDenied)
	assert.False(t, branchExists(t, p, "bob-api"))
	mustCommit(t, user("alice"), p, "alice-api", "main", "ci: change the build", create("ci/build.yml", "build"))
	e := requireEvent(t, p, "allow of alice", decisionOn("refs/heads/alice-api"))
	assert.Equal(t, "alice", e.Username)
	assert.Equal(t, "web", e.Protocol)
}

// TestOps_RevertUI: the revert button and the revert API run the same
// service and reach the hook as a web operation of the user who clicks. A
// revert in an ordinary branch brings back content the branch already had,
// so bob may revert a commit of alice there.
func TestOps_RevertUI(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "revert")
	sha := mustCommit(t, user("alice"), p, "feature", "main", "ci: change the build", create("ci/build.yml", "build"))
	require.NoError(t, gl.api(t.Context(), user("bob").Token, http.MethodPost, p.api("/repository/commits/"+sha+"/revert"),
		map[string]any{"branch": "feature"}, nil))
	e := requireEvent(t, p, "allow of the revert", func(e auditEvent) bool {
		return e.Event == "decision" && e.Ref == "refs/heads/feature" && e.Username == "bob" && e.Decision == "allow"
	})
	assert.Equal(t, "web", e.Protocol)
}

// jobTokenCI pushes a change of ci/ from a job with CI_JOB_TOKEN.
const jobTokenCI = `push:
  rules:
    - if: $CI_COMMIT_BRANCH =~ /^jt-/
  script:
    - git config user.email ci@e2e.invalid
    - git config user.name ci
    - mkdir -p ci && date +%s%N > ci/job.txt && git add ci/job.txt
    - 'git commit -q -m "ci: push from a job"'
    - git -c http.sslCAInfo=/etc/gitlab-stand/ca.crt push "https://gitlab-ci-token:${CI_JOB_TOKEN}@gitlab:PORT/${CI_PROJECT_PATH}.git" "HEAD:refs/heads/out-${CI_COMMIT_BRANCH}"
`

// TestOps_JobTokenPush: a CI_JOB_TOKEN push reaches the
// hook as the user who started the pipeline; when GitLab forbids it, the hook
// is not called.
func TestOps_JobTokenPush(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "jobtoken")
	alice := user("alice")
	port := strings.TrimPrefix(gl.baseURL, "https://localhost:")
	mustCommit(t, alice, p, "main", "", "ci: add the pipeline", create(".gitlab-ci.yml", strings.Replace(jobTokenCI, "PORT", port, 1)))
	allowPush := func(t *testing.T, on bool) {
		require.NoError(t, gl.root(t.Context(), http.MethodPut, p.api(""), map[string]any{"ci_push_repository_for_job_token_allowed": on}, nil))
	}
	// A new branch starts a pipeline as the user who created it.
	startPipeline := func(t *testing.T, u *standUser, branch string) pipeline {
		createBranch(t, u, p, branch, "main")
		return waitPipeline(t, p, branch)
	}

	t.Run("enabled", func(t *testing.T) {
		allowPush(t, true)
		pl := startPipeline(t, alice, "jt-alice")
		require.Equal(t, "success", pl.Status, pl.Trace)
		e := requireEvent(t, p, "allow of the job push", decisionOn("refs/heads/out-jt-alice"))
		assert.Equal(t, "allow", e.Decision)
		assert.Equal(t, "alice", e.Username)
		assert.Equal(t, "http", e.Protocol)

		pl = startPipeline(t, user("bob"), "jt-bob")
		require.Equal(t, "failed", pl.Status, pl.Trace)
		assert.Contains(t, pl.Trace, "GL-HOOK-ERR: "+ciDenied)
		e = requireEvent(t, p, "deny of the job push", decisionOn("refs/heads/out-jt-bob"))
		assert.Equal(t, "bob", e.Username)
	})
	t.Run("disabled", func(t *testing.T) {
		allowPush(t, false)
		pl := startPipeline(t, alice, "jt-off")
		require.Equal(t, "failed", pl.Status, pl.Trace)
		assert.NotContains(t, pl.Trace, "GL-HOOK-ERR")
		assert.False(t, branchExists(t, p, "out-jt-off"))
		for _, e := range auditEvents(t, p.Path) {
			assert.NotEqual(t, "refs/heads/out-jt-off", e.Ref, "the hook ran for a push GitLab forbids")
		}
	})
}

// TestOps_ServiceAccountSeesSubgroups: the
// service account is a Maintainer of the top-level group only.
func TestOps_ServiceAccountSeesSubgroups(t *testing.T) {
	t.Parallel()
	p := newProject(t, "visibility")
	require.NoError(t, gl.root(t.Context(), http.MethodPost, p.api("/protected_branches"),
		map[string]any{"name": "release/*", "push_access_level": accessMaintainer, "merge_access_level": accessMaintainer}, nil))
	sa := gl.state.ServiceAccount.Token
	t.Run("projects", func(t *testing.T) {
		var got struct {
			ID int `json:"id"`
		}
		require.NoError(t, gl.api(t.Context(), sa, http.MethodGet, "/projects/"+pathEscape(p.Path), nil, &got))
		assert.Equal(t, p.ID, got.ID)
	})
	t.Run("members", func(t *testing.T) {
		var members []struct {
			Username    string `json:"username"`
			AccessLevel int    `json:"access_level"`
		}
		require.NoError(t, gl.api(t.Context(), sa, http.MethodGet, p.api("/members/all?per_page=100"), nil, &members))
		levels := map[string]int{}
		for _, m := range members {
			levels[m.Username] = m.AccessLevel
		}
		assert.Equal(t, accessMaintainer, levels["alice"])
		assert.Equal(t, accessDeveloper, levels["bob"])
		assert.Equal(t, accessOwner, levels["olga"])
	})
	t.Run("protected branches", func(t *testing.T) {
		var branches []struct {
			Name string `json:"name"`
		}
		require.NoError(t, gl.api(t.Context(), sa, http.MethodGet, p.api("/protected_branches"), nil, &branches))
		names := make([]string, 0, len(branches))
		for _, b := range branches {
			names = append(names, b.Name)
		}
		assert.Contains(t, names, "release/*")
	})
}

// tagPolicy lets alice sign release tags with her SSH key.
func tagPolicy(t *testing.T) string {
	t.Helper()
	pub, err := os.ReadFile(user("alice").SSHKey + ".pub")
	require.NoError(t, err)
	return "version: 1\nrules:\n  release:\n    match: ['/^v\\d+\\.\\d+\\.\\d+$/']\n    signers: [alice]\n    message: Release tags are signed by alice.\nkeys:\n  alice: " +
		strings.TrimSpace(string(pub)) + "\n"
}

// pushSignedTag pushes a tag signed by alice with her SSH key.
func pushSignedTag(t *testing.T, p *project, tag string) {
	t.Helper()
	g := clone(t, user("alice").Token, p.Path)
	g.must("-c", "gpg.format=ssh", "-c", "user.signingkey="+user("alice").SSHKey, "tag", "-s", "-m", "release "+tag, tag)
	out, err := g.push(httpsURL(user("alice").Token, p.Path), "", "refs/tags/"+tag)
	require.NoError(t, err, out)
	e := requireEvent(t, p, "allow of the signed tag", func(e auditEvent) bool {
		return e.Event == "decision" && e.Ref == "refs/tags/"+tag && e.Decision == "allow"
	})
	assert.Equal(t, "tagpolicy", e.Module)
	assert.Equal(t, "alice", e.Signer)
}

const tagDenied = "gitlab-repopolicy: tags of this rule must be signed, and GitLab cannot sign a tag created in the UI or API"

// TestOps_TagViaUI: GitLab cannot sign a tag it creates, so a
// release tag from the UI is refused and the same tag signed by alice passes.
func TestOps_TagViaUI(t *testing.T) {
	t.Parallel()
	p := newProject(t, "tag-ui")
	alice := user("alice")
	mustCommit(t, alice, p, "main", "", "chore: add tag policy", create("TAGPOLICY", tagPolicy(t)))
	for _, body := range []map[string]any{
		{"tag_name": "v1.0.0", "ref": "main"},
		{"tag_name": "v1.0.1", "ref": "main", "message": "annotated in the UI"},
	} {
		err := gl.api(t.Context(), alice.Token, http.MethodPost, p.api("/repository/tags"), body, nil)
		requireHookDenied(t, err, tagDenied)
		assert.False(t, tagExists(t, p, body["tag_name"].(string)))
	}
	e := requireEvent(t, p, "deny of the UI tag", decisionOn("refs/tags/v1.0.0"))
	assert.Equal(t, "tag_not_signed", e.Code)
	assert.Equal(t, "web", e.Protocol)

	require.NoError(t, gl.api(t.Context(), alice.Token, http.MethodPost, p.api("/repository/tags"),
		map[string]any{"tag_name": "build-1", "ref": "main"}, nil), "a tag outside the rules is allowed")
	pushSignedTag(t, p, "v1.0.0")
}

// TestOps_TagViaReleasesAPI: a release that creates its tag is
// refused like a tag from the UI; a release of a signed tag is created.
func TestOps_TagViaReleasesAPI(t *testing.T) {
	t.Parallel()
	p := newProject(t, "tag-release")
	alice := user("alice")
	mustCommit(t, alice, p, "main", "", "chore: add tag policy", create("TAGPOLICY", tagPolicy(t)))
	release := func(tag string) error {
		return gl.api(t.Context(), alice.Token, http.MethodPost, p.api("/releases"),
			map[string]any{"tag_name": tag, "ref": "main", "name": tag, "description": "e2e"}, nil)
	}
	requireHookDenied(t, release("v2.0.0"), tagDenied)
	assert.False(t, tagExists(t, p, "v2.0.0"))
	e := requireEvent(t, p, "deny of the release tag", decisionOn("refs/tags/v2.0.0"))
	assert.Equal(t, "tag_not_signed", e.Code)

	pushSignedTag(t, p, "v2.0.1")
	require.NoError(t, release("v2.0.1"))
}

// TestOps_WikiAndDesignAllowed: wiki and design repositories are
// not projects, so a PATHPOLICY in a wiki means nothing and
// no audit event is written.
func TestOps_WikiAndDesignAllowed(t *testing.T) {
	t.Parallel()
	p := withCIPolicy(t, "wiki")
	alice, bob := user("alice"), user("bob")
	require.NoError(t, gl.api(t.Context(), alice.Token, http.MethodPost, p.api("/wikis"),
		map[string]any{"title": "home", "content": "e2e"}, nil))

	g := clone(t, bob.Token, p.Path+".wiki")
	g.commitFile("PATHPOLICY", "version: 1\nadmins: [nobody]\n", "a developer adds a policy file to the wiki")
	g.commitFile("ci/page.md", "page", "a developer changes a protected path of the project")
	out, err := g.push(httpsURL(bob.Token, p.Path+".wiki"), "", "HEAD")
	require.NoError(t, err, out)

	var issue struct {
		IID int `json:"iid"`
	}
	require.NoError(t, gl.api(t.Context(), alice.Token, http.MethodPost, p.api("/issues"), map[string]any{"title": "design"}, &issue))
	uploadDesign(t, bob, p, issue.IID)

	time.Sleep(2 * time.Second)
	assert.Empty(t, auditEvents(t, p.Path+".wiki"))
	assert.Empty(t, auditEvents(t, p.Path+".design"))
}

// uploadDesign uploads a PNG design to an issue: a write to the design repository.
func uploadDesign(t *testing.T, u *standUser, p *project, iid int) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	ops := fmt.Sprintf(`{"query":"mutation($files:[Upload!]!){designManagementUpload(input:{projectPath:\"%s\",iid:\"%d\",files:$files}){errors designs{filename}}}","variables":{"files":[null]}}`,
		p.Path, iid)
	require.NoError(t, w.WriteField("operations", ops))
	require.NoError(t, w.WriteField("map", `{"0":["variables.files.0"]}`))
	f, err := w.CreateFormFile("0", "design.png")
	require.NoError(t, err)
	png, err := os.ReadFile(filepath.Join("testdata", "design.png"))
	require.NoError(t, err)
	_, err = f.Write(png)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	var r struct {
		Upload struct {
			Errors  []string `json:"errors"`
			Designs []struct {
				Filename string `json:"filename"`
			} `json:"designs"`
		} `json:"designManagementUpload"`
	}
	postGraphQL(t, u, w.FormDataContentType(), &body, &r)
	require.Empty(t, r.Upload.Errors)
	require.Len(t, r.Upload.Designs, 1)
}

// TestOps_DefaultBranchSwitched: a Maintainer switches the default branch to
// a branch outside default_branch in the settings; the next push is rejected,
// because policies would be read from that branch.
func TestOps_DefaultBranchSwitched(t *testing.T) {
	t.Parallel()
	p := newProject(t, "default-switch")
	alice := user("alice")
	mustCommit(t, alice, p, "main", "", "feat: before the switch", create("a.txt", "1"))
	createBranch(t, alice, p, "legacy", "main")
	// Sidekiq handles the push of the README later: it points HEAD to the
	// default branch it reads, then protects that branch. A switch made before
	// it would be undone, so the test waits until main is protected.
	waitUntil(t, 3*time.Minute, "protection of main", func() bool {
		err := gl.root(t.Context(), http.MethodGet, p.api("/protected_branches/main"), nil, nil)
		if isStatus(err, http.StatusNotFound) {
			return false
		}
		require.NoError(t, err)
		return true
	})
	require.NoError(t, gl.root(t.Context(), http.MethodPut, p.api(""), map[string]any{"default_branch": "legacy"}, nil))
	var got struct {
		DefaultBranch string `json:"default_branch"`
	}
	require.NoError(t, gl.root(t.Context(), http.MethodGet, p.api(""), nil, &got))
	require.Equal(t, "legacy", got.DefaultBranch)
	sha, err := commit(t, alice, p, "main", "", "feat: after the switch", update("a.txt", "2"))
	if err == nil {
		// What the hook saw tells a HEAD that was not switched yet from a hook that did not run.
		h := fmt.Sprintf("%x", sha256.Sum256([]byte(strconv.Itoa(p.ID))))
		repo := "/var/opt/gitlab/git-data/repositories/@hashed/" + h[:2] + "/" + h[2:4] + "/" + h + ".git"
		t.Logf("commit %s entered main\nHEAD: %s\naudit: %+v\ngitaly:\n%s", sha, inContainer(t, "cat "+repo+"/HEAD"), auditEvents(t, p.Path),
			inContainer(t, "grep -E '"+p.Path+"|"+h+"' /var/log/gitlab/gitaly/current | grep -E 'PreReceiveHook|UserCommitFiles|WriteRef' || true"))
	}
	requireHookDenied(t, err, "the default branch legacy is not one of main")
	e := requireEvent(t, p, "deny of the switched default", func(e auditEvent) bool { return e.Event == "decision" && e.Decision == "deny" })
	assert.Equal(t, "default_branch_not_allowed", e.Code)
}

// TestOps_EmptyProjectFirstBranch: GitLab makes the first branch pushed into
// an empty project the default one, so with default_branch the first push
// must bring that branch; after it, other branches are pushed as usual.
func TestOps_EmptyProjectFirstBranch(t *testing.T) {
	t.Parallel()
	p := newEmptyProject(t, "empty-first")
	alice := user("alice")
	g := clone(t, alice.Token, p.Path)
	g.must("checkout", "-q", "-b", "dev")
	g.commitFile("a.txt", "1", "feat: first")
	out, err := g.push(httpsURL(alice.Token, p.Path), "", "HEAD:refs/heads/dev")
	requirePushDenied(t, out, err, "gitlab-repopolicy: the repository is empty: push main first")

	out, err = g.push(httpsURL(alice.Token, p.Path), "", "HEAD:refs/heads/main")
	require.NoError(t, err, out)
	out, err = g.push(httpsURL(alice.Token, p.Path), "", "HEAD:refs/heads/dev")
	require.NoError(t, err, out)
	var got struct {
		DefaultBranch string `json:"default_branch"`
	}
	require.NoError(t, gl.root(t.Context(), http.MethodGet, p.api(""), nil, &got))
	assert.Equal(t, "main", got.DefaultBranch)
}

// TestOps_BranchPolicy: a branch created in the GitLab UI or API goes through
// BRANCHPOLICY like a pushed one: a wrong name is denied, and a release branch
// is accepted only once the project protects release/*.
func TestOps_BranchPolicy(t *testing.T) {
	t.Parallel()
	p := newProject(t, "branch-standard")
	alice := user("alice")
	mustCommit(t, alice, p, "main", "", "feat: add the branch standard", create("BRANCHPOLICY", `version: 1
rules:
  release:
    match: ['/^release\/v[0-9]+\.[0-9]+\.[0-9]+$/']
    priority: 10
    protected: true
    message: Release branches look like release/v1.4.0.
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._\/-]+$/']
    priority: 20
    message: Work branches are <prefix>/<name>.
  other:
    match: all
    priority: 100
    deny: true
    message: Use release/vX.Y.Z or <prefix>/<name>.
`))
	newBranch := func(name string) error {
		return gl.api(t.Context(), alice.Token, http.MethodPost,
			p.api("/repository/branches?branch="+url.QueryEscape(name)+"&ref=main"), nil, nil)
	}
	requireHookDenied(t, newBranch("1-fix-login"), "branch 1-fix-login is forbidden by BRANCHPOLICY rule other")
	requireHookDenied(t, newBranch("release/v1.0.0"), "branch release/v1.0.0 must be protected")
	require.NoError(t, gl.root(t.Context(), http.MethodPost, p.api("/protected_branches"), map[string]any{
		"name": "release/*", "push_access_level": accessMaintainer, "merge_access_level": accessDeveloper,
	}, nil))
	require.NoError(t, newBranch("release/v1.0.0"))
	require.NoError(t, newBranch("alice/x"))
}
