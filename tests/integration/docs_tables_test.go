//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkMarker opens a table of a document whose rows a test runs: an HTML
// comment "<!-- check: KIND ARGS -->" right above the table.
var checkMarker = regexp.MustCompile(`(?m)^<!-- check: (.+?) -->\n`)

// TestDocsTableMarkers prevents a misspelled marker from silently losing coverage.
func TestDocsTableMarkers(t *testing.T) {
	for _, doc := range docs(t) {
		data, err := os.ReadFile(doc)
		require.NoError(t, err)
		for _, m := range checkMarker.FindAllStringSubmatch(string(data), -1) {
			kind := strings.Fields(m[1])[0]
			assert.Contains(t, []string{"lint-commit", "pattern", "commit-structure", "branch-push", "tag-rules"}, kind, "%s: unknown table marker", doc)
		}
	}
}

// docTable is a marked table: the marker, the header and the rows, each row
// split into cells with code spans unwrapped.
type docTable struct {
	doc    string
	marker []string
	header []string
	rows   [][]string
}

func docTables(t *testing.T, kinds ...string) []docTable {
	t.Helper()
	var out []docTable
	for _, name := range docs(t) {
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		text := string(data)
		for _, m := range checkMarker.FindAllStringSubmatchIndex(text, -1) {
			tb := docTable{doc: name, marker: strings.Fields(text[m[2]:m[3]])}
			if !slices.Contains(kinds, tb.marker[0]) {
				continue
			}
			lines := strings.Split(text[m[1]:], "\n")
			for i, line := range lines {
				if !strings.HasPrefix(line, "|") {
					break
				}
				if i == 0 {
					tb.header = cells(line)
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
	require.NotEmpty(t, out, "no documentation tables for %v", kinds)
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
	tables := docTables(t, "lint-commit", "pattern", "commit-structure")
	require.GreaterOrEqual(t, len(tables), 12, "preserve the original six COMMITPOLICY tables in each language")
	for _, tb := range tables {
		kind, args := tb.marker[0], tb.marker[1:]
		for _, row := range tb.rows {
			where := tb.doc + " " + strings.Join(tb.marker, " ")
			switch {
			case kind == "lint-commit" && len(args) > 0 && args[0] == "header-format=required":
				dir := t.TempDir()
				policy := filepath.Join(dir, "COMMITPOLICY")
				require.NoError(t, os.WriteFile(policy, []byte(`version: 1
rules:
  r:
    match: all
    message: m
    checks:
      header-format: {conventional: {level: error, value: required}}
`), 0o600))
				out := lint(t, s, where, policy, nil, row[0], "deny")
				assert.Contains(t, out, row[1], "%s: %q", where, row[0])
			case kind == "lint-commit":
				lint(t, s, where, filepath.Join(repo, filepath.FromSlash(args[0])), args[1:], row[0], verdict(t, row[1]))
			case kind == "pattern":
				dir := t.TempDir()
				policy := filepath.Join(dir, "COMMITPOLICY")
				require.NoError(t, os.WriteFile(policy, fmt.Appendf(nil, `version: 1
rules:
  r:
    match: all
    message: m
    checks:
      trailer-required: {x: {level: error, value: %s}}
      pattern: {x: {level: error, location: %s, value: '%s'}}
`, row[2], row[0], row[1]), 0o600))
				lint(t, s, where, policy, nil, row[3], verdict(t, row[4]))
			case kind == "commit-structure":
				for i, set := range tb.header[2:] {
					denied := structure(t, s, set, row[0])
					want, reasons, _ := strings.Cut(row[2+i], ":")
					var names []string
					for n := range strings.SplitSeq(strings.ReplaceAll(reasons, "`", ""), ",") {
						if n = strings.TrimSpace(n); n != "" {
							names = append(names, n)
						}
					}
					if verdict(t, want) == "pass" {
						names = []string{}
					}
					assert.Equal(t, names, denied, "%s: %s with %s", where, row[0], set)
				}
			default:
				t.Fatalf("%s: unknown check %q", tb.doc, kind)
			}
		}
	}
}

// structure lints a message of a layout such as "H _ B _ T" with the checks
// of the parts of a message set as "r / o / f / r": body-leading-blank-lines,
// body-required, trailer-leading-blank-lines, trailer-required. H is the
// header, B and T are lines of the body and the trailer, _ is an empty line.
// It returns the check types that broke, in the order of the output.
func structure(t *testing.T, s *Stand, set, layout string) []string {
	t.Helper()
	values := map[string]string{"o": "optional", "r": "required", "f": "forbidden"}
	v := strings.Split(set, "/")
	require.Len(t, v, 4, "%q: four values from the top of a message down", set)
	var mode [4]any
	for i := range mode {
		value, ok := values[strings.TrimSpace(v[i])]
		require.True(t, ok, "%q: o, r or f", set)
		mode[i] = value
	}
	file := filepath.Join(t.TempDir(), "COMMITPOLICY")
	require.NoError(t, os.WriteFile(file, fmt.Appendf(nil, `version: 1
rules:
  r:
    match: all
    message: m
    checks:
      body-leading-blank-lines: {x: {level: error, value: %s}}
      body-required: {x: {level: error, value: %s}}
      trailer-leading-blank-lines: {x: {level: error, value: %s}}
      trailer-required: {x: {level: error, value: %s}}
`, mode[:]...), 0o600))
	var lines []string
	for i, f := range strings.Fields(layout) {
		switch f {
		case "H":
			lines = append(lines, "fix: round refunds")
		case "_":
			lines = append(lines, "")
		default:
			lines = append(lines, fmt.Sprintf("%s%d text", f, i))
		}
	}
	out, _ := s.binary(t.TempDir(), "lint-commit", "-f", file, "-m", strings.Join(lines, "\n"))
	names := []string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "message: [r] error: "); ok {
			names = append(names, strings.Fields(rest)[0])
		}
	}
	return names
}

// pushers are the people of the tables of the documents.
var pushers = map[string]User{"alice": alice, "bob": bob}

// TestDocsBranchTables pushes every new branch of the marked tables of the
// BRANCHPOLICY document in both languages, with the presets of
// examples/presets.yaml and the protected branches the marker names.
func TestDocsBranchTables(t *testing.T) {
	presets, err := os.ReadFile(filepath.Join("..", "..", "examples", "presets.yaml"))
	require.NoError(t, err)
	tables := docTables(t, "branch-push")
	require.GreaterOrEqual(t, len(tables), 6, "preserve the original three BRANCHPOLICY tables in each language")
	for _, tb := range tables {
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
	tables := docTables(t, "tag-rules")
	require.GreaterOrEqual(t, len(tables), 4, "preserve the original two TAGPOLICY tables in each language")
	for _, tb := range tables {
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
