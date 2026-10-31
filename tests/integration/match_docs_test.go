//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// matchRow is a row of the table "name -> matches or not" of docs/match:
// the match list, the name and the answer.
var matchRow = regexp.MustCompile("(?m)^\\| `(\\[.*\\])` \\| `([^`]+)` \\| (так|ні|yes|no) \\|")

// TestMatchDocs checks every row of the match table of both languages with
// validate --branch, the matcher of all four modules.
func TestMatchDocs(t *testing.T) {
	s := NewStand(t)
	for _, doc := range []string{"user/policies/match.md", "user/policies/match.en.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
		require.NoError(t, err)
		rows := matchRow.FindAllStringSubmatch(string(data), -1)
		require.NotEmpty(t, rows, "%s has no match table", doc)
		for _, row := range rows {
			list, name, want := row[1], row[2], row[3] == "так" || row[3] == "yes"
			dir := t.TempDir()
			policy := "version: 1\nrules:\n  r:\n    match: " + list + "\n    priority: 1\n    message: m\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "BRANCHPOLICY"), []byte(policy), 0o600))
			out, code := s.binary(dir, "validate", "--branch", name)
			require.Equal(t, 0, code, "%s: %s", doc, out)
			assert.Equal(t, want, !regexp.MustCompile(`no rule matches`).MatchString(out), "%s: match %s, name %s:\n%s", doc, list, name, out)
		}
	}
}
