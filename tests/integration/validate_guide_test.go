//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	fencedBlock = regexp.MustCompile("(?ms)^```([a-z]*)\n(.*?)^```\n")
	heredoc     = regexp.MustCompile(`(?ms)^cat > (\S+) <<'EOF'\n(.*?)^EOF$`)
	isoDate     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
)

// TestValidateGuide follows the validate guide and the merge example of the
// configuration document in both languages: the files of their sh blocks are
// written into a demo directory, the commands of their console blocks run
// there with the binary, and the output and the exit codes are the ones the
// documents show. A policy file of the demo directory of the guide is a file
// of examples/.
func TestValidateGuide(t *testing.T) {
	bin := filepath.Dir(productBinary(t))
	examples, err := filepath.Glob(filepath.Join("..", "..", "examples", "*", "*"))
	require.NoError(t, err)
	for _, tc := range []struct {
		doc      string
		examples bool
	}{
		{"user/examples/validation-walkthrough.md", true},
		{"user/examples/validation-walkthrough.en.md", true},
		{"user/administration/configuration.md", false},
		{"user/administration/configuration.en.md", false},
		{"user/reference/validate.md", false},
		{"user/reference/validate.en.md", false},
	} {
		doc := tc.doc
		t.Run(doc, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
			require.NoError(t, err)
			demo := t.TempDir()
			if strings.HasPrefix(doc, "user/reference/") {
				data, err := os.ReadFile(filepath.Join("..", "..", "examples", "ci-config", "PATHPOLICY"))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(demo, "PATHPOLICY"), data, 0o644))
			}
			cwd, code, commands := demo, 0, 0
			for _, block := range fencedBlock.FindAllStringSubmatch(string(data), -1) {
				switch block[1] {
				case "sh":
					for _, h := range heredoc.FindAllStringSubmatch(block[2], -1) {
						path := filepath.Join(cwd, filepath.FromSlash(h[1]))
						require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
						require.NoError(t, os.WriteFile(path, []byte(h[2]), 0o644))
						if _, policy := policyModules[h[1]]; policy && tc.examples {
							assert.True(t, sameAsExample(t, examples, h[1], h[2]), "%s: %s is not a file of examples/", doc, h[1])
						}
					}
				case "console":
					lines := strings.Split(strings.TrimSuffix(block[2], "\n"), "\n")
					for i := 0; i < len(lines); i++ {
						cmd, ok := strings.CutPrefix(lines[i], "$ ")
						require.True(t, ok, "%s: %q is not a command", doc, lines[i])
						var want []string
						for i+1 < len(lines) && !strings.HasPrefix(lines[i+1], "$ ") {
							i++
							want = append(want, lines[i])
						}
						commands++
						switch {
						case strings.HasPrefix(cmd, "cd "):
							cwd = filepath.Join(cwd, strings.TrimPrefix(cmd, "cd "))
						case cmd == "echo $?":
							assert.Equal(t, []string{strconv.Itoa(code)}, want, "%s: exit code of the command before", doc)
						default:
							var out string
							out, code = shell(t, cwd, bin, cmd)
							assert.Equal(t, isoDate.ReplaceAllString(strings.Join(want, "\n"), "DATE"),
								isoDate.ReplaceAllString(strings.TrimSuffix(out, "\n"), "DATE"), "%s: $ %s", doc, cmd)
						}
					}
				}
			}
			assert.Positive(t, commands, "%s has no console blocks", doc)
		})
	}
}

// sameAsExample reports whether content is a file of examples/ with the name.
func sameAsExample(t *testing.T, examples []string, name, content string) bool {
	t.Helper()
	for _, e := range examples {
		if filepath.Base(e) != name {
			continue
		}
		data, err := os.ReadFile(e)
		require.NoError(t, err)
		if string(data) == content {
			return true
		}
	}
	return false
}

// shell runs a command line of a document with the binary first in PATH.
func shell(t *testing.T, dir, bin, line string) (string, int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", line)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + bin + string(filepath.ListSeparator) + gitPath(), "HOME=" + dir, "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	require.NoError(t, err)
	return string(out), 0
}
