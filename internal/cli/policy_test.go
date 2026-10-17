package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodPolicy = "version: 1\nrulesets:\n  - name: r\n    rules:\n      type-enum: {level: error, value: [feat]}\n"

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
		"bad.yaml":     "version: 1\nrulesets: [{name: r, rules: {typo: {level: error}}}]\n",
		"broken.yaml":  "version: 1\nrulesets: []\n",
		"config.yaml":  "gitlab_url: https://gitlab.example.com\ncommitpolicy_presets: {org: {rules: {type-enum: {level: error, value: [fix]}}}}\n",
		"extends.yaml": "version: 1\nextends: org\nrulesets: [{name: r}]\n",
	})
	code, out, _ := run(t, "validate")
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "COMMITPOLICY: valid\n", out)

	code, out, _ = run(t, "validate", "-f", filepath.Join(dir, "bad.yaml"), "--module", "commitpolicy")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, `bad.yaml: invalid`)
	assert.Contains(t, out, `error: ruleset r: unknown rule "typo"`)

	code, out, _ = run(t, "validate", "-f", "broken.yaml", "--module", "commitpolicy")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, "rulesets: 1 to 50 rulesets are required")

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
	code, _, errOut = run(t, "lint-commit", "--range", "HEAD")
	assert.Equal(t, ExitError, code, "outside of a repository")
	assert.Contains(t, errOut, "git")
}

func TestLintCommit_Ruleset(t *testing.T) {
	policy := "version: 1\nrulesets:\n  - name: protected\n    refs: protected\n    rules:\n      type-enum: {level: error, value: [feat, fix]}\n" +
		"  - name: all\n    rules:\n      header-max-length: {level: error, value: 20}\n"
	inDir(t, map[string]string{"COMMITPOLICY": policy})
	code, out, _ := run(t, "lint-commit", "-m", "chore: a header longer than twenty")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, out, "[protected]")
	assert.Contains(t, out, "[all]", "without --ruleset every ruleset applies")
	code, out, _ = run(t, "lint-commit", "--ruleset", "all", "-m", "chore: a header longer than twenty")
	assert.Equal(t, ExitError, code)
	assert.NotContains(t, out, "[protected]")
	code, out, _ = run(t, "lint-commit", "--ruleset", "protected", "--ruleset", "all", "-m", "fix: short")
	assert.Equal(t, ExitOK, code, out)
	code, _, errOut := run(t, "lint-commit", "--ruleset", "release", "-m", "fix: short")
	assert.Equal(t, ExitError, code)
	assert.Contains(t, errOut, `ruleset "release" is not in the file; it has protected, all`)
}

func TestLintCommit_PresetsOnlyConfig(t *testing.T) {
	inDir(t, map[string]string{
		"COMMITPOLICY": "version: 1\nextends: org\nrulesets:\n  - name: r\n",
		"config.yaml":  "# a developer copy: no gitlab_url, no token\nmode: strict\ncommitpolicy_presets:\n  org:\n    rules:\n      type-enum: {level: error, value: [feat]}\n",
	})
	code, out, errOut := run(t, "lint-commit", "--config", "config.yaml", "-m", "fix: no")
	assert.Equal(t, ExitError, code, errOut)
	assert.Contains(t, out, `type-enum: "fix" is not one of feat`, "the preset applies, the rest of the config is not read")
	code, out, _ = run(t, "validate", "--config", "config.yaml")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "COMMITPOLICY: valid")
}
