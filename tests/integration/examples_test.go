//go:build integration

package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// policyModules maps a policy file name to its module.
var policyModules = map[string]string{"PATHPOLICY": "pathpolicy", "COMMITPOLICY": "commitpolicy", "TAGPOLICY": "tagpolicy", "BRANCHPOLICY": "branchpolicy"}

// TestExamples covers the examples/ part of the compatibility corpus: every
// example is a valid policy file of its module, merged with the presets of
// examples/presets.yaml, a server config with only presets.
func TestExamples(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	presets, err := filepath.Abs(filepath.Join(root, "presets.yaml"))
	require.NoError(t, err)
	seen := map[string]int{}
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == filepath.Join(root, "presets.yaml") {
			return err
		}
		module, ok := policyModules[d.Name()]
		if !assert.True(t, ok, "%s: examples hold only PATHPOLICY, COMMITPOLICY, TAGPOLICY and BRANCHPOLICY files and presets.yaml", path) {
			return nil
		}
		abs, err := filepath.Abs(path)
		require.NoError(t, err)
		out, code := (&Stand{t: t}).binary(t.TempDir(), "validate", "--config", presets, "--module", module, "-f", abs)
		assert.Equal(t, 0, code, "%s:\n%s", path, out)
		seen[module]++
		return nil
	}))
	for _, module := range policyModules {
		assert.Positive(t, seen[module], "no example of %s", module)
	}
}

// exampleBlock quotes an example or shipped config: the first line names
// the source file and the rest must equal it exactly.
var exampleBlock = regexp.MustCompile("(?s)```yaml\n# ((?:examples|config)/[^\n]+)\n(.*?)```")

// TestDocsQuoteExamples keeps policy examples of the documentation equal to
// examples/, which the corpus checks.
func TestDocsQuoteExamples(t *testing.T) {
	repo := filepath.Join("..", "..")
	quoted := 0
	for _, doc := range docs(t) {
		data, err := os.ReadFile(doc)
		require.NoError(t, err)
		for _, m := range exampleBlock.FindAllStringSubmatch(string(data), -1) {
			example, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(m[1])))
			if !assert.NoError(t, err, "%s quotes %s", doc, m[1]) {
				continue
			}
			assert.Equal(t, string(example), m[2], "%s quotes %s with changes", doc, m[1])
			quoted++
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "# examples/") {
				assert.Contains(t, string(data), "```yaml\n"+line+"\n", "%s: %q must open a yaml block", doc, line)
			}
		}
	}
	assert.Positive(t, quoted, "documentation must quote tested examples")
	t.Logf("%d quoted examples", quoted)
}

// TestDocsPolicyBlocks also validates complete inline policies that are not
// quotes of examples/. Fragments without a version are intentionally partial.
func TestDocsPolicyBlocks(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	checked := 0
	for _, doc := range docs(t) {
		module := ""
		for _, name := range policyModules {
			if strings.Contains(filepath.ToSlash(doc), "/"+name+"/") || strings.HasPrefix(filepath.Base(doc), name+".") {
				module = name
				break
			}
		}
		data, err := os.ReadFile(doc)
		require.NoError(t, err)
		config := filepath.Join(repo, "examples", "presets.yaml")
		for _, block := range fencedBlock.FindAllStringSubmatch(string(data), -1) {
			if block[1] == "yaml" && strings.HasPrefix(block[2], "presets:\n") {
				config = filepath.Join(t.TempDir(), "presets.yaml")
				require.NoError(t, os.WriteFile(config, []byte(block[2]), 0o600))
			}
		}
		for _, block := range fencedBlock.FindAllStringSubmatch(string(data), -1) {
			if block[1] != "yaml" || !strings.Contains("\n"+block[2], "\nversion: 1\n") {
				continue
			}
			blockModule := module
			if blockModule == "" {
				switch {
				case strings.Contains(block[2], "\nadmins:"):
					blockModule = "pathpolicy"
				case strings.Contains(block[2], "\nkeys:"):
					blockModule = "tagpolicy"
				case strings.Contains(block[2], "    parser:"):
					blockModule = "commitpolicy"
				case strings.Contains(block[2], "    priority:"):
					blockModule = "branchpolicy"
				}
			}
			require.NotEmpty(t, blockModule, "%s: a complete inline policy needs an identifiable module", doc)
			dir := t.TempDir()
			file := filepath.Join(dir, "policy.yaml")
			require.NoError(t, os.WriteFile(file, []byte(block[2]), 0o600))
			out, code := (&Stand{t: t}).binary(dir, "validate", "-f", file, "--module", blockModule, "--config", config)
			assert.Equal(t, 0, code, "%s inline %s:\n%s", doc, blockModule, out)
			checked++
		}
	}
	require.GreaterOrEqual(t, checked, 56, "complete policies from both languages must remain covered")
	t.Logf("%d complete inline policies checked", checked)
}
