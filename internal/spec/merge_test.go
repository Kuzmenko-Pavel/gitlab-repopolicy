package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// ruleSpec is a rule of a module: match and message are required only after
// merging, so a file may override a preset rule partly.
var ruleSpec = &Spec{Type: Object, Fields: []Field{
	{Name: "match", AfterMerge: true, Spec: Match("", true)},
	{Name: "allow", AfterMerge: true, Spec: &Spec{Type: Array, MinItems: 1, Nullable: true, Items: &Spec{Type: String}}},
	{Name: "checks", Spec: &Spec{Type: Dict, Nullable: true, Values: &Spec{Type: Object, Nullable: true, Fields: []Field{
		{Name: "level", AfterMerge: true, Spec: &Spec{Type: String, Enum: []string{"error", "warn"}, Nullable: true}},
	}}}},
	{Name: "message", AfterMerge: true, Spec: Message()},
	{Name: "help", Spec: Help()},
}}

var fileSpec = &Spec{Type: Object, Fields: []Field{
	{Name: "version", Required: true, Spec: &Spec{Type: Integer, Const: 1}},
	{Name: "extends", Spec: Extends()},
	{Name: "rules", Spec: Rules(ruleSpec, 3)},
}}

var presetSpec = &Spec{Type: Object, Fields: []Field{{Name: "rules", Required: true, Spec: Rules(ruleSpec, 3)}}}

func tree(t *testing.T, src string) *strictyaml.Value {
	t.Helper()
	v, err := strictyaml.DecodeTree([]byte(src), 1<<16)
	require.NoError(t, err)
	return v
}

func merged(t *testing.T, presets map[string]any, file string) (*strictyaml.Value, Origin, error) {
	t.Helper()
	f := tree(t, file)
	require.NoError(t, Validate(fileSpec, f, nil))
	names, err := ExtendsNames(f.Fields["extends"])
	require.NoError(t, err)
	layers, err := NewPresets(presets, presetSpec, nil, 1<<16).Layers(names, f.Fields["rules"])
	if err != nil {
		return nil, nil, err
	}
	rules, origin := MergeRules(layers)
	return WithRules(f, rules), origin, ValidateMerged(fileSpec, WithRules(f, rules), nil)
}

var orgCI = map[string]any{"rules": map[string]any{
	"ci":     map[string]any{"match": "all", "allow": []any{"lead"}, "message": "CI belongs to the platform team.", "checks": map[string]any{"a": map[string]any{"level": "error"}, "b": map[string]any{"level": "warn"}}},
	"agents": map[string]any{"match": "default", "allow": []any{"lead"}, "message": "Agents."},
}}

func TestMerge_HelmRules(t *testing.T) {
	presets := map[string]any{"org-ci": orgCI, "org-extra": map[string]any{"rules": map[string]any{
		"ci": map[string]any{"match": "all", "allow": []any{"sre"}, "message": "Extra."},
	}}}
	doc, origin, err := merged(t, presets, `version: 1
extends: [org-ci, org-extra]
rules:
  ci:
    allow: [lead, alice]
    checks:
      b: null
  agents: null
  own:
    match: [main]
    allow: [bob]
    message: Own rule.
`)
	require.NoError(t, err)
	rules := doc.Fields["rules"]
	assert.Equal(t, []string{"ci", "own"}, rules.Keys)
	ci := rules.Fields["ci"]
	assert.Equal(t, []any{"lead", "alice"}, ci.Fields["allow"].Interface(), "a list is replaced, not appended")
	assert.Equal(t, "Extra.", ci.Fields["message"].Str, "the later preset wins")
	assert.Equal(t, map[string]any{"a": map[string]any{"level": "error"}}, ci.Fields["checks"].Interface(), "null removes a key")
	assert.Equal(t, 5, ci.Fields["allow"].Line, "values of the file keep their lines")
	assert.Zero(t, ci.Fields["message"].Line, "values of presets have no line")

	assert.Equal(t, []string{"preset:org-ci", "preset:org-extra", "file"}, origin["ci"])
	assert.Equal(t, []string{"file"}, origin["ci.allow"])
	assert.Equal(t, []string{"preset:org-extra"}, origin["ci.message"])
	assert.Equal(t, []string{"preset:org-ci"}, origin["ci.checks.a.level"])
	assert.NotContains(t, origin, "ci.checks.b")
	assert.NotContains(t, origin, "agents")
	assert.Equal(t, []string{"file"}, origin["own"])
}

func TestMerge_Errors(t *testing.T) {
	_, _, err := merged(t, map[string]any{"org-ci": orgCI}, `version: 1
rules:
  ci:
    allow: [alice]
`)
	assert.EqualError(t, err, "line 4: rule ci: match: required", "a partial rule of the file without a preset rule")

	_, _, err = merged(t, nil, "version: 1\nextends: org-ci\n")
	assert.EqualError(t, err, `extends: preset "org-ci" is not defined in the server config`)

	broken := map[string]any{"org-ci": map[string]any{"rules": map[string]any{"ci": map[string]any{"allow": []any{"x"}}}}}
	_, _, err = merged(t, broken, "version: 1\nextends: org-ci\n")
	assert.ErrorContains(t, err, `extends: preset "org-ci" of the server config is invalid: rule ci: match: required`)

	_, _, err = merged(t, map[string]any{"org-ci": orgCI}, "version: 1\nextends: org-ci\nrules:\n  ci:\n    message: null\n")
	assert.EqualError(t, err, "line 5: rule ci: message: required", "null removes a required field")

	_, err = ExtendsNames(tree(t, "[org-ci, org-ci]"))
	assert.EqualError(t, err, "preset org-ci is listed twice")
}

func TestMerge_EmptyResult(t *testing.T) {
	doc, _, err := merged(t, map[string]any{"org-ci": orgCI}, "version: 1\nextends: org-ci\nrules:\n  ci: null\n  agents: null\n")
	require.NoError(t, err)
	assert.Empty(t, doc.Fields["rules"].Keys)
	doc, _, err = merged(t, nil, "version: 1\n")
	require.NoError(t, err)
	assert.Empty(t, doc.Fields["rules"].Keys)
}

func TestPresets_DigestAndErrors(t *testing.T) {
	p := NewPresets(map[string]any{"org-ci": orgCI, "bad": map[string]any{"rules": 1}}, presetSpec, nil, 1<<16)
	assert.Len(t, p.Digest("org-ci"), 64)
	same := NewPresets(map[string]any{"org-ci": orgCI}, presetSpec, nil, 1<<16)
	assert.Equal(t, p.Digest("org-ci"), same.Digest("org-ci"))
	assert.Equal(t, []string{"bad: rules: expected a mapping, got a number"}, p.Errors())
}

func TestValidate_FileAndHelp(t *testing.T) {
	err := Validate(fileSpec, tree(t, "version: 1\nrules:\n  ci:\n    help:\n      docs_url: ftp://x\n"), nil)
	assert.EqualError(t, err, `line 5: rule ci: help.docs_url: "ftp://x" is not an http(s) URL`)
	err = Validate(fileSpec, tree(t, "version: 1\nrules:\n  Bad:\n    allow: [x]\n"), nil)
	assert.EqualError(t, err, `line 3: rules: rule name "Bad" must match [a-z0-9-]{1,63}`)
	err = Validate(fileSpec, tree(t, "version: 1\nrules:\n  ci:\n    match: release/*\n"), nil)
	assert.EqualError(t, err, `line 4: rule ci: match: "release/*" is not all, default or a list; write a single name as [release/*]`)
	require.NoError(t, Validate(fileSpec, tree(t, "version: 1\nrules:\n  ci:\n    allow: null\n    help: null\n"), nil))
}
