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
					"type-enum":         map[string]any{"level": "error", "value": []any{"feat", "fix"}},
					"header-max-length": map[string]any{"level": "warn", "value": 72},
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
		{"type-enum", "type-enum: {level: error, value: [feat, fix]}",
			[]string{"feat: x", "fix(a): y"}, []string{"feature: x"}, `error: type-enum: "feature" is not one of feat, fix`},
		{"scope", "scope: {level: error, value: required}", []string{"feat(a): x"}, []string{"feat: x"}, "scope: a scope is required"},
		{"scope-forbidden", "scope: {level: error, value: forbidden}", []string{"feat: x"}, []string{"feat(a): x"}, `scope: a scope is not allowed, found "a"`},
		{"scope-enum", "scope-enum: {level: warn, value: [api, ui, SPE, SPP]}",
			[]string{"feat(api): x", "feat: no scope", "feat(SPE, SPP): x", "feat(SPE/SPP): x"}, []string{"feat(db): x"}, `warn: scope-enum: "db" is not one of`},
		{"breaking-bang", "breaking-bang: {level: error, value: forbid}", []string{"feat: x"}, []string{"feat!: x"}, `breaking-bang: "!" after the type is not allowed`},
		{"header-max-length", "header-max-length: {level: error, value: 10}",
			[]string{"feat: ядро", "fix: 12345"}, []string{"fix: 123456"}, "the header is 11 characters, the limit is 10"},
		{"subject-case", "subject-case: {level: error, value: lower-first}", []string{"feat: add", "feat: 1st", "feat: ядро"}, []string{"feat: Add", "feat: Ядро"}, "must start with a lowercase letter"},
		{"subject-case-sentence", "subject-case: {level: error, value: sentence}", []string{"feat: Add"}, []string{"feat: add"}, "must start with an uppercase letter"},
		{"subject-full-stop", "subject-full-stop: {level: error, value: forbid}", []string{"feat: x"}, []string{"feat: x."}, "must not end with a full stop"},
		{"subject-full-stop-require", "subject-full-stop: {level: error, value: require}", []string{"feat: x."}, []string{"feat: x"}, "must end with a full stop"},
		{"body-required", "body-required: {level: error, types: [feat]}", []string{"feat: x\n\nwhy", "fix: no body"}, []string{"feat: x", "feat: x\n\nSigned-off-by: A <a@b>"}, "body-required"},
		{"body-max-line-length", "body-max-line-length: {level: warn, value: 20}",
			[]string{"feat: x\n\nshort\n\n```\n" + strings.Repeat("c", 50) + "\n```\n    " + strings.Repeat("i", 50) + "\nsee https://example.com/" + strings.Repeat("u", 50)},
			[]string{"feat: x\n\n" + strings.Repeat("b", 21)}, "body line 1 is 21 characters, the limit is 20"},
		{"trailer-required", "trailer-required: {level: error, value: [Signed-off-by]}",
			[]string{"feat: x\n\nsigned-off-by: A <a@b>"}, []string{"feat: x", "feat: x\n\nSigned-off-by: A <a@b>\nnot a trailer"}, "missing Signed-off-by"},
		{"pattern-trailer", `pattern: {level: error, location: trailer, keys: [Issue, Refs], value: '/[A-Za-z0-9_.\/-]*#[0-9]+/'}`,
			[]string{"fix: x\n\nIssue: group/my.project#1", "fix: x\n\nbody\n\nrefs: #12\nSigned-off-by: A <a@b>"},
			[]string{"fix: x\n\nsee group/p#1 in body", "fix: x\n\nOther: #1"}, `pattern: no match of /[A-Za-z0-9_.\/-]*#[0-9]+/ in a trailer Issue, Refs`},
		{"pattern-trailer-any-key", `pattern: {level: error, location: trailer, value: '/#[0-9]+/'}`,
			[]string{"fix: x\n\nOther: #1"}, []string{"fix: x #1"}, "in the trailer block"},
		{"pattern-body", `pattern: {level: error, location: body, value: '/#[0-9]+/'}`,
			[]string{"fix: x\n\nthe race (aws/agent#665) again"}, []string{"fix: x #1", "fix: x\n\nRefs: #1"}, "in the body"},
		{"pattern-message", `pattern: {level: error, location: message, value: '/#[0-9]+/'}`,
			[]string{"fix: x #1", "fix: x\n\nRefs: #1"}, []string{"fix: x"}, "in the message"},
		{"pattern-header", `pattern: {level: error, location: header, value: '/^(feat|fix)(\(.+\))?: [a-z]/'}`,
			[]string{"feat: ok", "fix(a): ok"}, []string{"feat: Bad"}, "in the header"},
		{"pattern-header-not", `pattern: {level: error, location: header, value: '!/^(WIP|fixup!)/i'}`,
			[]string{"feat: ok", "feat: wip later"}, []string{"WIP: x", "wip: x", "fixup! feat: x"}, `pattern: /^(WIP|fixup!)/i must not be in the header`},
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

func TestParser(t *testing.T) {
	free := func(checks string) error {
		_, err := Parse([]byte("version: 1\nrules:\n  r:\n    match: all\n    parser: free\n    message: m\n    checks:\n"+indent(checks, 6)), testSettings())
		return err
	}
	for _, c := range []string{"type-enum: {level: error, value: [feat]}", "scope: {level: error, value: required}",
		"scope-enum: {level: error, value: [a]}", "breaking-bang: {level: error, value: forbid}",
		"header-max-length: {level: error, value: 10, types: [feat]}"} {
		assert.Error(t, free(c), c)
	}
	require.NoError(t, free("header-max-length: {level: error, value: 20}\nsubject-full-stop: {level: error, value: forbid}"))
	p, err := Parse([]byte("version: 1\nrules:\n  r: {match: all, parser: free, message: m, checks: {subject-full-stop: {level: error, value: forbid}}}\n"), testSettings())
	require.NoError(t, err)
	assert.Empty(t, reasons(p, "Update legal_names.py"))
	assert.NotEmpty(t, reasons(p, "Update legal_names.py."))

	p = one(t, "{}")
	assert.Equal(t, []string{`error: header: expected "type(scope): subject"`}, reasons(p, "Update x"),
		"a message the parser cannot read is an error without any check")
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
		rule(", except: {users: ['bad user']}"):                                                             "not a GitLab username",
		rule(", except: {merge_commits: true}"):                                                             `rule r: except: unknown field "merge_commits"`,
		rule(", checks: {type-enum: {value: [a]}}"):                                                         "level: required",
		rule(", checks: {type-enum: {level: off, value: [a]}}"):                                             `level: "off" is not one of error, warn`,
		rule(", checks: {type-enum: {level: error}}"):                                                       "checks.type-enum.value: a non-empty list is required",
		rule(", checks: {type-enum: {level: error, value: []}}"):                                            "non-empty list",
		rule(", checks: {header-max-length: {level: error, value: 0}}"):                                     "from 1 to 10000",
		rule(", checks: {scope: {level: error, value: maybe}}"):                                             `checks.scope.value: "maybe" is not one of optional, required, forbidden`,
		rule(", checks: {body-required: {level: error, value: true}}"):                                      `checks.body-required: unknown field "value"`,
		rule(", checks: {type-enum: {level: error, value: [a], max: 1}}"):                                   `checks.type-enum: unknown field "max"`,
		rule(", checks: {pattern: {level: error, value: '/x/'}}"):                                           "location: required",
		rule(", checks: {pattern: {level: error, location: body}}"):                                         "checks.pattern.value: required",
		rule(", checks: {pattern: {level: error, location: body, value: '/(/'}}"):                           "missing closing",
		rule(", checks: {pattern: {level: error, location: body, value: 'PAY-1'}}"):                         "write a regex as /re/",
		rule(", checks: {pattern: {level: error, location: body, value: '/x/', keys: [a]}}"):                "only for location: trailer",
		rule(", checks: {pattern: {level: error, location: body, value: '/(?=x)/'}}"):                       "not a valid RE2 regex",
		rule(", checks: {reference: {level: error}}"):                                                       "", // an unknown check is left to Validate
		rule(", admins: [x]"):                                                                               `rule r: unknown field "admins"`,
		rule(", help: {docs_url: ftp://x}"):                                                                 "is not an http(s) URL",
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
	p, err := Parse([]byte("version: 1\nextends: org\nrules:\n  conventional:\n    checks:\n      header-max-length: {level: error, value: 20}\n      type-enum: null\n"), testSettings())
	require.NoError(t, err)
	assert.Empty(t, reasons(p, "chore: x"), "null removes the type-enum check of the preset")
	assert.Equal(t, []string{"error: header-max-length: the header is 37 characters, the limit is 20"},
		reasons(p, "feat: "+strings.Repeat("x", 31)), "the file overrides the level and the value")
	assert.Empty(t, p.Validate(t.Context(), nil))
	assert.Equal(t, []string{"preset:org", "file"}, p.rules[0].source)

	p, err = Parse([]byte("version: 1\nextends: org\n"), testSettings())
	require.NoError(t, err, "a file of version and extends is a whole policy")
	assert.Contains(t, strings.Join(reasons(p, "chore: x"), "\n"), `type-enum: "chore"`)

	p, err = Parse([]byte("version: 1\nextends: missing\n"), testSettings())
	require.NoError(t, err, "a missing preset does not break the policy on the anchor")
	assert.Equal(t, `extends: preset "missing" is not defined in the server config`, p.Validate(t.Context(), nil)[0].Msg)
	assert.Empty(t, reasons(p, "anything"), "the module is skipped")
	p, err = Parse([]byte("version: 1\nextends: broken\n"), testSettings())
	require.NoError(t, err)
	assert.Contains(t, p.Validate(t.Context(), nil)[0].Msg, `preset "broken" of the server config is invalid`)
	assert.Equal(t, []string{`presets.commitpolicy.broken: unknown field "bogus"`}, New(testSettings()).PresetErrors())

	_, err = Parse([]byte("version: 1\nrules:\n  conventional:\n    checks:\n      body-required: {level: error}\n"), testSettings())
	assert.ErrorContains(t, err, "rule conventional: match: required", "a partial rule without the preset")
}

func TestUnknownCheck(t *testing.T) {
	p, err := Parse([]byte("version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {future-check: {level: error, threshold: 3}, type-enum: {level: error, value: [feat]}}}\n"), testSettings())
	require.NoError(t, err, "the anchor version keeps working with the known checks")
	assert.Equal(t, []string{"future-check"}, p.rules[0].unknown)
	assert.NotEmpty(t, reasons(p, "fix: x"))
	problems := p.Validate(t.Context(), nil)
	require.Len(t, problems, 1)
	assert.Equal(t, `rule r: unknown check "future-check"`, problems[0].Msg, "a new version with an unknown check is rejected")
}

func TestMode(t *testing.T) {
	p, err := Parse([]byte("version: 1\nmode: warn\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {type-enum: {level: error, value: [feat]}}}\n"), testSettings())
	require.NoError(t, err)
	assert.Equal(t, []string{`warn: type-enum: "fix" is not one of feat`}, reasons(p, "fix: x"))
	p = one(t, "type-enum: {level: warn, value: [feat]}")
	assert.Equal(t, []string{`warn: type-enum: "fix" is not one of feat`}, reasons(p, "fix: x"))
}

func TestExcept_MergeAndRevert(t *testing.T) {
	p, err := Parse([]byte("version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, except: {merge_commit: true, git_reverts: true}, checks: {type-enum: {level: error, value: [feat]}}}\n"), testSettings())
	require.NoError(t, err)
	merge := []byte("tree t\nparent a\nparent b\nauthor x\n\nMerge branch 'feature' into 'main'\n")
	assert.Empty(t, p.LintCommit(t.Context(), merge))
	assert.Empty(t, p.LintMessage(t.Context(), "Revert \"feat: x\"\n\nThis reverts commit 0123456789abcdef.\n"))
	assert.Empty(t, p.LintMessage(t.Context(), "Revert \"Merge branch 'f' into 'master'\"\n\nThis reverts commit 0123456789abcdef, reversing\nchanges made to 89abcdef0123.\n"))
	assert.NotEmpty(t, p.LintMessage(t.Context(), "Revert \"feat: x\"\n\nno revert line"), "a revert header alone is not a git revert")
	strict := one(t, "type-enum: {level: error, value: [feat]}")
	assert.NotEmpty(t, strict.LintCommit(t.Context(), merge), "without except merge commits are checked")
}

// TestCommitizenSchema splits the commitizen schema of a real project into
// the conventional parser, checks and a pattern: the schema asked for a
// type of a list, a subject of up to 72 characters and a ref trailer.
func TestCommitizenSchema(t *testing.T) {
	p := one(t, `type-enum: {level: error, value: [build, ci, docs, feat, fix, perf, refactor, style, test, chore, revert, bump]}
header-max-length: {level: error, value: 100}
pattern: {level: error, location: trailer, keys: [ref, refs, issue], value: '/^[a-zA-Z0-9_.\/-]*#\d+$/'}
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
		"version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {type-enum: {level: error, value: [feat]}}}\n",
		"version: 1\nextends: org\nmode: warn\nrules:\n  r: {match: ['/^release\\//'], parser: free, message: m}\n",
		"version: 1\nrules:\n  r: {match: default, parser: free, message: m, checks: {pattern: {level: error, location: header, value: '!/^WIP/i'}}}\n",
		"version: 1\nrules:\n  r: {match: all, parser: conventional, message: m, checks: {pattern: {level: warn, location: trailer, keys: [Refs], value: '/#[0-9]+/'}}}\n",
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
