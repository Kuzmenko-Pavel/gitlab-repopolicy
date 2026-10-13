package commit

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSettings() *Settings {
	return NewSettings(map[string]any{
		"org": map[string]any{"rules": map[string]any{
			"type-enum":         map[string]any{"level": "error", "value": []any{"feat", "fix"}},
			"header-max-length": map[string]any{"level": "warn", "value": 72},
		}},
		"broken": map[string]any{"bogus": 1},
	}, 100*time.Millisecond, 2*time.Second, time.Now)
}

// one parses a policy with one conventional ruleset and the given rules block.
func one(t *testing.T, rules string) *policy {
	t.Helper()
	p, err := parsePolicy([]byte("version: 1\nrulesets:\n  - name: r\n    rules:\n"+indent(rules, 6)), testSettings())
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

func reasons(p *policy, msg string) []string {
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

func TestRule(t *testing.T) {
	tests := []struct {
		name, rules string
		ok, bad     []string
		want        string
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
		{"reference", `reference: {level: error, location: trailer, keys: [Issue, Refs], pattern: "[A-Za-z0-9_./-]*#[0-9]+"}`,
			[]string{"fix: x\n\nIssue: group/my.project#1", "fix: x\n\nbody\n\nrefs: #12\nSigned-off-by: A <a@b>"},
			[]string{"fix: x\n\nsee group/p#1 in body", "fix: x\n\nOther: #1"}, "no reference matching"},
		{"reference-body", `reference: {level: error, location: body, pattern: "#[0-9]+"}`,
			[]string{"fix: x\n\nthe race (aws/agent#665) again"}, []string{"fix: x #1", "fix: x\n\nRefs: #1"}, "in the body"},
		{"reference-any", `reference: {level: error, location: any, pattern: "#[0-9]+"}`,
			[]string{"fix: x #1", "fix: x\n\nRefs: #1"}, []string{"fix: x"}, "in the message"},
		{"trailer-required", "trailer-required: {level: error, value: [Signed-off-by]}",
			[]string{"feat: x\n\nsigned-off-by: A <a@b>"}, []string{"feat: x", "feat: x\n\nSigned-off-by: A <a@b>\nnot a trailer"}, "missing Signed-off-by"},
		{"pattern", `pattern: {level: error, value: '(?P<type>feat|fix): [a-z].*$'}`,
			[]string{"feat: ok", "fix: ok\n"}, []string{"chore: x", "feat: Bad"}, "does not match the required format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := one(t, tt.rules)
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
	t.Run("off", func(t *testing.T) {
		assert.Empty(t, reasons(one(t, "type-enum: {level: off, value: [feat]}"), "chore: x"))
	})
}

func TestFormatFree(t *testing.T) {
	free := func(rules string) error {
		_, err := parsePolicy([]byte("version: 1\nrulesets:\n  - name: r\n    format: free\n    rules:\n"+indent(rules, 6)), testSettings())
		return err
	}
	for _, r := range []string{"type-enum: {level: error, value: [feat]}", "scope: {level: error, value: required}",
		"scope-enum: {level: error, value: [a]}", "breaking-bang: {level: error, value: forbid}",
		"header-max-length: {level: error, value: 10, types: [feat]}"} {
		assert.Error(t, free(r), r)
	}
	require.NoError(t, free("header-max-length: {level: error, value: 20}\nsubject-full-stop: {level: error, value: forbid}"))
	p, err := parsePolicy([]byte("version: 1\nrulesets:\n  - name: r\n    format: free\n    rules: {subject-full-stop: {level: error, value: forbid}}\n"), testSettings())
	require.NoError(t, err)
	assert.Empty(t, reasons(p, "Update legal_names.py"))
	assert.NotEmpty(t, reasons(p, "Update legal_names.py."))
}

func TestPolicy_Invalid(t *testing.T) {
	cases := map[string]string{
		"version: 2\nrulesets: [{name: r}]\n":                                                                            "version: 2",
		"version: 1\nmode: strict\nrulesets: [{name: r}]\n":                                                              `mode: "strict"`,
		"version: 1\nrulesets: []\n":                                                                                     "at least one ruleset",
		"version: 1\nrulesets: [{name: r}, {name: r}]\n":                                                                 "used twice",
		"version: 1\nrulesets: [{refs: all}]\n":                                                                          "name is required",
		"version: 1\nrulesets: [{name: r, refs: feature}]\n":                                                             `refs: "feature"`,
		"version: 1\nrulesets: [{name: r, refs: [heads/x]}]\n":                                                           "not a ref pattern",
		"version: 1\nrulesets: [{name: r, refs: ['refs/heads/[']}]\n":                                                    "syntax error",
		"version: 1\nrulesets: [{name: r, format: angular}]\n":                                                           `format: "angular"`,
		"version: 1\nrulesets: [{name: r, except: {users: ['bad user']}}]\n":                                             "not a GitLab username",
		"version: 1\nrulesets: [{name: r, except: {bots: true}}]\n":                                                      "field bots not found",
		"version: 1\nrulesets: [{name: r, rules: {type-enum: {value: [a]}}}]\n":                                          "level: required",
		"version: 1\nrulesets: [{name: r, rules: {type-enum: {level: high, value: [a]}}}]\n":                             `level: "high"`,
		"version: 1\nrulesets: [{name: r, rules: {type-enum: {level: error}}}]\n":                                        "value: required",
		"version: 1\nrulesets: [{name: r, rules: {type-enum: {level: error, value: []}}}]\n":                             "non-empty list",
		"version: 1\nrulesets: [{name: r, rules: {header-max-length: {level: error, value: 0}}}]\n":                      "from 1 to 10000",
		"version: 1\nrulesets: [{name: r, rules: {scope: {level: error, value: maybe}}}]\n":                              "expected one of optional, required, forbidden",
		"version: 1\nrulesets: [{name: r, rules: {body-required: {level: error, value: true}}}]\n":                       `unknown key "value"`,
		"version: 1\nrulesets: [{name: r, rules: {type-enum: {level: error, value: [a], max: 1}}}]\n":                    `unknown key "max"`,
		"version: 1\nrulesets: [{name: r, rules: {reference: {level: error, pattern: x}}}]\n":                            "location: required",
		"version: 1\nrulesets: [{name: r, rules: {reference: {level: error, location: body}}}]\n":                        "pattern: a regular expression",
		"version: 1\nrulesets: [{name: r, rules: {reference: {level: error, location: body, pattern: '('}}}]\n":          "missing closing",
		"version: 1\nrulesets: [{name: r, rules: {reference: {level: error, location: body, pattern: x, keys: [a]}}}]\n": "only for location: trailer",
		"version: 1\nrulesets: [{name: r, rules: {pattern: {level: error, value: '(?<'}}}]\n":                            "value:",
		"version: 1\nrulesets: [{name: r, admins: [x]}]\n":                                                               "field admins not found",
	}
	for in, want := range cases {
		_, err := parsePolicy([]byte(in), testSettings())
		require.Error(t, err, in)
		assert.Contains(t, err.Error(), want, in)
	}
}

func TestExtends(t *testing.T) {
	p, err := parsePolicy([]byte("version: 1\nextends: org\nrulesets:\n  - name: r\n    rules:\n      header-max-length: {level: error, value: 20}\n"), testSettings())
	require.NoError(t, err)
	assert.Contains(t, strings.Join(reasons(p, "chore: x"), "\n"), `type-enum: "chore"`, "preset rule applies")
	assert.Contains(t, strings.Join(reasons(p, "feat: "+strings.Repeat("x", 30)), "\n"), "error: header-max-length", "file rule overrides the preset level and value")
	assert.Empty(t, p.Validate(t.Context(), nil))

	p, err = parsePolicy([]byte("version: 1\nextends: missing\nrulesets: [{name: r}]\n"), testSettings())
	require.NoError(t, err, "an unknown preset does not break the policy on the anchor")
	assert.Contains(t, p.Validate(t.Context(), nil)[0].Msg, `preset "missing" is not defined`)
	p, err = parsePolicy([]byte("version: 1\nextends: broken\nrulesets: [{name: r}]\n"), testSettings())
	require.NoError(t, err)
	assert.Contains(t, p.Validate(t.Context(), nil)[0].Msg, `preset "broken" of the server config is invalid`)
	require.Len(t, testSettings().PresetErrors(), 1)
	assert.Contains(t, testSettings().PresetErrors()[0], "commitpolicy_presets.broken: line 1: field bogus not found")

	free, err := parsePolicy([]byte("version: 1\nextends: org\nrulesets: [{name: r, format: free}]\n"), testSettings())
	require.NoError(t, err, "conventional-only rules of a preset are not applied to a free ruleset")
	assert.Empty(t, reasons(free, "Anything"))
}

func TestUnknownRule(t *testing.T) {
	p, err := parsePolicy([]byte("version: 1\nrulesets: [{name: r, rules: {future-rule: {level: error, threshold: 3}, type-enum: {level: error, value: [feat]}}}]\n"), testSettings())
	require.NoError(t, err, "the anchor version keeps working with the known rules")
	assert.Equal(t, []string{"future-rule"}, p.rulesets[0].unknown)
	assert.NotEmpty(t, reasons(p, "fix: x"))
	problems := p.Validate(t.Context(), nil)
	require.Len(t, problems, 1)
	assert.Equal(t, `ruleset r: unknown rule "future-rule"`, problems[0].Msg, "a new version with an unknown rule is rejected")
}

func TestMode(t *testing.T) {
	p, err := parsePolicy([]byte("version: 1\nmode: warn\nrulesets: [{name: r, rules: {type-enum: {level: error, value: [feat]}}}]\n"), testSettings())
	require.NoError(t, err)
	assert.Equal(t, []string{`warn: type-enum: "fix" is not one of feat`}, reasons(p, "fix: x"))
	p = one(t, "type-enum: {level: warn, value: [feat]}")
	assert.Equal(t, []string{`warn: type-enum: "fix" is not one of feat`}, reasons(p, "fix: x"))
}

func TestExcept_MergeAndRevert(t *testing.T) {
	p, err := parsePolicy([]byte("version: 1\nrulesets: [{name: r, except: {merge_commits: true, git_reverts: true}, rules: {type-enum: {level: error, value: [feat]}}}]\n"), testSettings())
	require.NoError(t, err)
	merge := []byte("tree t\nparent a\nparent b\nauthor x\n\nMerge branch 'feature' into 'main'\n")
	assert.Empty(t, p.LintCommit(t.Context(), merge))
	assert.Empty(t, p.LintMessage(t.Context(), "Revert \"feat: x\"\n\nThis reverts commit 0123456789abcdef.\n"))
	assert.Empty(t, p.LintMessage(t.Context(), "Revert \"Merge branch 'f' into 'master'\"\n\nThis reverts commit 0123456789abcdef, reversing\nchanges made to 89abcdef0123.\n"))
	assert.NotEmpty(t, p.LintMessage(t.Context(), "Revert \"feat: x\"\n\nno revert line"), "a revert header alone is not a git revert")
	strict := one(t, "type-enum: {level: error, value: [feat]}")
	assert.NotEmpty(t, strict.LintCommit(t.Context(), merge), "without except merge commits are checked")
}

func TestPattern_CompileRE2(t *testing.T) {
	p := one(t, `pattern: {level: error, value: '(?=[\s\S]*Signed-off-by:)(?P<type>feat|fix)(\((?P<scope>[^()]+)\))?: .+'}`)
	assert.Empty(t, reasons(p, "feat(api): x\n\nSigned-off-by: A <a@b>"), "Python groups and lookahead work")
	assert.NotEmpty(t, reasons(p, "feat(api): x"))
}

func TestPattern_Timeout(t *testing.T) {
	p, err := parsePolicy([]byte("version: 1\nrulesets: [{name: r, rules: {pattern: {level: warn, value: '(a+)+b'}}}]\n"),
		NewSettings(nil, 100*time.Millisecond, 2*time.Second, time.Now))
	require.NoError(t, err)
	start := time.Now()
	got := p.rulesets[0].check(ParseMessage(strings.Repeat("a", 5000)+"c", false), nil, 100*time.Millisecond)
	assert.Less(t, time.Since(start), 2*time.Second)
	require.Len(t, got, 1)
	assert.True(t, got[0].timeout)
	assert.Equal(t, levelWarn, got[0].level, "commit_check_timeout carries the level of the pattern rule")
}

func TestPattern_CatastrophicBacktracking(t *testing.T) {
	r := one(t, `pattern: {level: error, value: '^(\w+\s?)*$'}`).rulesets[0].rules[0]
	_, err := r.matchPattern(strings.Repeat("word ", 4000)+"!", 5*time.Second)
	require.Error(t, err, "the backtracking stack limit or the timeout stops the match")
}

func TestPattern_Budget(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { now = now.Add(600 * time.Millisecond); return now }
	p, err := parsePolicy([]byte("version: 1\nrulesets: [{name: r, rules: {pattern: {level: error, value: 'feat: .*'}}}]\n"),
		NewSettings(nil, 100*time.Millisecond, time.Second, clock))
	require.NoError(t, err)
	b := &budget{left: time.Second, clock: clock}
	var timeouts int
	for range 5 {
		for _, f := range p.rulesets[0].check(ParseMessage("feat: ok", true), b, 100*time.Millisecond) {
			if f.timeout {
				timeouts++
				assert.Contains(t, f.reason, "budget of the push is spent")
			}
		}
	}
	assert.Equal(t, 3, timeouts, "after the budget is spent every remaining commit is a commit_check_timeout")
}

func TestPattern_Examples(t *testing.T) {
	p := one(t, "pattern:\n  level: error\n  value: 'feat: .+'\n  examples:\n    valid: ['feat: x', 'fix: y']\n    invalid: ['feat: z', 'chore: w']\n")
	got := p.Validate(t.Context(), nil)
	require.Len(t, got, 2)
	assert.Contains(t, got[0].Msg, `pattern does not match the example "fix: y"`)
	assert.Contains(t, got[1].Msg, `pattern matches the example "feat: z"`)
}

// TestAppendixA reproduces the commitizen schema check of architecture
// appendix A on regexp2 v2 in RE2 mode, after normalisation.
func TestAppendixA(t *testing.T) {
	const schema = `(?=([\s\S]+(ref|refs|Refs|Issue|issue):\s[a-zA-Z0-9_\/-]*#\d+[\s\S]*$))(?=(^((?P<change_type>build|ci|docs|feat|fix|perf|refactor|style|test|chore|revert|bump)(?:\((?P<scope>[^()\r\n]*)\)|\()?):\s(?P<subject>[^\n]{0,72})(?:(?P<body>(?:\n(?!(ref|refs|Refs|Issue|issue):)[^\n]*)*)(?:\n(?P<footer>(?:(ref|refs|Refs|Issue|issue):\s[a-zA-Z0-9_\/-]*#\d+))?))?$))`
	r := &rule{name: "pattern", level: levelError}
	require.NoError(t, r.parsePattern(schema, nil))
	cases := []struct {
		msg   string
		match bool
	}{
		{"feat(: add x\n\nref: group/project#1", true},
		{"feat: \n\nref: group/project#1", true},
		{"feat: add x\nbody right after the header\nref: group/project#1", true},
		{"feat: add x\n\nSigned-off-by: A <a@b>\nref: group/project#1", true},
		{"feat: add x\n\nref: group/project#1\nSigned-off-by: A <a@b>", false},
		{"feat(BSE)!: add x\n\nref: group/project#1", false},
		{"Merge branch 'f' into 'main'", false},
		{"Revert \"feat: x\"\n\nref: group/project#1", false},
		{"fix: x\n\nref: group/my.project#1", false},
		{"fix: x\n\nref: group/project#1\n", true},
	}
	for _, tc := range cases {
		got, err := r.matchPattern(Normalise(tc.msg), time.Second)
		require.NoError(t, err, tc.msg)
		assert.Equal(t, tc.match, got, "%q", tc.msg)
	}
	// What the structural rules do with the same messages: the cases the regex gets wrong.
	p := one(t, `reference: {level: error, location: trailer, keys: [ref], pattern: "[A-Za-z0-9_./-]*#[0-9]+"}`)
	assert.NotEmpty(t, reasons(p, "feat(: add x\n\nref: group/project#1"), "unclosed scope is rejected")
	assert.NotEmpty(t, reasons(p, "feat: \n\nref: group/project#1"), "empty subject is rejected")
	assert.NotEmpty(t, reasons(p, "feat: add x\nbody\n\nref: group/project#1"), "missing empty line is rejected")
	assert.Empty(t, reasons(p, "feat: add x\n\nref: group/project#1\nSigned-off-by: A <a@b>"), "trailers in any order")
	assert.Empty(t, reasons(p, "fix: x\n\nref: group/my.project#1"), "dots in the project path")
	assert.Empty(t, reasons(p, "fix: x\n\nref: group/project#1\n"), "trailing newline")
}

func FuzzParsePolicy(f *testing.F) {
	for _, s := range []string{
		"version: 1\nrulesets: [{name: r, rules: {type-enum: {level: error, value: [feat]}}}]\n",
		"version: 1\nextends: org\nmode: warn\nrulesets: [{name: r, refs: [refs/heads/*], format: free}]\n",
		"version: 1\nrulesets: [{name: r, rules: {pattern: {level: error, value: '(?=x)y', examples: {valid: [y]}}}}]\n",
		"version: 1\nrulesets: [{name: r, rules: {reference: {level: warn, location: trailer, keys: [Refs], pattern: '#[0-9]+'}}}]\n",
	} {
		f.Add([]byte(s), "feat(x): y\n\nRefs: #1\n")
	}
	f.Fuzz(func(t *testing.T, data []byte, message string) {
		p, err := parsePolicy(data, testSettings())
		if err != nil {
			return
		}
		_ = p.Validate(t.Context(), nil)
		for _, rs := range p.rulesets {
			rs.check(ParseMessage(message, rs.conventional), nil, 10*time.Millisecond)
		}
	})
}
