package branch

import (
	"errors"
	"regexp"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

const (
	maxRules        = 50
	maxMatchBytes   = 255
	maxPatternBytes = 256
	maxMessage      = 1000
)

// Spec is BRANCHPOLICY version 1.
var Spec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version of the file."}},
	{Name: "extends", Spec: &spec.Spec{Type: spec.String,
		Desc: "A preset from branchpolicy_presets of the server config; a rule of the file with the name of a preset rule replaces it."}},
	{Name: "mode", Spec: &spec.Spec{Type: spec.String, Enum: []string{"warn", "enforce"},
		Desc: "warn turns every denial into a warning; the default is enforce."}},
	{Name: "rules", Spec: rulesSpec},
}}

var rulesSpec = &spec.Spec{Type: spec.Array, MaxItems: maxRules, Noun: "rules", Named: "rule",
	Desc: "Classes of branch names; a new branch takes the first rule whose match it fits, a name that fits none is denied.", Items: ruleSpec}

var ruleSpec = &spec.Spec{Type: spec.Object, Check: "rule", Fields: []spec.Field{
	{Name: "name", Required: true, Spec: spec.RuleName()},
	{Name: "match", Required: true, Spec: &spec.Spec{Type: spec.String, MinLength: 1, MaxLength: maxMatchBytes, Bytes: true,
		Desc: "Pattern of the class as in GitLab protected branches: * matches any characters, including /."}},
	{Name: "deny", Spec: &spec.Spec{Type: spec.Boolean, Desc: "true: branches of this class may not be created."}},
	{Name: "pattern", Spec: &spec.Spec{Type: spec.String, MinLength: 1, MaxLength: maxPatternBytes, Bytes: true, Check: "re2",
		Desc: "RE2 regular expression that the whole branch name must match."}},
	{Name: "user_prefix", Spec: &spec.Spec{Type: spec.Boolean,
		Desc: "true: the part of the name before the first / is the username of an active GitLab user."}},
	{Name: "protected", Spec: &spec.Spec{Type: spec.Boolean,
		Desc: "true: a new branch of this class must be a protected branch of the GitLab project."}},
	{Name: "examples", Spec: &spec.Spec{Type: spec.Object, Fields: []spec.Field{
		{Name: "valid", Spec: &spec.Spec{Type: spec.Array, Items: &spec.Spec{Type: spec.String}}},
		{Name: "invalid", Spec: &spec.Spec{Type: spec.Array, Items: &spec.Spec{Type: spec.String}}},
	}, Desc: "Names that pattern must accept and reject; checked when the file is accepted, not shown to users."}},
	{Name: "message", Spec: &spec.Spec{Type: spec.String, MaxLength: maxMessage,
		Desc: "Text shown when a branch is denied, up to 1000 characters: the format with an example and, for protected, what to protect."}},
}}

// presetSpec is an entry of branchpolicy_presets in the server config.
var presetSpec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{{Name: "rules", Spec: rulesSpec}}}

var checks = spec.Checks{
	"re2": func(v *strictyaml.Value) error {
		_, err := regexp.Compile(v.Str)
		return err
	},
	// rule: a forbidden class has nothing to check, and examples test a pattern.
	"rule": func(v *strictyaml.Value) error {
		_, hasPattern := v.Fields["pattern"]
		_, hasExamples := v.Fields["examples"]
		if d, ok := v.Fields["deny"]; ok && d.Bool {
			for _, f := range []string{"pattern", "user_prefix", "protected", "examples"} {
				if _, ok := v.Fields[f]; ok {
					return errors.New("deny: no " + f + " with deny")
				}
			}
		}
		if hasExamples && !hasPattern {
			return errors.New("examples: only with pattern")
		}
		return nil
	},
}
