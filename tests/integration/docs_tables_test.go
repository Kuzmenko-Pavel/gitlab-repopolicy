//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkMarker opens a table of a document whose rows a test runs: an HTML
// comment "<!-- check: KIND ARGS -->" right above the table.
var checkMarker = regexp.MustCompile(`(?m)^<!-- check: (.+?) -->\n`)

// docTable is a marked table: the marker and its rows without the header,
// each row split into cells with code spans unwrapped.
type docTable struct {
	doc    string
	marker []string
	rows   [][]string
}

func docTables(t *testing.T, names ...string) []docTable {
	t.Helper()
	var out []docTable
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
		require.NoError(t, err)
		text := string(data)
		for _, m := range checkMarker.FindAllStringSubmatchIndex(text, -1) {
			tb := docTable{doc: name, marker: strings.Fields(text[m[2]:m[3]])}
			lines := strings.Split(text[m[1]:], "\n")
			for i, line := range lines {
				if !strings.HasPrefix(line, "|") {
					break
				}
				if i < 2 {
					continue // the header and the separator
				}
				tb.rows = append(tb.rows, cells(line))
			}
			require.NotEmpty(t, tb.rows, "%s: the table after %q is empty", name, text[m[0]:m[1]])
			out = append(out, tb)
		}
	}
	return out
}

var escapedPipe = regexp.MustCompile(`\\\|`)

// cells splits a table row on unescaped pipes and unwraps code spans.
func cells(line string) []string {
	line = escapedPipe.ReplaceAllString(strings.Trim(line, "|"), "\x00")
	parts := strings.Split(line, "|")
	out := make([]string, 0, len(parts))
	for _, c := range parts {
		c = strings.ReplaceAll(strings.TrimSpace(c), "\x00", "|")
		if strings.HasPrefix(c, "`") && strings.HasSuffix(c, "`") && len(c) > 1 {
			c = c[1 : len(c)-1]
		}
		out = append(out, c)
	}
	return out
}

// verdict is the result column of a table in either language.
func verdict(t *testing.T, s string) string {
	t.Helper()
	switch s {
	case "проходить", "passes":
		return "pass"
	case "відмова", "denied":
		return "deny"
	case "попередження", "warning":
		return "warn"
	}
	t.Fatalf("unknown result %q", s)
	return ""
}

// lint runs lint-commit on a message with a policy file and checks the verdict.
func lint(t *testing.T, s *Stand, where, policyFile string, rules []string, message, want string) string {
	t.Helper()
	args := make([]string, 0, 5+2*len(rules))
	args = append(args, "lint-commit", "-f", policyFile, "-m", strings.ReplaceAll(message, `\n`, "\n"))
	for _, r := range rules {
		args = append(args, "--rule", r)
	}
	out, code := s.binary(t.TempDir(), args...)
	switch want {
	case "pass":
		assert.True(t, code == 0 && out == "", "%s: %q must pass:\n%s", where, message, out)
	case "warn":
		assert.True(t, code == 0 && strings.Contains(out, "warning:"), "%s: %q must warn:\n%s", where, message, out)
	case "deny":
		assert.True(t, code == 1 && strings.Contains(out, "error:"), "%s: %q must be denied:\n%s", where, message, out)
	}
	return out
}

// TestDocsCommitTables runs every marked table of the COMMITPOLICY document
// in both languages.
func TestDocsCommitTables(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	s := NewStand(t)
	for _, tb := range docTables(t, "commitpolicy.md", "commitpolicy.en.md") {
		kind, args := tb.marker[0], tb.marker[1:]
		for _, row := range tb.rows {
			where := tb.doc + " " + strings.Join(tb.marker, " ")
			switch {
			case kind == "lint-commit" && len(args) > 0 && args[0] == "parser=conventional":
				dir := t.TempDir()
				policy := filepath.Join(dir, "COMMITPOLICY")
				require.NoError(t, os.WriteFile(policy, []byte("version: 1\nrules:\n  r:\n    match: all\n    parser: conventional\n    message: m\n"), 0o600))
				out := lint(t, s, where, policy, nil, row[0], "deny")
				assert.Contains(t, out, row[1], "%s: %q", where, row[0])
			case kind == "lint-commit":
				lint(t, s, where, filepath.Join(repo, filepath.FromSlash(args[0])), args[1:], row[0], verdict(t, row[1]))
			case kind == "pattern":
				keys := ""
				if row[2] != "" {
					keys = ", keys: " + row[2]
				}
				dir := t.TempDir()
				policy := filepath.Join(dir, "COMMITPOLICY")
				require.NoError(t, os.WriteFile(policy, []byte("version: 1\nrules:\n  r:\n    match: all\n    parser: free\n    message: m\n"+
					"    checks:\n      pattern: {level: error, location: "+row[0]+", value: '"+row[1]+"'"+keys+"}\n"), 0o600))
				lint(t, s, where, policy, nil, row[3], verdict(t, row[4]))
			default:
				t.Fatalf("%s: unknown check %q", tb.doc, kind)
			}
		}
	}
}

// pushers are the people of the tables of the documents.
var pushers = map[string]User{"alice": alice, "bob": bob}

// TestDocsBranchTables pushes every new branch of the marked tables of the
// BRANCHPOLICY document in both languages, with the presets of
// examples/presets.yaml and the protected branches the marker names.
func TestDocsBranchTables(t *testing.T) {
	presets, err := os.ReadFile(filepath.Join("..", "..", "examples", "presets.yaml"))
	require.NoError(t, err)
	for _, tb := range docTables(t, "branchpolicy.md", "branchpolicy.en.md") {
		require.Equal(t, "branch-push", tb.marker[0], tb.doc)
		policy, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(tb.marker[1])))
		require.NoError(t, err)
		s, _ := mainWith(t, map[string]string{"BRANCHPOLICY": string(policy)})
		s.AddConfig(string(presets))
		s.Team()
		s.Protected(strings.Split(tb.marker[2], ","), nil)
		for _, row := range tb.rows {
			name, who, rule, want := row[0], pushers[row[1]], row[2], verdict(t, map[string]string{
				"дозволено": "проходить", "allowed": "passes", "відмова": "відмова", "denied": "denied"}[row[3]])
			r := s.Push(who, "HEAD:refs/heads/"+name)
			where := tb.doc + " " + tb.marker[1] + " " + name
			if want == "pass" {
				assert.True(t, r.OK, "%s must be allowed:\n%s", where, r.Output)
				continue
			}
			assert.False(t, r.OK, "%s must be denied:\n%s", where, r.Output)
			assert.Contains(t, r.Output, `BRANCHPOLICY rule "`+rule+`", branch `+name, where)
		}
	}
}

var signerLabel = regexp.MustCompile(`([A-Za-z0-9_.-]+) \(`)

// TestDocsTagTables asks validate --tag about every tag of the marked tables
// of the TAGPOLICY document in both languages.
func TestDocsTagTables(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	s := NewStand(t)
	for _, tb := range docTables(t, "tagpolicy.md", "tagpolicy.en.md") {
		require.Equal(t, "tag-rules", tb.marker[0], tb.doc)
		for _, row := range tb.rows {
			out, code := s.binary(t.TempDir(), "validate", "-f", filepath.Join(repo, filepath.FromSlash(tb.marker[1])), "--module", "tagpolicy", "--tag", row[0])
			where := tb.doc + " " + tb.marker[1] + " " + row[0]
			require.Equal(t, 0, code, "%s: %s", where, out)
			if row[1] == "-" {
				assert.Contains(t, out, "tag "+row[0]+": no rule matches", where)
				continue
			}
			rules := strings.ReplaceAll(strings.ReplaceAll(row[1], "`", ""), " ", "")
			assert.Contains(t, strings.ReplaceAll(out, " ", ""), "tag"+row[0]+":rules"+rules+"(", where)
			_, line, _ := strings.Cut(out, "  signers: ")
			line, _, _ = strings.Cut(line, "\n")
			var got []string
			for _, m := range signerLabel.FindAllStringSubmatch(line, -1) {
				got = append(got, m[1])
			}
			assert.Equal(t, strings.Split(row[2], ", "), got, where)
		}
	}
}
