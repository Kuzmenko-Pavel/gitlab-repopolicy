package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodPolicy = "version: 1\nrules:\n  r:\n    match: all\n    parser: conventional\n    message: m\n    checks:\n      type-enum: {level: error, value: [feat]}\n"

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
		"config.yaml":  "gitlab_url: https://gitlab.example.com\npresets: {commitpolicy: {org: {rules: {r: {match: all, parser: conventional, message: m, checks: {type-enum: {level: error, value: [fix]}}}}}}}\n",
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
	assert.Equal(t, "message: [r] error: type-enum: \"fix\" is not one of feat\n", out)
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
	policy := "version: 1\nrules:\n  main:\n    match: [main]\n    parser: conventional\n    message: m\n    checks:\n      type-enum: {level: error, value: [feat, fix]}\n" +
		"  all:\n    match: all\n    parser: conventional\n    message: m\n    checks:\n      header-max-length: {level: error, value: 20}\n"
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
		"config.yaml":  "# a developer copy: no gitlab_url, no token\nmode: strict\npresets:\n  commitpolicy:\n    org:\n      rules:\n        r: {match: all, parser: conventional, message: m, checks: {type-enum: {level: error, value: [feat]}}}\n",
	})
	code, out, errOut := run(t, "lint-commit", "--config", "config.yaml", "-m", "fix: no")
	assert.Equal(t, ExitError, code, errOut)
	assert.Contains(t, out, `type-enum: "fix" is not one of feat`, "the preset applies, the rest of the config is not read")
	code, out, _ = run(t, "validate", "--config", "config.yaml")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "COMMITPOLICY: valid")
}
