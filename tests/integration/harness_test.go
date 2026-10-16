//go:build integration

// Package integration pushes with a real git into a bare repository whose
// pre-receive hook runs the binary the way the install-hook wrapper does:
// git creates the quarantine, passes stdin and calls the hook.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab/gitlabtest"
)

// hookBin is the test hook binary built once by TestMain with -cover; workDir
// holds it and the fixtures of benchmarks and is removed after the run.
var hookBin, workDir string

func TestMain(m *testing.M) {
	if _, err := gitBin(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "repopolicy-it")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	workDir = dir
	hookBin = filepath.Join(dir, "testhook")
	build := exec.CommandContext(context.Background(), "go", "build", "-cover", "-covermode=atomic",
		"-coverpkg=github.com/Kuzmenko-Pavel/gitlab-repopolicy/...", "-o", hookBin, "./internal/testhook")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build test hook:", err)
		os.Exit(1)
	}
	code := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

// gitBin returns GIT_BIN: "git" for the git in PATH, otherwise an absolute
// path to a git executable; anything else stops the tests.
func gitBin() (string, error) {
	bin := os.Getenv("GIT_BIN")
	if bin == "" || bin == "git" {
		return "git", nil
	}
	if !filepath.IsAbs(bin) {
		return "", fmt.Errorf("GIT_BIN %q is not an absolute path", bin)
	}
	if info, err := os.Stat(bin); err != nil || info.IsDir() {
		return "", fmt.Errorf("GIT_BIN %q is not a file", bin)
	}
	return bin, nil
}

// gitExe is the git that pushes: exec looks a bare name up in the PATH of the
// test process, not in the PATH given to the child.
func gitExe(t testing.TB) string {
	t.Helper()
	bin, err := gitBin()
	require.NoError(t, err)
	if bin == "git" {
		bin, err = exec.LookPath("git")
		require.NoError(t, err)
	}
	return bin
}

// gitPath returns PATH with the directory of GIT_BIN first, for push and hook.
func gitPath() string {
	path := os.Getenv("PATH")
	if bin, err := gitBin(); err == nil && bin != "git" {
		path = filepath.Dir(bin) + string(filepath.ListSeparator) + path
	}
	return path
}

// User is a pusher as GitLab identifies it.
type User struct {
	Name string
	GLID string
}

var (
	alice = User{Name: "alice", GLID: "user-2"}
	bob   = User{Name: "bob", GLID: "user-3"}
)

// Stand is one bare repository with the hook, a work clone and a fake GitLab API.
type Stand struct {
	t       *testing.T
	Dir     string
	Bare    string
	Work    string
	API     *gitlabtest.Server
	audit   string
	config  string
	env     []string
	Project string
	Repo    string
	// HookPath replaces PATH of the hook process when set.
	HookPath string
	// Wrapper, when set, is run instead of the binary: the wrapper of install-hook.
	Wrapper  string
	coverDir string
}

// Option changes the stand before the first push.
type Option func(*Stand)

// WithMode sets mode in config.yaml.
func WithMode(mode string) Option {
	return func(s *Stand) { s.writeConfig(mode) }
}

// WithObjectFormat creates the repositories with the object format, for example sha256.
func WithObjectFormat(format string) Option {
	return func(s *Stand) {
		s.git(s.Dir, "init", "-q", "--bare", "-b", "main", "--object-format="+format, s.Bare)
		s.git(s.Dir, "init", "-q", "-b", "main", "--object-format="+format, s.Work)
	}
}

// NewStand creates an empty repository with the hook installed.
func NewStand(t *testing.T, opts ...Option) *Stand {
	t.Helper()
	dir := t.TempDir()
	s := &Stand{t: t, Dir: dir, Bare: filepath.Join(dir, "repo.git"), Work: filepath.Join(dir, "work"),
		audit: filepath.Join(dir, "audit.log"), config: filepath.Join(dir, "config.yaml"),
		API: gitlabtest.NewUnix(t), Project: "acme/platform/app", Repo: "project-1"}
	s.coverDir = os.Getenv("GOCOVERDIR")
	if s.coverDir == "" {
		s.coverDir = filepath.Join(dir, "cover")
		require.NoError(t, os.MkdirAll(s.coverDir, 0o755))
	}
	s.env = []string{"PATH=" + gitPath(), "HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=dev", "GIT_AUTHOR_EMAIL=dev@example.com", "GIT_COMMITTER_NAME=dev",
		"GIT_COMMITTER_EMAIL=dev@example.com", "GOCOVERDIR=" + s.coverDir, "LC_ALL=C"}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token"), []byte("glpat-test\n"), 0o600))
	// The service account is a Maintainer of the project unless a test says otherwise.
	s.API.Handle("GET", "/api/v4/projects/1", gitlabtest.OK(map[string]any{"permissions": map[string]any{
		"group_access": map[string]any{"access_level": 40}}}))
	s.writeConfig("enforce")
	for _, o := range opts {
		o(s)
	}
	if _, err := os.Stat(s.Bare); os.IsNotExist(err) {
		s.git(dir, "init", "-q", "--bare", "-b", "main", s.Bare)
		s.git(dir, "init", "-q", "-b", "main", s.Work)
	}
	return s
}

func (s *Stand) writeConfig(mode string) {
	cfg := fmt.Sprintf("mode: %s\ngitlab_url: %s\ntoken_file: %s\naudit:\n  file: %s\nbreakglass_dir: %s\n",
		mode, s.API.URL(), filepath.Join(s.Dir, "token"), s.audit, filepath.Join(s.Dir, "breakglass"))
	require.NoError(s.t, os.WriteFile(s.config, []byte(cfg), 0o600))
}

// AddConfig appends YAML to config.yaml, for sections such as settings_check.
func (s *Stand) AddConfig(yaml string) {
	s.t.Helper()
	f, err := os.OpenFile(s.config, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(s.t, err)
	_, err = f.WriteString(yaml)
	require.NoError(s.t, err)
	require.NoError(s.t, f.Close())
}

// git runs git with the stand environment and returns trimmed stdout.
func (s *Stand) git(dir string, args ...string) string {
	s.t.Helper()
	out, err := s.gitErr(dir, args...)
	require.NoError(s.t, err, "git %v: %s", args, out)
	return out
}

func (s *Stand) gitErr(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(s.t.Context(), gitExe(s.t), args...)
	cmd.Env, cmd.Dir = s.env, dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Commit writes files in the work clone ("" deletes a file) and commits them.
func (s *Stand) Commit(msg string, files map[string]string) string {
	s.t.Helper()
	// Deletions first, then writes in name order: a file may become a directory.
	names := slices.Sorted(maps.Keys(files))
	for _, name := range names {
		if files[name] == "" {
			require.NoError(s.t, os.RemoveAll(filepath.Join(s.Work, name)))
		}
	}
	for _, name := range names {
		if content := files[name]; content != "" {
			p := filepath.Join(s.Work, name)
			require.NoError(s.t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(s.t, os.WriteFile(p, []byte(content), 0o644))
		}
	}
	s.git(s.Work, "add", "-A")
	s.git(s.Work, "commit", "-q", "--allow-empty", "-m", msg)
	return s.Rev("HEAD")
}

// Rev resolves a revision in the work clone.
func (s *Stand) Rev(rev string) string { return s.git(s.Work, "rev-parse", rev) }

// RemoteRev resolves a ref of the server repository, "" when it does not exist.
func (s *Stand) RemoteRev(ref string) string {
	out, err := s.gitErr(s.Bare, "rev-parse", "--verify", "-q", ref)
	if err != nil {
		return ""
	}
	return out
}

// Result of a push.
type Result struct {
	OK     bool
	Output string
	// HookGit is "git --version" as the hook saw it.
	HookGit string
}

// Seed pushes without the hook to set up the state before the push under test.
func (s *Stand) Seed(refspecs ...string) {
	s.t.Helper()
	hook := filepath.Join(s.Bare, "hooks", "pre-receive")
	require.NoError(s.t, os.RemoveAll(hook))
	s.git(s.Work, append([]string{"push", "-q", "-f", s.Bare}, refspecs...)...)
}

// Push pushes as user through the hook; force pushes are allowed by git.
func (s *Stand) Push(user User, refspecs ...string) Result {
	s.t.Helper()
	versionFile := filepath.Join(s.Dir, "hook-git-version")
	path := ""
	if s.HookPath != "" {
		path = fmt.Sprintf("PATH=%q\nexport PATH\n", s.HookPath)
	}
	hook := fmt.Sprintf(`#!/bin/sh
git --version > %q
%sGL_ID=%q GL_USERNAME=%q GL_PROTOCOL=ssh GL_REPOSITORY=%q GL_PROJECT_PATH=%q
export GL_ID GL_USERNAME GL_PROTOCOL GL_REPOSITORY GL_PROJECT_PATH
%s
`, versionFile, path, user.GLID, user.Name, s.Repo, s.Project, s.hookExec())
	require.NoError(s.t, os.MkdirAll(filepath.Join(s.Bare, "hooks"), 0o755))
	require.NoError(s.t, os.WriteFile(filepath.Join(s.Bare, "hooks", "pre-receive"), []byte(hook), 0o755))
	out, err := s.gitErr(s.Work, append([]string{"push", "--porcelain", "-f", s.Bare}, refspecs...)...)
	version, rerr := os.ReadFile(versionFile)
	require.NoError(s.t, rerr, "the hook did not run: %s", out)
	require.NoError(s.t, os.Remove(versionFile))
	return Result{OK: err == nil, Output: out, HookGit: strings.TrimSpace(string(version))}
}

func (s *Stand) hookExec() string {
	if s.Wrapper != "" {
		return fmt.Sprintf("exec %q", s.Wrapper)
	}
	return fmt.Sprintf("exec %q hook pre-receive --config %q", hookBin, s.config)
}

// CLI is the outcome of a command of the binary.
type CLI struct {
	Code   int
	Stdout string
	Stderr string
}

// Run runs a command of the binary in the work clone with the stand
// environment and extra variables, such as REPOPOLICY_TEST_NOT_ROOT.
func (s *Stand) Run(extraEnv []string, args ...string) CLI {
	s.t.Helper()
	cmd := exec.CommandContext(s.t.Context(), hookBin, args...)
	cmd.Env, cmd.Dir = append(slices.Clone(s.env), extraEnv...), s.Work
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		require.ErrorAs(s.t, err, &exit, "%s", stderr.String())
		code = exit.ExitCode()
	}
	return CLI{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

// Event is an audit event as JSON.
type Event map[string]any

// Audit returns the audit events written so far and clears the log.
func (s *Stand) Audit() []Event {
	s.t.Helper()
	f, err := os.Open(s.audit)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(s.t, err)
	var events []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		require.NoError(s.t, json.Unmarshal(sc.Bytes(), &e))
		events = append(events, e)
	}
	require.NoError(s.t, f.Close())
	require.NoError(s.t, os.Remove(s.audit))
	return events
}

// Events filters events by name.
func Events(events []Event, name string) []Event {
	var out []Event
	for _, e := range events {
		if e["event"] == name {
			out = append(out, e)
		}
	}
	return out
}

// expectedGitVersion is "git --version" of GIT_BIN itself, not looked up in PATH.
func expectedGitVersion(t *testing.T) string {
	cmd := exec.CommandContext(t.Context(), gitExe(t), "--version")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	var out bytes.Buffer
	cmd.Stdout = &out
	require.NoError(t, cmd.Run())
	return strings.TrimSpace(out.String())
}

// Protected scripts the protected branches and tags of the project in the fake API.
func (s *Stand) Protected(branches []string, tags []string) {
	toNames := func(names []string) []map[string]any {
		out := make([]map[string]any, 0, len(names))
		for _, n := range names {
			out = append(out, map[string]any{"name": n})
		}
		return out
	}
	s.API.Handle("GET", "/api/v4/projects/1/protected_branches?per_page=100&page=1", gitlabtest.OK(toNames(branches)))
	s.API.Handle("GET", "/api/v4/projects/1/protected_tags?per_page=100&page=1", gitlabtest.OK(toNames(tags)))
}

// ProtectedCalls is the number of protected branch and tag requests.
func (s *Stand) ProtectedCalls() int {
	return s.API.Calls("GET", "/api/v4/projects/1/protected_branches?per_page=100&page=1") +
		s.API.Calls("GET", "/api/v4/projects/1/protected_tags?per_page=100&page=1")
}

// gitStdin runs git with stdin and returns trimmed stdout.
func (s *Stand) gitStdin(stdin, dir string, args ...string) string {
	s.t.Helper()
	cmd := exec.CommandContext(s.t.Context(), gitExe(s.t), args...)
	cmd.Env, cmd.Dir = s.env, dir
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	require.NoError(s.t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

var (
	owner   = User{Name: "owner", GLID: "user-5"}
	devLead = User{Name: "dev-lead", GLID: "user-6"}
)

// Team scripts the fake API with the users of the tests and their roles in
// the project: alice Maintainer, bob Developer, owner Owner, dev-lead Developer.
func (s *Stand) Team() {
	type member struct {
		id    int
		name  string
		level int
	}
	team := []member{{2, "alice", 40}, {3, "bob", 30}, {5, "owner", 50}, {6, "dev-lead", 30}}
	nodes := make([]any, 0, len(team))
	for _, m := range team {
		nodes = append(nodes, map[string]any{"id": fmt.Sprintf("gid://gitlab/User/%d", m.id), "username": m.name, "state": "active"})
		s.API.Handle("GET", fmt.Sprintf("/api/v4/projects/1/members/all/%d", m.id), gitlabtest.OK(map[string]any{"access_level": m.level, "state": "active"}))
	}
	s.API.Handle("POST", "/api/graphql", gitlabtest.OK(map[string]any{"data": map[string]any{"users": map[string]any{"nodes": nodes}}}))
	s.Protected([]string{"main", "release/*"}, nil)
}
