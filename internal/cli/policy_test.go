package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/branch"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/tag"
)

const goodPolicy = "version: 1\nrules:\n  r:\n    match: all\n    parser: conventional\n    message: m\n    checks:\n      type-enum: {types: {level: error, value: [feat]}}\n"

func inDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	t.Chdir(dir)
	return dir
}

func TestValidate(t *testing.T) {
	dir := inDir(t, map[string]string{
		"COMMITPOLICY": goodPolicy,
		"bad.yaml":     "version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {typo: {level: error}}}\n",
		"broken.yaml":  "version: 1\nrules:\n  r: {match: all}\n",
		"config.yaml":  "gitlab_url: https://gitlab.example.com\npresets: {commitpolicy: {org: {rules: {r: {match: all, parser: conventional, message: m, checks: {type-enum: {types: {level: error, value: [fix]}}}}}}}}\n",
		"extends.yaml": "version: 1\nextends: org\n",
	})
	code, out, _ := run(t, "validate")
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "COMMITPOLICY: valid\n", out)

	code, out, _ = run(t, "validate", "-f", filepath.Join(dir, "bad.yaml"), "--module", "commitpolicy")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, `bad.yaml: invalid`)
	assert.Contains(t, out, `error: rule r: unknown check "typo"`)

	code, out, _ = run(t, "validate", "-f", "broken.yaml", "--module", "commitpolicy")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, "rule r: parser: required")

	code, out, _ = run(t, "validate", "-f", "extends.yaml", "--module", "commitpolicy")
	assert.Equal(t, ExitError, code, "a preset is unknown without the server config")
	assert.Contains(t, out, `preset "org" is not defined`)
	code, _, _ = run(t, "validate", "-f", "extends.yaml", "--module", "commitpolicy", "--config", "config.yaml")
	assert.Equal(t, ExitOK, code)

	code, _, errOut := run(t, "validate", "-f", "bad.yaml")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "is not a policy file; use --module with one of branchpolicy, commitpolicy")
	code, _, _ = run(t, "validate", "extra")
	assert.Equal(t, ExitUsage, code)
	code, _, errOut = run(t, "validate", "--config", "missing.yaml")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "missing.yaml")
	code, out, _ = run(t, "validate", "-f", "missing", "--module", "commitpolicy")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, "missing: invalid")

	inDir(t, nil)
	code, out, _ = run(t, "validate")
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "no policy files in the current directory\n", out)
}

func TestValidate_TagPolicyKeys(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE"
	inDir(t, map[string]string{"TAGPOLICY": "version: 1\nkeys:\n  lead: " + key + "\n"})
	code, out, _ := run(t, "validate")
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "TAGPOLICY: valid\n  warning: keys.lead is not used by any rule\n", out, "the checks that need time run offline too")
}

func TestLintCommit_Usage(t *testing.T) {
	inDir(t, map[string]string{"COMMITPOLICY": goodPolicy})
	code, _, errOut := run(t, "lint-commit")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "give exactly one of -m and --range")
	code, _, _ = run(t, "lint-commit", "-m", "x", "--range", "a..b")
	assert.Equal(t, ExitUsage, code)
	code, _, errOut = run(t, "lint-commit", "-m", "x", "--skip-author", "bot@example.com")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--skip-author needs --range")
	code, out, _ := run(t, "lint-commit", "-m", "feat: ok")
	assert.Equal(t, ExitOK, code)
	assert.Empty(t, out)
	code, out, _ = run(t, "lint-commit", "-m", "fix: no")
	assert.Equal(t, ExitError, code)
	assert.Equal(t, "message: [r] error: type-enum types: \"fix\" is not one of feat\n", out)
	code, _, errOut = run(t, "lint-commit", "-m", "feat: ok", "-f", "missing")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, "missing")
	inDir(t, map[string]string{"COMMITPOLICY": "version: 1\nextends: missing\n"})
	code, _, errOut = run(t, "lint-commit", "-m", "feat: ok")
	assert.Equal(t, ExitError, code, "a missing preset is an error, not a silent pass")
	assert.Contains(t, errOut, `preset "missing" is not defined`)
	inDir(t, map[string]string{"COMMITPOLICY": goodPolicy})
	code, _, errOut = run(t, "lint-commit", "--range", "HEAD")
	assert.Equal(t, ExitError, code, "outside of a repository")
	assert.Contains(t, errOut, "git")
}

func TestLintCommit_Rule(t *testing.T) {
	policy := "version: 1\nrules:\n  main:\n    match: [main]\n    parser: conventional\n    message: m\n    checks:\n      type-enum: {types: {level: error, value: [feat, fix]}}\n" +
		"  all:\n    match: all\n    parser: conventional\n    message: m\n    checks:\n      header-max-length: {short: {level: error, value: 20}}\n"
	inDir(t, map[string]string{"COMMITPOLICY": policy})
	code, out, _ := run(t, "lint-commit", "-m", "chore: a header longer than twenty")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, "[main]")
	assert.Contains(t, out, "[all]", "without --rule every rule applies")
	code, out, _ = run(t, "lint-commit", "--rule", "all", "-m", "chore: a header longer than twenty")
	assert.Equal(t, ExitError, code)
	assert.NotContains(t, out, "[main]")
	code, out, _ = run(t, "lint-commit", "--rule", "main", "--rule", "all", "-m", "fix: short")
	assert.Equal(t, ExitOK, code, out)
	code, _, errOut := run(t, "lint-commit", "--rule", "release", "-m", "fix: short")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, `rule "release" is not in the file; it has main, all`)
}

func TestLintCommit_PresetsOnlyConfig(t *testing.T) {
	inDir(t, map[string]string{
		"COMMITPOLICY": "version: 1\nextends: org\n",
		"config.yaml":  "# a developer copy: no gitlab_url, no token\nmode: strict\npresets:\n  commitpolicy:\n    org:\n      rules:\n        r: {match: all, parser: conventional, message: m, checks: {type-enum: {types: {level: error, value: [feat]}}}}\n",
	})
	code, out, errOut := run(t, "lint-commit", "--config", "config.yaml", "-m", "fix: no")
	assert.Equal(t, ExitError, code, errOut)
	assert.Contains(t, out, `type-enum types: "fix" is not one of feat`, "the preset applies, the rest of the config is not read")
	code, out, _ = run(t, "validate", "--config", "config.yaml")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "COMMITPOLICY: valid")
}

const branchPresetConfig = `presets:
  branchpolicy:
    org:
      rules:
        personal: {match: ['/^[^/]+\//'], priority: 10, user_prefix: true, message: Personal branches start with your username.}
        release: {match: ['/^release\//'], priority: 5, protected: true, message: Release branches are protected.}
        other: {match: all, priority: 100, deny: true, message: Use username/topic.}
`

func TestValidate_Branch(t *testing.T) {
	inDir(t, map[string]string{
		"BRANCHPOLICY": "version: 1\nextends: org\nrules:\n  release:\n    message: Release branches belong to the release team.\n" +
			"  hotfix: {match: ['/^hotfix\\//'], priority: 1, message: Hotfixes are free.}\n",
		"config.yaml": branchPresetConfig,
	})
	tests := []struct {
		branch, want string
	}{
		{"hotfix/1", "branch hotfix/1: rule hotfix (match: [/^hotfix\\//]; from file)\n  result: allowed\n"},
		{"release/1.2", "branch release/1.2: rule release (match: [/^release\\//]; from preset:org, file)\n" +
			"  requires: the project protects the branch in Settings > Repository > Protected branches\n  result: allowed when the requirements hold\n"},
		{"petro/fix", "branch petro/fix: rule personal (match: [/^[^/]+\\//]; from preset:org)\n" +
			"  requires: petro is an active GitLab user (checked with --online)\n  result: allowed when the requirements hold\n"},
		{"fix", "branch fix: rule other (match: all; from preset:org)\n  result: denied, branches of this class may not be created\n  message: Use username/topic.\n"},
	}
	for _, tt := range tests {
		code, out, errOut := run(t, "validate", "--config", "config.yaml", "--branch", tt.branch)
		assert.Equal(t, ExitOK, code, "a query, not a check: %s", errOut)
		assert.Contains(t, out, tt.want)
	}
	code, out, _ := run(t, "validate", "--config", "config.yaml")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "  extends: org\n  rule other: preset:org\n")
	assert.Contains(t, out, "  rule release: preset:org, file\n    match: preset:org\n    message: file\n    priority: preset:org\n    protected: preset:org\n")
	assert.Contains(t, out, "  rule hotfix: file\n")

	code, out, _ = run(t, "validate", "--branch", "fix")
	assert.Equal(t, ExitError, code, "the file is checked first")
	assert.NotContains(t, out, "branch fix:")

	inDir(t, map[string]string{"BRANCHPOLICY": "version: 1\nrules:\n  feature: {match: ['/^feature\\//'], priority: 1, message: m}\n"})
	_, out, _ = run(t, "validate", "--branch", "main")
	assert.Contains(t, out, "branch main: no rule matches, the name is not checked\n")
	assert.NotContains(t, out, "extends:", "origins only for a file with presets")
	code, _, errOut := run(t, "validate", "--tag", "v1")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--tag needs a TAGPOLICY file")
}

func TestValidate_Tag(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE"
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
	inDir(t, map[string]string{"policy.yaml": "version: 1\nkeys:\n  lead: " + key + "\n  dev: " + other + "\nrules:\n" +
		"  release: {match: ['/^v\\d/'], signers: [lead, dev], protected: true, message: m}\n" +
		"  stable: {match: ['/^v1\\./'], signers: [lead], message: m}\n"})
	code, out, _ := run(t, "validate", "-f", "policy.yaml", "--module", "tagpolicy", "--tag", "v1.2.0")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "tag v1.2.0: rules release, stable (from file)\n  signers: lead (SSH)\n"+
		"  requires: the project protects the tag in Settings > Repository > Protected tags\n"+
		"  the tag must be signed by one of the signers; validate does not check signatures\n")
	_, out, _ = run(t, "validate", "-f", "policy.yaml", "--module", "tagpolicy", "--tag", "build-1")
	assert.Contains(t, out, "tag build-1: no rule matches, the tag is not checked\n")
	code, _, errOut := run(t, "validate", "-f", "policy.yaml", "--module", "tagpolicy", "--branch", "x")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--branch needs a BRANCHPOLICY file")
}

func TestValidate_BrokenPreset(t *testing.T) {
	inDir(t, map[string]string{
		"COMMITPOLICY": goodPolicy,
		"config.yaml":  "presets:\n  commitpolicy:\n    org:\n      rules:\n        r: {match: all}\n",
	})
	code, out, _ := run(t, "validate", "--config", "config.yaml")
	assert.Equal(t, ExitError, code, "a preset no file extends is still reported")
	assert.Contains(t, out, "config.yaml: invalid presets\n  error: presets.commitpolicy.org: ")
	assert.Contains(t, out, "COMMITPOLICY: valid\n")
}

// fakeUsers answers the user lookups of validate --online.
type fakeUsers struct {
	users  map[string]module.User
	status module.Status
}

func (f fakeUsers) Users(context.Context, []string) (map[string]module.User, module.Status) {
	return f.users, f.status
}

func (fakeUsers) AccessLevels(context.Context, []int64) (map[int64]int, module.Status) {
	return nil, module.Confirmed()
}

func (fakeUsers) Known(string) bool { return false }

func TestPrintBranch_UserPrefix(t *testing.T) {
	p, err := branch.Parse([]byte("version: 1\nrules:\n  work:\n    match: ['/./']\n    priority: 1\n    user_prefix: true\n    message: Work branches start with your username.\n"), nil)
	require.NoError(t, err)
	tests := []struct {
		name, branch string
		v            module.Validator
		want         string
	}{
		{"no prefix", "fix-login", nil, "  result: denied, the name does not start with a GitLab username and /\n  message: Work branches start with your username.\n"},
		{"active user", "alice/fix", fakeUsers{users: map[string]module.User{"alice": {ID: 1, Active: true}}, status: module.Confirmed()},
			"  alice is an active GitLab user\n  result: allowed\n"},
		{"blocked user", "bob/fix", fakeUsers{users: map[string]module.User{"bob": {ID: 2}}, status: module.Confirmed()},
			"  result: denied, bob is not an active GitLab user\n"},
		{"no answer", "carol/fix", fakeUsers{status: module.Unknown("timeout")},
			"  requires: carol is an active GitLab user (not checked: timeout)\n  result: allowed when the requirements hold\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printBranch(context.Background(), &output{w: &buf}, p, tt.branch, tt.v)
			assert.Contains(t, buf.String(), tt.want)
		})
	}
}

func TestPrintTag_NoCommonSigner(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE"
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
	p, err := tag.New(nil).Parse([]byte("version: 1\nkeys:\n  a: " + key + "\n  b: " + other + "\nrules:\n" +
		"  one: {match: ['/^v/'], signers: [a], message: m}\n  two: {match: ['/-prod$/'], signers: [b], message: m}\n"))
	require.NoError(t, err)
	var buf bytes.Buffer
	printTag(&output{w: &buf}, p, "v1-prod")
	assert.Contains(t, buf.String(), "  signers: none, the matching rules have no signer in common; every such tag is denied\n")
}

func TestValidate_OnlineNeedsConfig(t *testing.T) {
	inDir(t, map[string]string{"COMMITPOLICY": goodPolicy})
	code, _, errOut := run(t, "validate", "--online")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "--online needs --config with gitlab_url and token_file")
}
