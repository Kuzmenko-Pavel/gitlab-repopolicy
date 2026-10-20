package branch

import (
	"errors"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

const maxRules = 50

// Spec is BRANCHPOLICY version 1.
var Spec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version of the file."}},
	{Name: "mode", Spec: &spec.Spec{Type: spec.String, Enum: []string{"warn", "enforce"},
		Desc: "warn turns every denial into a warning; the default is enforce."}},
	{Name: "extends", Spec: spec.Extends()},
	{Name: "rules", Spec: spec.Rules(ruleSpec, maxRules)},
}}

// presetSpec is a preset of the server config: complete rules only.
var presetSpec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "rules", Required: true, Spec: spec.Rules(ruleSpec, maxRules)},
}}

var ruleSpec = &spec.Spec{Type: spec.Object, Check: "rule",
	Desc: "A class of branch names; a new branch takes the matching rule with the lowest priority, a name that matches none is not checked.",
	Fields: []spec.Field{
		{Name: "match", AfterMerge: true, Spec: spec.Match("all or a list of branch names and /regexes/: the names of the class.", false)},
		{Name: "priority", AfterMerge: true, Spec: &spec.Spec{Type: spec.Integer, Minimum: 0, Maximum: 1000000, Nullable: true,
			Desc: "Order of the rules: the lowest number that matches wins; unique after merging with presets."}},
		{Name: "deny", Spec: &spec.Spec{Type: spec.Boolean, Nullable: true, Desc: "true: branches of this class may not be created."}},
		{Name: "user_prefix", Spec: &spec.Spec{Type: spec.Boolean, Nullable: true,
			Desc: "true: the part of the name before the first / is the username of an active GitLab user."}},
		{Name: "protected", Spec: &spec.Spec{Type: spec.Boolean, Nullable: true,
			Desc: "true: a new branch of this class must be a protected branch of the GitLab project."}},
		{Name: "message", AfterMerge: true, Spec: spec.Message()},
		{Name: "help", Spec: spec.Help()},
	}}

var checks = spec.Checks{
	// rule: a forbidden class has nothing to check.
	"rule": func(v *strictyaml.Value) error {
		if d, ok := v.Fields["deny"]; ok && d.Bool {
			for _, f := range []string{"user_prefix", "protected"} {
				if x, ok := v.Fields[f]; ok && x.Kind != strictyaml.Null {
					return errors.New("deny: no " + f + " with deny")
				}
			}
		}
		return nil
	},
}
