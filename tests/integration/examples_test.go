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
// example is a valid policy file of its module.
func TestExamples(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	seen := map[string]int{}
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		module, ok := policyModules[d.Name()]
		if !assert.True(t, ok, "%s: examples hold only PATHPOLICY, COMMITPOLICY, TAGPOLICY and BRANCHPOLICY files", path) {
			return nil
		}
		abs, err := filepath.Abs(path)
		require.NoError(t, err)
		out, code := (&Stand{t: t}).binary(t.TempDir(), "validate", "--module", module, "-f", abs)
		assert.Equal(t, 0, code, "%s:\n%s", path, out)
		seen[module]++
		return nil
	}))
	for _, module := range policyModules {
		assert.Positive(t, seen[module], "no example of %s", module)
	}
}

// exampleBlock is a fenced YAML block of the documentation that quotes an
// example: its first line names the file, the rest is the file as it is.
var exampleBlock = regexp.MustCompile("(?s)```yaml\n# (examples/[^\n]+)\n(.*?)```")

// TestDocsQuoteExamples keeps policy examples of the documentation equal to
// examples/, which the corpus checks.
func TestDocsQuoteExamples(t *testing.T) {
	repo := filepath.Join("..", "..")
	docs, err := filepath.Glob(filepath.Join(repo, "docs", "*.md"))
	require.NoError(t, err)
	readmes, err := filepath.Glob(filepath.Join(repo, "*.md"))
	require.NoError(t, err)
	quoted := 0
	for _, doc := range append(readmes, docs...) {
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
	t.Logf("%d quoted examples", quoted)
}
