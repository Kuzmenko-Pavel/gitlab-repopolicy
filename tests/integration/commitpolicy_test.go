//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	commitPolicy = `version: 1
rulesets:
  - name: conventional
    except: {merge_commits: true, git_reverts: true}
    rules:
      type-enum: {level: error, value: [feat, fix, chore]}
      subject-full-stop: {level: error, value: forbid}
`
	commitWarn      = "version: 1\nmode: warn\nrulesets:\n  - name: conventional\n    rules:\n      type-enum: {level: error, value: [feat, fix]}\n"
	commitWeak      = "version: 1\nrulesets:\n  - name: conventional\n    format: free\n    rules: {}\n"
	commitProtected = "version: 1\nrulesets:\n  - name: protected\n    refs: protected\n    rules:\n      type-enum: {level: error, value: [feat, fix]}\n"
	commitMerged    = "version: 1\nrulesets:\n  - name: all\n    refs: [\"refs/heads/*\"]\n    except: {merge_commits: true, merged_commits: true}\n    rules:\n      type-enum: {level: error, value: [feat, fix]}\n"
	commitRelease   = "version: 1\nrulesets:\n  - name: release\n    refs: [\"refs/heads/release/*\"]\n    rules:\n      type-enum: {level: error, value: [fix]}\n"
)

func TestCommitPolicy_Enforce(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("feat: good", map[string]string{"a": "1"})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	s.Commit("WIP stuff.", map[string]string{"a": "2"})
	r := s.Push(alice, "HEAD:refs/heads/main")
	assertDenied(t, r, "COMMITPOLICY [conventional] refs/heads/main: commit message format", `"WIP stuff."`,
		`header: expected "type(scope): subject"`, "fix: git rebase -i and reword")
	ev := Events(s.Audit(), "decision")
	require.NotEmpty(t, ev)
	last := ev[len(ev)-1]
	assert.Equal(t, "commitpolicy", last["module"])
	assert.Equal(t, "commit_format_violation", last["code"])
}

func TestCommitPolicy_InvalidNewVersionOnFeatureBranch(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("feat: typo in the policy", map[string]string{"COMMITPOLICY": strings.Replace(commitPolicy, "type-enum", "type-enun", 1)})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "COMMITPOLICY refs/heads/feature: new policy version is invalid",
		`unknown rule "type-enun"`)
}

func TestCommitPolicy_WeakenedInSamePush(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("anything goes now", map[string]string{"COMMITPOLICY": commitWeak})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/main"), `"anything goes now"`)
}

func TestCommitPolicy_DeletionChecksWithOldPolicy(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("remove the policy", map[string]string{"COMMITPOLICY": ""})
	r := s.Push(alice, "HEAD:refs/heads/main")
	assertDenied(t, r, `"remove the policy"`)
	s.git(s.Work, "commit", "-q", "--amend", "-m", "chore: remove the policy")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	ev := Events(s.Audit(), "policy_change")
	require.NotEmpty(t, ev)
	assert.Equal(t, "delete", ev[len(ev)-1]["action"])
}

// mergeBranch creates a feature branch with the commits and merges it into
// main with a merge commit, like the merge button of GitLab.
func mergeBranch(s *Stand, messages ...string) {
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	for i, m := range messages {
		s.Commit(m, map[string]string{fmt.Sprintf("f%d", i): m})
	}
	s.Seed("HEAD:refs/heads/feature")
	s.git(s.Work, "checkout", "-q", "main")
	s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature' into 'main'", "feature")
}

func TestCommitPolicy_MergeWithoutSquash(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitPolicy})
	mergeBranch(s, "feat: good", "bad message")
	r := s.Push(alice, "HEAD:refs/heads/main")
	assertDenied(t, r, `"bad message"`)
	assert.NotContains(t, r.Output, "Merge branch", "the merge commit is excepted")
}

func TestCommitPolicy_MergedCommits(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitMerged})
	mergeBranch(s, "feat: good", "bad message")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	s.Commit("direct bad message", map[string]string{"x": "1"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/main"), `"direct bad message"`)
	s.git(s.Work, "reset", "-q", "--hard", "HEAD~1")
	s.git(s.Work, "checkout", "-q", "-b", "topic")
	s.Commit("bad on a branch", map[string]string{"y": "1"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/topic"), `"bad on a branch"`)
}

func TestCommitPolicy_SquashMerge(t *testing.T) {
	s, base := mainWith(t, map[string]string{"COMMITPOLICY": commitPolicy})
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("wip 1", map[string]string{"a": "1"})
	s.Commit("wip 2", map[string]string{"a": "2"})
	s.git(s.Work, "checkout", "-q", "main")
	s.git(s.Work, "merge", "-q", "--squash", "feature")
	s.git(s.Work, "commit", "-q", "-m", "feat: squashed work (!1)")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	assert.NotEqual(t, base, s.RemoteRev("refs/heads/main"))
}

func TestCommitPolicy_CreateProtectedFromFeature(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitRelease})
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("feat: not allowed on release", map[string]string{"a": "1"})
	s.Seed("HEAD:refs/heads/feature")
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature2"))
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/release/1"), "COMMITPOLICY [release] refs/heads/release/1", `type-enum: "feat"`)
}

func TestCommitPolicy_WarnVsEnforce(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitWarn})
	s.Commit("chore: tidy", map[string]string{"a": "1"})
	r := s.Push(alice, "HEAD:refs/heads/main")
	assertAllowed(t, r)
	assert.Contains(t, r.Output, "gitlab-repopolicy: warning (COMMITPOLICY: 1 violation)")
	assert.Contains(t, r.Output, `type-enum: "chore" is not one of feat, fix`)
	ev := Events(s.Audit(), "decision")
	require.NotEmpty(t, ev)
	assert.Equal(t, "warn", ev[len(ev)-1]["decision"])
}

func TestCommitPolicy_TooManyCommits(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		allowed      bool
	}{{"enforce", commitPolicy, false}, {"warn", commitWarn, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s, base := mainWith(t, map[string]string{"COMMITPOLICY": tc.policy})
			s.fastImport(base, "refs/heads/many", 10_001)
			r := s.Push(alice, "refs/heads/many:refs/heads/main")
			assert.Equal(t, tc.allowed, r.OK, r.Output)
			assert.Contains(t, r.Output, "More than 10000 commits enter the ref")
		})
	}
}

func TestCommitPolicy_InvalidAnchorSkipped(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": "version: 1\nrulesets: []\n"})
	s.Commit("anything", map[string]string{"a": "1"})
	r := s.Push(alice, "HEAD:refs/heads/main")
	assertAllowed(t, r)
	assert.Contains(t, r.Output, "COMMITPOLICY on the default branch is invalid")
	assert.Len(t, Events(s.Audit(), "invalid_commit_policy"), 1)
}

func TestCommitPolicy_ProtectedClassAPIFailure(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitProtected})
	s.Commit("bad", map[string]string{"a": "1"})
	r := s.Push(alice, "HEAD:refs/heads/feature")
	assertAllowed(t, r)
	assert.Contains(t, r.Output, "COMMITPOLICY [protected] refs/heads/feature: unable to verify authorization")
	assert.Contains(t, r.Output, "this ruleset is skipped")
}

func TestCommitPolicy_FeatureBranchFree(t *testing.T) {
	s, _ := mainWith(t, map[string]string{"COMMITPOLICY": commitProtected})
	s.Protected([]string{"main"}, nil)
	s.Commit("wip", map[string]string{"a": "1"})
	r := s.Push(alice, "HEAD:refs/heads/feature")
	assertAllowed(t, r)
	assert.NotContains(t, r.Output, "warning")
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/main"), `"wip"`)
}

func TestLintCommit(t *testing.T) {
	s, base := mainWith(t, map[string]string{"COMMITPOLICY": commitPolicy})
	s.Commit("feat: good", map[string]string{"a": "1"})
	s.Commit("bad one.", map[string]string{"a": "2"})
	policy := filepath.Join(s.Dir, "COMMITPOLICY")
	require.NoError(t, os.WriteFile(policy, []byte(commitPolicy), 0o600))
	out, code := s.binary(s.Work, "lint-commit", "--range", base+"..HEAD", "-f", policy)
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, `[conventional] error: header: expected "type(scope): subject"`)
	assert.NotContains(t, out, "feat: good")
	hook := s.Push(alice, "HEAD:refs/heads/main")
	assertDenied(t, hook, `header: expected "type(scope): subject"`)

	out, code = s.binary(s.Work, "lint-commit", "-m", "fix: ok", "-f", policy)
	assert.Equal(t, 0, code, out)
	out, code = s.binary(s.Work, "lint-commit", "-m", "fix: ok.", "-f", policy)
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "subject-full-stop")
}

func TestCorpus(t *testing.T) {
	root := filepath.Join("..", "corpus")
	modules, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, m := range modules {
		t.Run(m.Name(), func(t *testing.T) {
			for _, kind := range []string{"valid", "invalid"} {
				files, err := filepath.Glob(filepath.Join(root, m.Name(), kind, "*"))
				require.NoError(t, err)
				require.NotEmpty(t, files, "%s/%s is empty", m.Name(), kind)
				for _, f := range files {
					abs, err := filepath.Abs(f)
					require.NoError(t, err)
					out, code := (&Stand{t: t}).binary(t.TempDir(), "validate", "--module", m.Name(), "-f", abs)
					if kind == "valid" {
						assert.Equal(t, 0, code, "%s:\n%s", f, out)
					} else {
						assert.Equal(t, 1, code, "%s must be rejected:\n%s", f, out)
					}
				}
			}
		})
	}
}

// binary runs the hook binary with args and returns the combined output and the exit code.
func (s *Stand) binary(dir string, args ...string) (string, int) {
	s.t.Helper()
	cmd := exec.CommandContext(s.t.Context(), hookBin, args...)
	cmd.Dir = dir
	cmd.Env = append(append([]string{}, s.env...), "PATH="+gitPath())
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	require.NoError(s.t, err)
	return string(out), 0
}

// fastImport adds n commits on top of base under ref in the work clone, in one git process.
func (s *Stand) fastImport(base, ref string, n int) {
	s.t.Helper()
	var b bytes.Buffer
	for i := range n {
		fmt.Fprintf(&b, "commit %s\ncommitter dev <dev@example.com> %d +0000\ndata <<EOM\nfeat: commit %d\nEOM\n", ref, 1760000000+i, i)
		if i == 0 {
			fmt.Fprintf(&b, "from %s\n", base)
		}
		b.WriteString("\n")
	}
	s.gitStdin(b.String(), s.Work, "fast-import", "--quiet")
}
