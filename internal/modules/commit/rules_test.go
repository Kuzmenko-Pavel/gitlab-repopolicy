package commit

import (
	_ "embed"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSettings() *Settings {
	return NewSettings(map[string]any{
		"org": map[string]any{"rules": map[string]any{
			"conventional": map[string]any{"match": "all", "message": "Commits follow Conventional Commits.",
				"checks": map[string]any{
					"header-format":     map[string]any{"conventional": map[string]any{"level": "error", "value": "required"}},
					"type-enum":         map[string]any{"types": map[string]any{"level": "error", "value": []any{"feat", "fix"}}},
					"header-max-length": map[string]any{"short": map[string]any{"level": "warn", "value": 72}},
				}},
		}},
		"broken": map[string]any{"bogus": 1},
	})
}

// one parses a policy with one rule and the given checks block.
func one(t *testing.T, checks string) *Policy {
	t.Helper()
	p, err := Parse([]byte("version: 1\nrules:\n  r:\n    match: all\n    message: m\n    checks:\n"+indent(checks, 6)), testSettings())
	require.NoError(t, err)
	return p
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, pad+l)
	}
	return strings.Join(out, "\n") + "\n"
}

func reasons(p *Policy, msg string) []string {
	findings := p.lint(commitInfo{message: msg})
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		lv := "warn"
		if f.Error {
			lv = "error"
		}
		out = append(out, lv+": "+f.Reason)
	}
	return out
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name, checks string
		ok, bad      []string
		want         string
	}{
		{"header-format-required", "header-format: {x: {level: error, value: required}}",
			[]string{"feat: x", "fix(a)!: y"}, []string{"Update x", "feat(): x", "feat:x"}, "error: header-format x: "},
		{"header-format-optional", "header-format: {x: {level: error, value: optional}}",
			[]string{"feat: x", "Update x", "PAY-1 Round refunds"}, nil, ""},
		{"header-max-length", "header-max-length: {x: {level: error, value: 10}}",
			[]string{"feat: ядро", "fix: 12345"}, []string{"fix: 123456"}, "header-max-length x: the header is 11 characters, the limit is 10"},
		{"type-enum", "type-enum: {x: {level: error, value: [feat, fix]}}",
			[]string{"feat: x", "fix(a): y", "Update x"}, []string{"feature: x"}, `error: type-enum x: "feature" is not one of feat, fix`},
		{"scope", "scope: {x: {level: error, value: required}}", []string{"feat(a): x"}, []string{"feat: x"}, "scope x: a scope is required"},
		{"scope-forbidden", "scope: {x: {level: error, value: forbidden}}", []string{"feat: x"}, []string{"feat(a): x"}, `scope x: a scope is not allowed, found "a"`},
		{"scope-enum", "scope-enum: {x: {level: warn, value: [api, ui, SPE, SPP]}}",
			[]string{"feat(api): x", "feat: no scope", "feat(SPE, SPP): x", "feat(SPE/SPP): x"}, []string{"feat(db): x"}, `warn: scope-enum x: "db" is not one of`},
		{"breaking-bang-forbidden", "breaking-bang: {x: {level: error, value: forbidden}}", []string{"feat: x"}, []string{"feat!: x"}, `breaking-bang x: "!" after the type is not allowed`},
		{"breaking-bang-required", "breaking-bang: {x: {level: error, value: required}}", []string{"feat!: x"}, []string{"feat: x"}, `breaking-bang x: "!" after the type is required`},
		{"subject-starts-lowercase-required", "subject-starts-lowercase: {x: {level: error, value: required}}",
			[]string{"feat: add", "feat: ядро", "update the guide"}, []string{"feat: Add", "feat: Ядро", "feat: 2fa for login", "PAY-1 Round refunds"},
			"subject-starts-lowercase x: the subject must start with a lowercase letter"},
		{"subject-starts-lowercase-forbidden", "subject-starts-lowercase: {x: {level: error, value: forbidden}}",
			[]string{"feat: Add", "feat: 2fa for login"}, []string{"feat: add"}, "the subject must not start with a lowercase letter"},
		{"subject-trailing-dot-forbidden", "subject-trailing-dot: {x: {level: error, value: forbidden}}", []string{"feat: x"}, []string{"feat: x.", "PAY-1 Round refunds."}, "subject-trailing-dot x: the subject must not end with a full stop"},
		{"subject-trailing-dot-required", "subject-trailing-dot: {x: {level: error, value: required}}", []string{"feat: x."}, []string{"feat: x"}, "the subject must end with a full stop"},
		{"body-leading-blank-lines-required", "body-leading-blank-lines: {x: {level: error, value: required}}",
			[]string{"feat: x\n\nwhy", "feat: x\n\n"}, []string{"feat: x\nwhy", "feat: x\n", "feat: x"}, "body-leading-blank-lines x: an empty line must follow the header"},
		{"body-leading-blank-lines-forbidden", "body-leading-blank-lines: {x: {level: error, value: forbidden}}",
			[]string{"feat: x\nwhy", "feat: x"}, []string{"feat: x\n\nwhy"}, "the line after the header must not be empty"},
		{"body-required", "body-required: {x: {level: error, value: required, types: [feat]}}",
			[]string{"feat: x\n\nwhy", "feat: x\nwhy right after the header", "fix: no body"}, []string{"feat: x"}, "body-required x: the message needs a body"},
		{"body-forbidden", "body-required: {x: {level: error, value: forbidden}}", []string{"feat: x"}, []string{"feat: x\n\nwhy"}, "the message must not have a body"},
		{"body-max-length", "body-max-length: {x: {level: warn, value: 10}}",
			[]string{"feat: x\n\n12345\n1234"}, []string{"feat: x\n\n12345\n12345"}, "body-max-length x: the body is 11 characters, the limit is 10"},
		{"trailer-leading-blank-lines-required", "body-required: {x: {level: error, value: required}}\ntrailer-required: {x: {level: error, value: required}}\ntrailer-leading-blank-lines: {x: {level: error, value: required}}",
			[]string{"feat: x\n\nwhy\n\nRefs: #1"}, []string{"feat: x\n\nwhy\nRefs: #1"}, "trailer-leading-blank-lines x: an empty line must separate the body and the trailer"},
		{"trailer-leading-blank-lines-forbidden", "trailer-required: {x: {level: error, value: required}}\ntrailer-leading-blank-lines: {x: {level: error, value: forbidden}}",
			[]string{"feat: x\nwhy\nRefs: #1", "feat: x\n\nRefs: #1"}, []string{"feat: x\n\nwhy\n\nRefs: #1"}, "the line before the trailer must not be empty"},
		{"trailer-required", "trailer-required: {x: {level: error, value: required}}",
			[]string{"feat: x\n\nRefs: #1", "feat: x\nRefs: #1"}, []string{"feat: x", "feat: x\n\n"}, "trailer-required x: the message needs a trailer"},
		{"trailer-forbidden", "trailer-required: {x: {level: error, value: forbidden}}\nbody-required: {x: {level: error, value: required}}",
			[]string{"feat: x\n\nwhy\n\nmore why"}, []string{"feat: x"}, "body-required x"},
		{"trailer-max-length", "trailer-required: {x: {level: error, value: required}}\ntrailer-max-length: {x: {level: error, value: 8}}",
			[]string{"feat: x\n\nRefs: #1"}, []string{"feat: x\n\nRefs: #12"}, "trailer-max-length x: the trailer is 9 characters, the limit is 8"},
		{"pattern-trailer", `trailer-required: {x: {level: error, value: required}}
pattern: {issue: {level: error, location: trailer, value: '/(?m)^(Issue|Refs): [A-Za-z0-9_.\/-]*#[0-9]+$/'}}`,
			[]string{"fix: x\n\nIssue: group/my.project#1", "fix: x\n\nbody\n\nRefs: #12\nSigned-off-by: A <a@b>"},
			[]string{"fix: x\n\nsee group/p#1 in body\n\nOther: #1"}, `pattern issue: no match of /(?m)^(Issue|Refs): [A-Za-z0-9_.\/-]*#[0-9]+$/ in the trailer`},
		{"pattern-body", `pattern: {x: {level: error, location: body, value: '/#[0-9]+/'}}`,
			[]string{"fix: x\n\nthe race (aws/agent#665) again"}, []string{"fix: x #1"}, "in the body"},
		{"pattern-message", `pattern: {x: {level: error, location: message, value: '/#[0-9]+/'}}`,
			[]string{"fix: x #1", "fix: x\n\nRefs: #1"}, []string{"fix: x"}, "in the message"},
		{"pattern-header-not", `pattern: {no-wip: {level: error, location: header, value: '!/^(WIP|fixup!)/i'}}`,
			[]string{"feat: ok", "feat: wip later"}, []string{"WIP: x", "wip: x", "fixup! feat: x"}, `pattern no-wip: /^(WIP|fixup!)/i must not be in the header`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := one(t, tt.checks)
			for _, msg := range tt.ok {
				assert.Empty(t, reasons(p, msg), msg)
			}
			for _, msg := range tt.bad {
				got := reasons(p, msg)
				require.NotEmpty(t, got, msg)
				assert.Contains(t, strings.Join(got, "\n"), tt.want, msg)
			}
		})
	}
}

// TestStructure: the same messages with the settings of the parts of a
// message from the top down: body-leading-blank-lines, body-required,
// trailer-leading-blank-lines, trailer-required.
func TestStructure(t *testing.T) {
	settings := []string{"r r r r", "o r o r", "f r f r", "o o f r", "r o r o"}
	values := map[string]string{"o": "optional", "r": "required", "f": "forbidden"}
	for _, tc := range []struct {
		layout string
		pass   [5]bool
	}{
		{"H _ B _ T", [5]bool{true, true, false, false, true}},
		{"H _ B _ B _ T", [5]bool{true, true, false, false, true}},
		{"H _ B B _ T T", [5]bool{true, true, false, false, true}},
		{"H B B B T", [5]bool{false, true, true, true, false}},
		{"H _ B B B T", [5]bool{false, true, false, true, true}},
		{"H _ T", [5]bool{false, false, false, true, true}},
		{"H T", [5]bool{false, false, false, true, false}},
		{"H _ B", [5]bool{false, false, false, true, true}},
		{"H", [5]bool{false, false, false, false, false}},
	} {
		for i, set := range settings {
			v := strings.Fields(set)
			p := one(t, fmt.Sprintf(`body-leading-blank-lines: {x: {level: error, value: %s}}
body-required: {x: {level: error, value: %s}}
trailer-leading-blank-lines: {x: {level: error, value: %s}}
trailer-required: {x: {level: error, value: %s}}
`, values[v[0]], values[v[1]], values[v[2]], values[v[3]]))
			got := reasons(p, layout(tc.layout))
			assert.Equal(t, tc.pass[i], len(got) == 0, "%s with %s: %v", tc.layout, set, got)
		}
	}
}

// TestHeaderNotParsed: a header that is not conventional skips the checks of
// type, scope and "!"; the subject checks read the whole header unless the
// rule requires the format.
func TestHeaderNotParsed(t *testing.T) {
	p := one(t, `header-format: {x: {level: warn, value: required}}
type-enum: {x: {level: error, value: [feat, fix]}}
scope: {x: {level: error, value: required}}
subject-starts-lowercase: {x: {level: error, value: required}}
subject-trailing-dot: {x: {level: error, value: forbidden}}
`)
	assert.Equal(t, []string{`warn: header-format x: expected "type(scope): subject"`}, reasons(p, "Fix timeout."))

	p = one(t, `header-format: {x: {level: error, value: optional}}
type-enum: {x: {level: error, value: [feat, fix]}}
subject-starts-lowercase: {x: {level: error, value: required}}
subject-trailing-dot: {x: {level: error, value: forbidden}}
`)
	assert.Equal(t, []string{
		"error: subject-starts-lowercase x: the subject must start with a lowercase letter",
		"error: subject-trailing-dot x: the subject must not end with a full stop",
	}, reasons(p, "Fix timeout."), "with optional the subject is the whole header")
	assert.Empty(t, reasons(p, "fix timeout"))
	assert.Equal(t, []string{`error: type-enum x: "docs" is not one of feat, fix`}, reasons(p, "docs: x"), "a conventional header is still read")

	assert.Equal(t, []string{"error: header-format x: the scope in parentheses is empty"},
		reasons(one(t, "header-format: {x: {level: error, value: required}}"), "feat(): x"))
	assert.Equal(t, []string{"error: header is empty"}, reasons(one(t, "{}"), "\nbody"), "a message without a header cannot be parsed")
	assert.Equal(t, []string{"error: message contains a NUL byte"}, reasons(one(t, "{}"), "feat: x\x00"))
}

// TestHeaderNotParsed_Catalogue runs every check type, set to fail, on a
// message whose header is not conventional, with header-format optional and
// required. The checks of type, scope and "!" and the checks with types never
// run on it; the subject checks run only with optional; the checks of the
// header length, the body, the trailer and pattern always run.
func TestHeaderNotParsed_Catalogue(t *testing.T) {
	msg := "Update the guide.\n\nWhy it changed.\n\nRefs: #1"
	for _, tc := range []struct {
		check              string
		optional, required bool
	}{
		{"header-max-length: {x: {level: error, value: 5}}", true, true},
		{"type-enum: {x: {level: error, value: [feat]}}", false, false},
		{"scope: {x: {level: error, value: required}}", false, false},
		{"scope-enum: {x: {level: error, value: [api]}}", false, false},
		{"breaking-bang: {x: {level: error, value: required}}", false, false},
		{"subject-starts-lowercase: {x: {level: error, value: required}}", true, false},
		{"subject-trailing-dot: {x: {level: error, value: forbidden}}", true, false},
		{"body-leading-blank-lines: {x: {level: error, value: forbidden}}", true, true},
		{"body-required: {x: {level: error, value: forbidden}}", true, true},
		{"body-max-length: {x: {level: error, value: 3}}", true, true},
		{"trailer-leading-blank-lines: {x: {level: error, value: forbidden}}", true, true},
		{"trailer-required: {x: {level: error, value: required}}\ntrailer-max-length: {x: {level: error, value: 3}}", true, true},
		{"pattern: {x: {level: error, location: header, value: '/^feat/'}}", true, true},
		{"pattern: {x: {level: error, location: trailer, value: '!/^Refs: /'}}", true, true},
		{"body-required: {x: {level: error, value: forbidden, types: [feat]}}", false, false},
		{"pattern: {x: {level: error, location: header, value: '/^feat/', types: [feat]}}", false, false},
	} {
		for format, want := range map[string]bool{"optional": tc.optional, "required": tc.required} {
			p := one(t, "header-format: {f: {level: warn, value: "+format+"}}\n"+tc.check)
			var got []string
			for _, r := range reasons(p, msg) {
				if !strings.HasPrefix(r, "warn: header-format f: ") {
					got = append(got, r)
				}
			}
			assert.Equal(t, want, len(got) > 0, "%s with header-format %s: %v", tc.check, format, got)
		}
	}
}

// TestHeaderNotParsed_Parts: the body and the trailer of a message whose
// header is not conventional are found the same way as after a conventional
// header, and checks with types do not change how they are found.
func TestHeaderNotParsed_Parts(t *testing.T) {
	refs := `pattern: {refs: {level: error, location: trailer, value: '/(?m)^Refs: #\d+$/'}}`
	p := one(t, "trailer-required: {x: {level: error, value: required}}\n"+refs)
	for _, msg := range []string{"PAY-1 Round refunds\n\nRefs: #1", "PAY-1 Round refunds\n\nwhy\n\nRefs: #1", "PAY-1 Round refunds\nRefs: #1"} {
		assert.Empty(t, reasons(p, msg), "%q", msg)
	}
	assert.Equal(t, []string{"error: trailer-required x: the message needs a trailer, the last part of the message",
		`error: pattern refs: no match of /(?m)^Refs: #\d+$/ in the trailer`}, reasons(p, "PAY-1 Round refunds"))

	p = one(t, "trailer-required: {x: {level: error, value: required, types: [feat]}}\n"+refs)
	assert.Empty(t, reasons(p, "feat: round refunds\n\nRefs: #1"))
	assert.Equal(t, []string{`error: pattern refs: no match of /(?m)^Refs: #\d+$/ in the trailer`},
		reasons(p, "PAY-1 Round refunds\n\nRefs: #1"), "without a type the trailer is optional and the paragraph after the header is the body")
}

// TestTrailerMode: the checks of a type that apply to the type of the commit
// decide how the message is split; required wins over forbidden, and
// without such checks the part is optional. Contradicting checks are not
// reconciled: forbidden still reports the trailer that required found.
func TestTrailerMode(t *testing.T) {
	msg := "fix: round refunds\n\nwhy\n\nRefs: #1"
	refs := "pattern: {refs: {level: error, location: trailer, value: '/^Refs: #1$/'}}\n"
	noRefs := "error: pattern refs: no match of /^Refs: #1$/ in the trailer"
	forbidden := "warn: trailer-required a: the message must not have a trailer"
	for _, tc := range []struct {
		checks string
		want   []string
	}{
		{"", []string{}},
		{"trailer-required: {a: {level: warn, value: forbidden}}", []string{noRefs}},
		{"trailer-required: {a: {level: warn, value: forbidden}, b: {level: warn, value: required}}", []string{forbidden}},
		{"trailer-required: {a: {level: warn, value: forbidden}, b: {level: warn, value: optional}}", []string{noRefs}},
		{"trailer-required: {a: {level: warn, value: forbidden, types: [feat]}}", []string{}},
		{"trailer-required: {a: {level: warn, value: forbidden}, b: {level: warn, value: required, types: [feat]}}", []string{noRefs}},
	} {
		assert.Equal(t, tc.want, reasons(one(t, refs+tc.checks), msg), tc.checks)
	}

	// body-required decides whether a required trailer is the whole paragraph after the header.
	msg = "fix: round refunds\n\nwhy\nRefs: #1"
	refs = "trailer-required: {x: {level: error, value: required}}\npattern: {refs: {level: error, location: trailer, value: '/^Refs: #1$/'}}\n"
	assert.NotEmpty(t, reasons(one(t, refs), msg), "the trailer is the whole paragraph")
	assert.Empty(t, reasons(one(t, refs+"body-required: {x: {level: warn, value: required}}"), msg), "the trailer is the last line")
	assert.NotEmpty(t, reasons(one(t, refs+"body-required: {x: {level: warn, value: required, types: [feat]}}"), msg), "the check does not apply to fix")
}

//go:embed testdata/extends-named-checks.yaml
var namedOverridePolicy string

// TestNamedChecks: one rule holds several checks of one type, each with its
// own level, and a preset check of a type is overridden by its name.
func TestNamedChecks(t *testing.T) {
	p := one(t, `pattern:
  no-wip: {level: error, location: header, value: '!/\bWIP\b/i'}
  issue-ref: {level: warn, location: message, value: '/#\d+/'}
`)
	assert.Equal(t, []string{
		`warn: pattern issue-ref: no match of /#\d+/ in the message`,
		`error: pattern no-wip: /\bWIP\b/i must not be in the header`,
	}, reasons(p, "feat: WIP x"), "checks of a type run in the order of their names")
	assert.Empty(t, reasons(p, "feat: x\n\nRefs: #1"))

	p, err := Parse([]byte(namedOverridePolicy), testSettings())
	require.NoError(t, err)
	assert.Equal(t, []string{"error: header-max-length short: the header is 80 characters, the limit is 72"},
		reasons(p, "feat: "+strings.Repeat("x", 74)), "the file changes the level of the preset check and keeps its value")
	assert.Equal(t, []string{
		"error: header-max-length hard: the header is 106 characters, the limit is 100",
		"error: header-max-length short: the header is 106 characters, the limit is 72",
	}, reasons(p, "feat: "+strings.Repeat("x", 100)))
}

// TestSignoffParagraph: git commit -s writes Signed-off-by into the paragraph
// of the other trailers. A required trailer takes the whole paragraph after
// the header when the body is not required and an empty line may precede the
// trailer; otherwise the trailer is the last line.
func TestSignoffParagraph(t *testing.T) {
	msg := "fix: round refunds\n\nRefs: #665\nSigned-off-by: A <a@b>\n"
	checks := func(body, leading string) string {
		return fmt.Sprintf(`body-leading-blank-lines: {x: {level: error, value: required}}
body-required: {x: {level: error, value: %s}}
trailer-leading-blank-lines: {x: {level: error, value: %s}}
trailer-required: {x: {level: error, value: required}}
pattern: {refs: {level: error, location: trailer, value: '/(?m)^Refs: #\d+$/'}}
`, body, leading)
	}
	assert.Empty(t, reasons(one(t, checks("optional", "required")), msg), "Refs and Signed-off-by are the trailer, the body is empty")
	assert.Equal(t, []string{"error: pattern refs: no match of /(?m)^Refs: #\\d+$/ in the trailer"},
		reasons(one(t, checks("required", "forbidden")), msg), "the trailer is the last line, Refs is the body")
}

// TestTrailerByType: trailer-required with types finds the trailer only in
// commits of these types; in others the last line is not a trailer.
func TestTrailerByType(t *testing.T) {
	p := one(t, `trailer-required: {x: {level: error, value: required, types: [feat]}}
body-required: {x: {level: error, value: required}}
`)
	assert.Equal(t, []string{"error: body-required x: the message needs a body"}, reasons(p, "feat: x\n\nRefs: #1"), "the last line of feat is the trailer")
	assert.Empty(t, reasons(p, "fix: x\n\nRefs: #1"), "in fix the same line is the body")
}

func TestPolicy_Invalid(t *testing.T) {
	rule := func(fields string) string {
		return "version: 1\nrules:\n  r: {match: all, message: m" + fields + "}\n"
	}
	cases := map[string]string{
		"version: 2\n":                                                                                  "version: 2",
		"version: 1\nmode: strict\n":                                                                    `mode: "strict"`,
		"version: 1\nrulesets: []\n":                                                                    `unknown field "rulesets"`,
		"version: 1\nrules:\n  Release: {match: all}\n":                                                 `rule name "Release" must match`,
		"version: 1\nrules:\n  r: {message: m}\n":                                                       "rule r: match: required",
		"version: 1\nrules:\n  r: {match: all}\n":                                                       "rule r: message: required",
		rule(", parser: conventional"):                                                                  `rule r: unknown field "parser"`,
		"version: 1\nrules:\n  r: {match: feature, message: m}\n":                                       `"feature" is not all, default or a list`,
		"version: 1\nrules:\n  r: {match: ['refs/heads/x'], message: m}\n":                              "without refs/heads/",
		"version: 1\nrules:\n  r: {match: all, message: " + strings.Repeat("m", 1001) + "}\n":           "message: 1001 characters",
		rule(", check_history: all"):                                                                    `check_history: "all" is not one of new-content, incoming`,
		rule(", except: {users: ['bad user']}"):                                                         "not a GitLab username",
		rule(", except: {merge_commits: true}"):                                                         `rule r: except: unknown field "merge_commits"`,
		rule(", checks: {type-enum: {x: {value: [a]}}}"):                                                "level: required",
		rule(", checks: {type-enum: {x: {level: off, value: [a]}}}"):                                    `level: "off" is not one of error, warn`,
		rule(", checks: {type-enum: {x: {level: error}}}"):                                              "checks.type-enum.x.value: a non-empty list is required",
		rule(", checks: {type-enum: {level: error, value: [a]}}"):                                       `checks.type-enum.level: expected a mapping, got a string`,
		rule(", checks: {type-enum: {Bad: {level: error, value: [a]}}}"):                                `check name "Bad" must match`,
		rule(", checks: {header-format: {x: {level: error}}}"):                                          "checks.header-format.x.value: required",
		rule(", checks: {header-format: {x: {level: error, value: forbidden}}}"):                        `"forbidden" is not one of optional, required`,
		rule(", checks: {header-format: {x: {level: error, value: required, types: [feat]}}}"):          `checks.header-format.x: unknown field "types"`,
		rule(", checks: {header-max-length: {x: {level: error, value: 0}}}"):                            "from 1 to 10000",
		rule(", checks: {body-max-length: {x: {level: error, value: 0}}}"):                              "from 1 to 10000",
		rule(", checks: {scope: {x: {level: error, value: maybe}}}"):                                    `checks.scope.x.value: "maybe" is not one of optional, required, forbidden`,
		rule(", checks: {trailer-required: {x: {level: error, value: [Signed-off-by]}}}"):               "checks.trailer-required.x.value: expected a string",
		rule(", checks: {body-min-line-length: {x: {level: error, value: 1}}}"):                         "", // an unknown type is left to Validate
		rule(", checks: {type-enum: {x: {level: error, value: [a], max: 1}}}"):                          `checks.type-enum.x: unknown field "max"`,
		rule(", checks: {pattern: {x: {level: error, value: '/x/'}}}"):                                  "location: required",
		rule(", checks: {pattern: {x: {level: error, location: body}}}"):                                "checks.pattern.x.value: required",
		rule(", checks: {pattern: {x: {level: error, location: trailer, value: '/x/', keys: [Refs]}}}"): `checks.pattern.x: unknown field "keys"`,
		rule(", checks: {pattern: {x: {level: error, location: body, value: '/(/'}}}"):                  "missing closing",
		rule(", checks: {pattern: {x: {level: error, location: body, value: 'PAY-1'}}}"):                "write a regex as /re/",
		rule(", checks: {pattern: {x: {level: error, location: body, value: '/(?=x)/'}}}"):              "not a valid RE2 regex",
		rule(", admins: [x]"):               `rule r: unknown field "admins"`,
		rule(", help: {docs_url: ftp://x}"): "is not an http(s) URL",
	}
	for in, want := range cases {
		_, err := Parse([]byte(in), testSettings())
		if want == "" {
			require.NoError(t, err, in)
			continue
		}
		require.Error(t, err, in)
		assert.Contains(t, err.Error(), want, in)
	}
}

//go:embed testdata/extends-override.yaml
var extendsOverridePolicy string

//go:embed testdata/extends-lift.yaml
var extendsLiftPolicy string

//go:embed testdata/partial-rule.yaml
var partialRulePolicy string

func TestExtends(t *testing.T) {
	p, err := Parse([]byte(extendsOverridePolicy), testSettings())
	require.NoError(t, err)
	assert.Empty(t, reasons(p, "chore: x"), "null removes the type-enum checks of the preset")
	assert.Equal(t, []string{"error: header-max-length short: the header is 37 characters, the limit is 20"},
		reasons(p, "feat: "+strings.Repeat("x", 31)), "the file overrides the level and the value")
	assert.Empty(t, p.Validate(t.Context(), nil))
	assert.Equal(t, []string{"preset:org", "file"}, p.rules[0].source)

	p, err = Parse([]byte(extendsLiftPolicy), testSettings())
	require.NoError(t, err)
	assert.Empty(t, reasons(p, "chore: x"), "null removes one named check of the preset")
	assert.Empty(t, reasons(p, "Update x"), "value: optional lifts the requirement of the preset")

	p, err = Parse([]byte("version: 1\nextends: org\n"), testSettings())
	require.NoError(t, err, "a file of version and extends is a whole policy")
	assert.Contains(t, strings.Join(reasons(p, "chore: x"), "\n"), `type-enum types: "chore"`)

	p, err = Parse([]byte("version: 1\nextends: missing\n"), testSettings())
	require.NoError(t, err, "a missing preset does not break the policy on the anchor")
	assert.Equal(t, `extends: preset "missing" is not defined in the server config`, p.Validate(t.Context(), nil)[0].Msg)
	assert.Empty(t, reasons(p, "anything"), "the module is skipped")
	p, err = Parse([]byte("version: 1\nextends: broken\n"), testSettings())
	require.NoError(t, err)
	assert.Contains(t, p.Validate(t.Context(), nil)[0].Msg, `preset "broken" of the server config is invalid`)
	assert.Equal(t, []string{`presets.commitpolicy.broken: unknown field "bogus"`}, New(testSettings()).PresetErrors())

	_, err = Parse([]byte(partialRulePolicy), testSettings())
	assert.ErrorContains(t, err, "rule conventional: match: required", "a partial rule without the preset")
}

func TestUnknownCheck(t *testing.T) {
	p, err := Parse([]byte("version: 1\nrules:\n  r: {match: all, message: m, checks: {future-check: {level: error, threshold: 3}, type-enum: {x: {level: error, value: [feat]}}}}\n"), testSettings())
	require.NoError(t, err, "the anchor version keeps working with the known checks")
	assert.Equal(t, []string{"future-check"}, p.rules[0].unknown)
	assert.NotEmpty(t, reasons(p, "fix: x"))
	problems := p.Validate(t.Context(), nil)
	require.Len(t, problems, 1)
	assert.Equal(t, `rule r: unknown check "future-check"`, problems[0].Msg, "a new version with an unknown check is rejected")
}

func TestMode(t *testing.T) {
	p, err := Parse([]byte("version: 1\nmode: warn\nrules:\n  r: {match: all, message: m, checks: {type-enum: {x: {level: error, value: [feat]}}}}\n"), testSettings())
	require.NoError(t, err)
	assert.Equal(t, []string{`warn: type-enum x: "fix" is not one of feat`}, reasons(p, "fix: x"))
	p = one(t, "type-enum: {x: {level: warn, value: [feat]}}")
	assert.Equal(t, []string{`warn: type-enum x: "fix" is not one of feat`}, reasons(p, "fix: x"))
}

func TestExcept_MergeAndRevert(t *testing.T) {
	p, err := Parse([]byte("version: 1\nrules:\n  r: {match: all, message: m, except: {merge_commit: true, git_reverts: true}, checks: {header-format: {x: {level: error, value: required}}, type-enum: {x: {level: error, value: [feat]}}}}\n"), testSettings())
	require.NoError(t, err)
	merge := []byte("tree t\nparent a\nparent b\nauthor x\n\nMerge branch 'feature' into 'main'\n")
	assert.Empty(t, p.LintCommit(t.Context(), merge))
	assert.Empty(t, p.LintMessage(t.Context(), "Revert \"feat: x\"\n\nThis reverts commit 0123456789abcdef.\n"))
	assert.Empty(t, p.LintMessage(t.Context(), "Revert \"Merge branch 'f' into 'master'\"\n\nThis reverts commit 0123456789abcdef, reversing\nchanges made to 89abcdef0123.\n"))
	assert.NotEmpty(t, p.LintMessage(t.Context(), "Revert \"feat: x\"\n\nno revert line"), "a revert header alone is not a git revert")
	strict := one(t, "header-format: {x: {level: error, value: required}}")
	assert.NotEmpty(t, strict.LintCommit(t.Context(), merge), "without except merge commits are checked")
}

// TestCommitizenSchema splits the commitizen schema of a real project into
// checks and a pattern: the schema asked for a type of a list, a subject of
// up to 100 characters and a ref trailer.
func TestCommitizenSchema(t *testing.T) {
	p := one(t, `header-format: {conventional: {level: error, value: required}}
header-max-length: {commitizen: {level: error, value: 100}}
type-enum: {commitizen: {level: error, value: [build, ci, docs, feat, fix, perf, refactor, style, test, chore, revert, bump]}}
body-leading-blank-lines: {commitizen: {level: error, value: required}}
trailer-required: {commitizen: {level: error, value: required}}
pattern: {ref: {level: error, location: trailer, value: '/(?mi)^(ref|refs|issue): [a-zA-Z0-9_.\/-]*#\d+$/'}}
`)
	cases := []struct {
		msg string
		ok  bool
	}{
		{"feat: add x\n\nref: group/project#1", true},
		{"feat: add x\n\nref: group/project#1\nSigned-off-by: A <a@b>", true},
		{"fix: x\n\nref: group/my.project#1", true},
		{"fix: x\n\nref: group/project#1\n", true},
		{"feat(: add x\n\nref: group/project#1", false},
		{"feat: \n\nref: group/project#1", false},
		{"feat: add x\nbody right after the header\nref: group/project#1", false},
		{"feat: add x", false},
		{"Merge branch 'f' into 'main'", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.ok, len(reasons(p, tc.msg)) == 0, "%q: %v", tc.msg, reasons(p, tc.msg))
	}
}

func FuzzParsePolicy(f *testing.F) {
	for _, s := range []string{
		"version: 1\nrules:\n  r: {match: all, message: m, checks: {type-enum: {x: {level: error, value: [feat]}}}}\n",
		"version: 1\nextends: org\nmode: warn\nrules:\n  r: {match: ['/^release\\//'], check_history: incoming, message: m}\n",
		"version: 1\nrules:\n  r: {match: default, message: m, checks: {pattern: {no-wip: {level: error, location: header, value: '!/^WIP/i'}}}}\n",
		"version: 1\nrules:\n  r: {match: all, message: m, checks: {trailer-required: {x: {level: warn, value: required}}, pattern: {refs: {level: warn, location: trailer, value: '/#[0-9]+/'}}}}\n",
	} {
		f.Add([]byte(s), "feat(x): y\n\nRefs: #1\n")
	}
	f.Fuzz(func(t *testing.T, data []byte, message string) {
		p, err := Parse(data, testSettings())
		if err != nil {
			return
		}
		_ = p.Validate(t.Context(), nil)
		for _, r := range p.rules {
			r.apply(r.parse(message))
		}
	})
}
