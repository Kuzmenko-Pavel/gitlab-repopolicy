package commit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSettings() *Settings {
	return NewSettings(map[string]any{
		"org": map[string]any{"rules": map[string]any{
			"conventional": map[string]any{"match": "all", "parser": "conventional", "message": "Commits follow Conventional Commits.",
				"checks": map[string]any{
					"type-enum":         map[string]any{"types": map[string]any{"level": "error", "value": []any{"feat", "fix"}}},
					"header-max-length": map[string]any{"short": map[string]any{"level": "warn", "value": 72}},
				}},
		}},
		"broken": map[string]any{"bogus": 1},
	})
}

// one parses a policy with one conventional rule and the given checks block.
func one(t *testing.T, checks string) *Policy {
	t.Helper()
	p, err := Parse([]byte("version: 1\nrules:\n  r:\n    match: all\n    parser: conventional\n    message: m\n    checks:\n"+indent(checks, 6)), testSettings())
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
		{"header-format", "header-format: {x: {level: error}}",
			[]string{"feat: x", "fix(a)!: y"}, []string{"Update x", "feat(): x", "feat:x"}, "error: header-format x: "},
		{"header-max-length", "header-max-length: {x: {level: error, value: 10}}",
			[]string{"feat: ядро", "fix: 12345"}, []string{"fix: 123456"}, "header-max-length x: the header is 11 characters, the limit is 10"},
		{"type-enum", "type-enum: {x: {level: error, value: [feat, fix]}}",
			[]string{"feat: x", "fix(a): y"}, []string{"feature: x"}, `error: type-enum x: "feature" is not one of feat, fix`},
		{"scope", "scope: {x: {level: error, value: required}}", []string{"feat(a): x"}, []string{"feat: x"}, "scope x: a scope is required"},
		{"scope-forbidden", "scope: {x: {level: error, value: forbidden}}", []string{"feat: x"}, []string{"feat(a): x"}, `scope x: a scope is not allowed, found "a"`},
		{"scope-optional", "scope: {x: {level: error, value: optional}}", []string{"feat: x", "feat(a): x"}, nil, ""},
		{"scope-enum", "scope-enum: {x: {level: warn, value: [api, ui, SPE, SPP]}}",
			[]string{"feat(api): x", "feat: no scope", "feat(SPE, SPP): x", "feat(SPE/SPP): x"}, []string{"feat(db): x"}, `warn: scope-enum x: "db" is not one of`},
		{"breaking-bang-forbidden", "breaking-bang: {x: {level: error, value: forbidden}}", []string{"feat: x"}, []string{"feat!: x"}, `breaking-bang x: "!" after the type is not allowed`},
		{"breaking-bang-required", "breaking-bang: {x: {level: error, value: required}}", []string{"feat!: x"}, []string{"feat: x"}, `breaking-bang x: "!" after the type is required`},
		{"breaking-bang-optional", "breaking-bang: {x: {level: error, value: optional}}", []string{"feat: x", "feat!: x"}, nil, ""},
		{"subject-starts-lowercase-required", "subject-starts-lowercase: {x: {level: error, value: required}}",
			[]string{"feat: add", "feat: ядро"}, []string{"feat: Add", "feat: Ядро", "feat: 2fa for login", `feat: "quoted" name`}, "subject-starts-lowercase x: the subject must start with a lowercase letter"},
		{"subject-starts-lowercase-forbidden", "subject-starts-lowercase: {x: {level: error, value: forbidden}}",
			[]string{"feat: Add", "feat: 2fa for login"}, []string{"feat: add"}, "the subject must not start with a lowercase letter"},
		{"subject-trailing-dot-forbidden", "subject-trailing-dot: {x: {level: error, value: forbidden}}", []string{"feat: x"}, []string{"feat: x."}, "subject-trailing-dot x: the subject must not end with a full stop"},
		{"subject-trailing-dot-required", "subject-trailing-dot: {x: {level: error, value: required}}", []string{"feat: x."}, []string{"feat: x"}, "the subject must end with a full stop"},
		{"body-leading-blank-lines-required", "body-leading-blank-lines: {x: {level: error, value: required}}",
			[]string{"feat: x\n\nwhy", "feat: x\n\n", "feat: x\n\nSigned-off-by: A <a@b>"}, []string{"feat: x\nwhy", "feat: x\n", "feat: x"},
			"body-leading-blank-lines x: an empty line must follow the header"},
		{"body-leading-blank-lines-forbidden", "body-leading-blank-lines: {x: {level: error, value: forbidden}}",
			[]string{"feat: x\nwhy", "feat: x"}, []string{"feat: x\n\nwhy"}, "the line after the header must not be empty"},
		{"body-min-line-length", "body-min-line-length: {x: {level: error, types: [feat]}}",
			[]string{"feat: x\n\nwhy", "feat: x\nwhy right after the header", "fix: no body"},
			[]string{"feat: x", "feat: x\n\nSigned-off-by: A <a@b>"}, "body-min-line-length x: the message needs a body"},
		{"body-min-line-length-20", "body-min-line-length: {x: {level: error, value: 20}}",
			[]string{"fix: x\n\nRetry.\nUse backoff of 2s.\n\nRefs: #123"}, []string{"fix: x\n\nRetry once.\n\nRefs: #123"}, "the body is 11 characters, at least 20 are required"},
		{"body-min-line-length-0", "body-min-line-length: {x: {level: error, value: 0}}", []string{"fix: x", "fix: x\n\nwhy"}, nil, ""},
		{"body-max-line-length", "body-max-line-length: {x: {level: warn, value: 20}}",
			[]string{"feat: x\n\nshort\n\n```\n" + strings.Repeat("c", 50) + "\n```\n    " + strings.Repeat("i", 50) + "\nsee https://example.com/" + strings.Repeat("u", 50)},
			[]string{"feat: x\n\n" + strings.Repeat("b", 21)}, "body-max-line-length x: body line 1 is 21 characters, the limit is 20"},
		{"trailer-required", "trailer-required: {x: {level: error, value: [Signed-off-by]}}",
			[]string{"feat: x\n\nsigned-off-by: A <a@b>"}, []string{"feat: x", "feat: x\n\nSigned-off-by: A <a@b>\nnot a trailer"}, "trailer-required x: missing Signed-off-by"},
		{"pattern-trailer", `pattern: {issue: {level: error, location: trailer, keys: [Issue, Refs], value: '/[A-Za-z0-9_.\/-]*#[0-9]+/'}}`,
			[]string{"fix: x\n\nIssue: group/my.project#1", "fix: x\n\nbody\n\nrefs: #12\nSigned-off-by: A <a@b>"},
			[]string{"fix: x\n\nsee group/p#1 in body", "fix: x\n\nOther: #1"}, `pattern issue: no match of /[A-Za-z0-9_.\/-]*#[0-9]+/ in a trailer Issue, Refs`},
		{"pattern-trailer-any-key", `pattern: {x: {level: error, location: trailer, value: '/#[0-9]+/'}}`,
			[]string{"fix: x\n\nOther: #1"}, []string{"fix: x #1"}, "in the trailer block"},
		{"pattern-body", `pattern: {x: {level: error, location: body, value: '/#[0-9]+/'}}`,
			[]string{"fix: x\n\nthe race (aws/agent#665) again"}, []string{"fix: x #1", "fix: x\n\nRefs: #1"}, "in the body"},
		{"pattern-message", `pattern: {x: {level: error, location: message, value: '/#[0-9]+/'}}`,
			[]string{"fix: x #1", "fix: x\n\nRefs: #1"}, []string{"fix: x"}, "in the message"},
		{"pattern-header", `pattern: {x: {level: error, location: header, value: '/^(feat|fix)(\(.+\))?: [a-z]/'}}`,
			[]string{"feat: ok", "fix(a): ok"}, []string{"feat: Bad"}, "in the header"},
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

// TestNamedChecks: one rule holds several checks of one type, each with its
// own level, and a preset check of a type is overridden by its name.
func TestNamedChecks(t *testing.T) {
	p := one(t, `pattern:
  no-wip: {level: error, location: header, value: '!/\bWIP\b/i'}
  issue-ref: {level: warn, location: trailer, keys: [Refs], value: '/^#\d+$/'}
`)
	assert.Equal(t, []string{
		`warn: pattern issue-ref: no match of /^#\d+$/ in a trailer Refs`,
		`error: pattern no-wip: /\bWIP\b/i must not be in the header`,
	}, reasons(p, "feat: WIP x"), "checks of a type run in the order of their names")
	assert.Empty(t, reasons(p, "feat: x\n\nRefs: #1"))

	p, err := Parse([]byte("version: 1\nextends: org\nrules:\n  conventional:\n    checks:\n      header-max-length:\n        short: {level: error}\n        hard: {level: error, value: 100}\n"), testSettings())
	require.NoError(t, err)
	assert.Equal(t, []string{"error: header-max-length short: the header is 80 characters, the limit is 72"},
		reasons(p, "feat: "+strings.Repeat("x", 74)), "the file changes the level of the preset check and keeps its value")
	assert.Equal(t, []string{
		"error: header-max-length hard: the header is 106 characters, the limit is 100",
		"error: header-max-length short: the header is 106 characters, the limit is 72",
	}, reasons(p, "feat: "+strings.Repeat("x", 100)))
}

// TestHeaderNotParsed: a header that is not conventional is reported only by
// header-format, the checks of its parts are skipped.
func TestHeaderNotParsed(t *testing.T) {
	p := one(t, `header-format: {x: {level: warn}}
type-enum: {x: {level: error, value: [feat, fix]}}
scope: {x: {level: error, value: required}}
subject-starts-lowercase: {x: {level: error, value: required}}
subject-trailing-dot: {x: {level: error, value: required}}
`)
	assert.Equal(t, []string{`warn: header-format x: expected "type(scope): subject"`}, reasons(p, "Fix timeout"))
	assert.Empty(t, one(t, "type-enum: {x: {level: error, value: [feat]}}").lint(commitInfo{message: "Fix timeout"}),
		"without header-format a header that is not conventional is not rejected")
	assert.Equal(t, []string{"error: header-format x: the scope in parentheses is empty"},
		reasons(one(t, "header-format: {x: {level: error}}"), "feat(): x"))
	assert.Equal(t, []string{`error: header-format x: expected "type(scope): subject", a space must follow ":"`},
		reasons(one(t, "header-format: {x: {level: error}}"), "feat:x"))
}

func TestParser(t *testing.T) {
	free := func(checks string) error {
		_, err := Parse([]byte("version: 1\nrules:\n  r:\n    match: all\n    parser: free\n    message: m\n    checks:\n"+indent(checks, 6)), testSettings())
		return err
	}
	for _, c := range []string{"header-format: {x: {level: error}}", "type-enum: {x: {level: error, value: [feat]}}",
		"scope: {x: {level: error, value: required}}", "scope-enum: {x: {level: error, value: [a]}}",
		"breaking-bang: {x: {level: error, value: forbidden}}", "header-max-length: {x: {level: error, value: 10, types: [feat]}}"} {
		assert.Error(t, free(c), c)
	}
	require.NoError(t, free("header-max-length: {x: {level: error, value: 20}}\nsubject-trailing-dot: {x: {level: error, value: forbidden}}"))
	p, err := Parse([]byte("version: 1\nrules:\n  r: {match: all, parser: free, message: m, checks: {subject-trailing-dot: {x: {level: error, value: forbidden}}}}\n"), testSettings())
	require.NoError(t, err)
	assert.Empty(t, reasons(p, "Update legal_names.py"))
	assert.NotEmpty(t, reasons(p, "Update legal_names.py."))

	p = one(t, "{}")
	assert.Empty(t, reasons(p, "Update x"), "the conventional parser alone checks nothing")
	assert.Equal(t, []string{"error: header is empty"}, reasons(p, "\nbody"), "a message without a header cannot be parsed")
	assert.Equal(t, []string{"error: message contains a NUL byte"}, reasons(p, "feat: x\x00"))
}

func TestPolicy_Invalid(t *testing.T) {
	rule := func(fields string) string {
		return "version: 1\nrules:\n  r: {match: all, parser: conventional, message: m" + fields + "}\n"
	}
	cases := map[string]string{
		"version: 2\n":                                                                                      "version: 2",
		"version: 1\nmode: strict\n":                                                                        `mode: "strict"`,
		"version: 1\nrulesets: []\n":                                                                        `unknown field "rulesets"`,
		"version: 1\nrules:\n  Release: {match: all}\n":                                                     `rule name "Release" must match`,
		"version: 1\nrules:\n  r: {parser: free, message: m}\n":                                             "rule r: match: required",
		"version: 1\nrules:\n  r: {match: all, message: m}\n":                                               "rule r: parser: required",
		"version: 1\nrules:\n  r: {match: all, parser: free}\n":                                             "rule r: message: required",
		rule(", format: free"):                                                                              `unknown field "format"`,
		"version: 1\nrules:\n  r: {match: feature, parser: free, message: m}\n":                             `"feature" is not all, default or a list`,
		"version: 1\nrules:\n  r: {match: ['refs/heads/x'], parser: free, message: m}\n":                    "without refs/heads/",
		"version: 1\nrules:\n  r: {match: all, parser: angular, message: m}\n":                              `parser: "angular"`,
		"version: 1\nrules:\n  r: {match: all, parser: free, message: " + strings.Repeat("m", 1001) + "}\n": "message: 1001 characters",
		rule(", check_history: all"):                                                                        `check_history: "all" is not one of new-content, incoming`,
		rule(", except: {users: ['bad user']}"):                                                             "not a GitLab username",
		rule(", except: {merge_commits: true}"):                                                             `rule r: except: unknown field "merge_commits"`,
		rule(", checks: {type-enum: {x: {value: [a]}}}"):                                                    "level: required",
		rule(", checks: {type-enum: {x: {level: off, value: [a]}}}"):                                        `level: "off" is not one of error, warn`,
		rule(", checks: {type-enum: {x: {level: error}}}"):                                                  "checks.type-enum.x.value: a non-empty list is required",
		rule(", checks: {type-enum: {x: {level: error, value: []}}}"):                                       "non-empty list",
		rule(", checks: {type-enum: {level: error, value: [a]}}"):                                           `checks.type-enum.level: expected a mapping, got a string`,
		rule(", checks: {type-enum: {Bad: {level: error, value: [a]}}}"):                                    `check name "Bad" must match`,
		rule(", checks: {header-max-length: {x: {level: error, value: 0}}}"):                                "from 1 to 10000",
		rule(", checks: {body-min-line-length: {x: {level: error, value: -1}}}"):                            "from 0 to 10000",
		rule(", checks: {scope: {x: {level: error, value: maybe}}}"):                                        `checks.scope.x.value: "maybe" is not one of optional, required, forbidden`,
		rule(", checks: {breaking-bang: {x: {level: error, value: forbid}}}"):                               `"forbid" is not one of optional, required, forbidden`,
		rule(", checks: {header-format: {x: {level: error, types: [feat]}}}"):                               `checks.header-format.x: unknown field "types"`,
		rule(", checks: {type-enum: {x: {level: error, value: [a], max: 1}}}"):                              `checks.type-enum.x: unknown field "max"`,
		rule(", checks: {pattern: {x: {level: error, value: '/x/'}}}"):                                      "location: required",
		rule(", checks: {pattern: {x: {level: error, location: body}}}"):                                    "checks.pattern.x.value: required",
		rule(", checks: {pattern: {x: {level: error, location: body, value: '/(/'}}}"):                      "missing closing",
		rule(", checks: {pattern: {x: {level: error, location: body, value: 'PAY-1'}}}"):                    "write a regex as /re/",
		rule(", checks: {pattern: {x: {level: error, location: body, value: '/x/', keys: [a]}}}"):           "only for location: trailer",
		rule(", checks: {pattern: {x: {level: error, location: body, value: '/(?=x)/'}}}"):                  "not a valid RE2 regex",
		rule(", checks: {reference: {x: {level: error}}}"):                                                  "", // an unknown type is left to Validate
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

func TestExtends(t *testing.T) {
	p, err := Parse([]byte("version: 1\nextends: org\nrules:\n  conventional:\n    checks:\n      header-max-length:\n        short: {level: error, value: 20}\n      type-enum: null\n"), testSettings())
	require.NoError(t, err)
	assert.Empty(t, reasons(p, "chore: x"), "null removes the type-enum checks of the preset")
	assert.Equal(t, []string{"error: header-max-length short: the header is 37 characters, the limit is 20"},
		reasons(p, "feat: "+strings.Repeat("x", 31)), "the file overrides the level and the value")
	assert.Empty(t, p.Validate(t.Context(), nil))
	assert.Equal(t, []string{"preset:org", "file"}, p.rules[0].source)

	p, err = Parse([]byte("version: 1\nextends: org\nrules:\n  conventional:\n    checks:\n      type-enum:\n        types: null\n"), testSettings())
	require.NoError(t, err)
	assert.Empty(t, reasons(p, "chore: x"), "null removes one named check of the preset")

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

	_, err = Parse([]byte("version: 1\nrules:\n  conventional:\n    checks:\n      body-min-line-length:\n        x: {level: error}\n"), testSettings())
	assert.ErrorContains(t, err, "rule conventional: match: required", "a partial rule without the preset")
}

func TestUnknownCheck(t *testing.T) {
	p, err := Parse([]byte("version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {future-check: {level: error, threshold: 3}, type-enum: {x: {level: error, value: [feat]}}}}\n"), testSettings())
	require.NoError(t, err, "the anchor version keeps working with the known checks")
	assert.Equal(t, []string{"future-check"}, p.rules[0].unknown)
	assert.NotEmpty(t, reasons(p, "fix: x"))
	problems := p.Validate(t.Context(), nil)
	require.Len(t, problems, 1)
	assert.Equal(t, `rule r: unknown check "future-check"`, problems[0].Msg, "a new version with an unknown check is rejected")
}

func TestMode(t *testing.T) {
	p, err := Parse([]byte("version: 1\nmode: warn\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {type-enum: {x: {level: error, value: [feat]}}}}\n"), testSettings())
	require.NoError(t, err)
	assert.Equal(t, []string{`warn: type-enum x: "fix" is not one of feat`}, reasons(p, "fix: x"))
	p = one(t, "type-enum: {x: {level: warn, value: [feat]}}")
	assert.Equal(t, []string{`warn: type-enum x: "fix" is not one of feat`}, reasons(p, "fix: x"))
}

func TestExcept_MergeAndRevert(t *testing.T) {
	p, err := Parse([]byte("version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, except: {merge_commit: true, git_reverts: true}, checks: {header-format: {x: {level: error}}, type-enum: {x: {level: error, value: [feat]}}}}\n"), testSettings())
	require.NoError(t, err)
	merge := []byte("tree t\nparent a\nparent b\nauthor x\n\nMerge branch 'feature' into 'main'\n")
	assert.Empty(t, p.LintCommit(t.Context(), merge))
	assert.Empty(t, p.LintMessage(t.Context(), "Revert \"feat: x\"\n\nThis reverts commit 0123456789abcdef.\n"))
	assert.Empty(t, p.LintMessage(t.Context(), "Revert \"Merge branch 'f' into 'master'\"\n\nThis reverts commit 0123456789abcdef, reversing\nchanges made to 89abcdef0123.\n"))
	assert.NotEmpty(t, p.LintMessage(t.Context(), "Revert \"feat: x\"\n\nno revert line"), "a revert header alone is not a git revert")
	strict := one(t, "header-format: {x: {level: error}}")
	assert.NotEmpty(t, strict.LintCommit(t.Context(), merge), "without except merge commits are checked")
}

// TestCommitizenSchema splits the commitizen schema of a real project into
// the conventional parser, checks and a pattern: the schema asked for a
// type of a list, a subject of up to 72 characters and a ref trailer.
func TestCommitizenSchema(t *testing.T) {
	p := one(t, `header-format: {conventional: {level: error}}
header-max-length: {commitizen: {level: error, value: 100}}
type-enum: {commitizen: {level: error, value: [build, ci, docs, feat, fix, perf, refactor, style, test, chore, revert, bump]}}
body-leading-blank-lines: {commitizen: {level: error, value: required}}
pattern: {ref: {level: error, location: trailer, keys: [ref, refs, issue], value: '/^[a-zA-Z0-9_.\/-]*#\d+$/'}}
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
		"version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {type-enum: {x: {level: error, value: [feat]}}}}\n",
		"version: 1\nextends: org\nmode: warn\nrules:\n  r: {match: ['/^release\\//'], check_history: incoming, parser: free, message: m}\n",
		"version: 1\nrules:\n  r: {match: default, parser: free, message: m, checks: {pattern: {no-wip: {level: error, location: header, value: '!/^WIP/i'}}}}\n",
		"version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {pattern: {refs: {level: warn, location: trailer, keys: [Refs], value: '/#[0-9]+/'}}}}\n",
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
			r.apply(ParseMessage(message, r.conventional))
		}
	})
}
