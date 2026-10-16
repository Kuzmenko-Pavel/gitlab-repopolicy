package commit

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"

	"github.com/dlclark/regexp2/v2"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// ruleDef is a rule of the catalogue: its spec and whether it needs the
// conventional format.
type ruleDef struct {
	conventional bool
	spec         *spec.Spec
}

// catalogue is the rules of v1; new rules are only added.
var catalogue = map[string]ruleDef{
	"type-enum":            {true, ruleSpec(listValue("Allowed types."))},
	"scope":                {true, ruleSpec(enumValue("optional", "required", "forbidden"))},
	"scope-enum":           {true, ruleSpec(listValue("Allowed scopes."))},
	"breaking-bang":        {true, ruleSpec(enumValue("allow", "forbid"))},
	"header-max-length":    {false, ruleSpec(numberValue())},
	"subject-case":         {false, ruleSpec(enumValue("lower-first", "sentence", "any"))},
	"subject-full-stop":    {false, ruleSpec(enumValue("forbid", "require"))},
	"body-required":        {false, ruleSpec()},
	"body-max-line-length": {false, ruleSpec(numberValue())},
	"reference": {false, withCheck(ruleSpec(
		spec.Field{Name: "location", Required: true, Spec: &spec.Spec{Type: spec.String, Enum: []string{"trailer", "body", "any"}}},
		spec.Field{Name: "pattern", Required: true, Spec: &spec.Spec{Type: spec.String, MinLength: 1, Check: "re2",
			Desc: "RE2 regular expression of the reference, for example group/project#123."}},
		spec.Field{Name: "keys", Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Noun: "trailer keys", Items: nonEmpty(),
			Desc: "Trailer keys to look in, for location: trailer."}},
	), "reference-keys")},
	"trailer-required": {false, ruleSpec(listValue("Trailer keys that every message must have."))},
	"pattern": {false, ruleSpec(
		spec.Field{Name: "value", Required: true, Spec: &spec.Spec{Type: spec.String, MinLength: 1, Check: "regexp2",
			Desc: "regexp2 expression in RE2 mode, matched at the start of the message."}},
		spec.Field{Name: "examples", Spec: &spec.Spec{Type: spec.Object, Fields: []spec.Field{
			{Name: "valid", Spec: &spec.Spec{Type: spec.Array, Items: &spec.Spec{Type: spec.String}}},
			{Name: "invalid", Spec: &spec.Spec{Type: spec.Array, Items: &spec.Spec{Type: spec.String}}},
		}}},
	)},
}

func ruleSpec(value ...spec.Field) *spec.Spec {
	return &spec.Spec{Type: spec.Object, Fields: append([]spec.Field{
		{Name: "level", Required: true, Spec: &spec.Spec{Type: spec.String, Enum: []string{"error", "warn", "off"}}},
		{Name: "types", Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Noun: "types", Items: nonEmpty(),
			Desc: "Apply the rule only to these types."}},
	}, value...)}
}

func withCheck(s *spec.Spec, check string) *spec.Spec {
	s.Check = check
	return s
}

func nonEmpty() *spec.Spec { return &spec.Spec{Type: spec.String, MinLength: 1} }

func listValue(desc string) spec.Field {
	return spec.Field{Name: "value", Required: true, Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Noun: "values", Items: nonEmpty(), Desc: desc}}
}

func enumValue(values ...string) spec.Field {
	return spec.Field{Name: "value", Required: true, Spec: &spec.Spec{Type: spec.String, Enum: values}}
}

func numberValue() spec.Field {
	return spec.Field{Name: "value", Required: true, Spec: &spec.Spec{Type: spec.Integer, Minimum: 1, Maximum: 10000}}
}

// rulesSpec is the rules of a ruleset or a preset. It is open: a rule of a
// newer version is skipped on the anchor and rejected in a new version.
var rulesSpec = func() *spec.Spec {
	s := &spec.Spec{Type: spec.Object, Open: true}
	for _, name := range slices.Sorted(maps.Keys(catalogue)) {
		s.Fields = append(s.Fields, spec.Field{Name: name, Spec: catalogue[name].spec})
	}
	return s
}()

// Spec is COMMITPOLICY version 1; new rules are only added to the catalogue.
var Spec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version of the file."}},
	{Name: "mode", Spec: &spec.Spec{Type: spec.String, Enum: []string{"warn", "enforce"},
		Desc: "warn turns every error into a warning; the default is enforce."}},
	{Name: "extends", Spec: &spec.Spec{Type: spec.String,
		Desc: "A preset from commitpolicy_presets of the server config; rules of the file override it."}},
	{Name: "rulesets", Required: true, Spec: &spec.Spec{Type: spec.Array, MinItems: 1, MaxItems: maxRulesets, Noun: "rulesets",
		Named: "ruleset", Desc: "Rule sets; every ruleset that matches a ref applies (AND).", Items: rulesetSpec}},
}}

var rulesetSpec = &spec.Spec{Type: spec.Object, Check: "conventional", Fields: []spec.Field{
	{Name: "name", Required: true, Spec: spec.RuleName()},
	{Name: "refs", Spec: spec.Refs("all (default), default, protected or a list of ref patterns such as refs/heads/release/*.")},
	{Name: "format", Spec: &spec.Spec{Type: spec.String, Enum: []string{"conventional", "free"},
		Desc: "conventional (default): type(scope)!: subject; free: the header is the subject."}},
	{Name: "except", Spec: &spec.Spec{Type: spec.Object, Fields: []spec.Field{
		{Name: "merge_commits", Spec: &spec.Spec{Type: spec.Boolean, Desc: "Skip merge commits."}},
		{Name: "git_reverts", Spec: &spec.Spec{Type: spec.Boolean, Desc: "Skip messages of git revert and of the GitLab revert button."}},
		{Name: "merged_commits", Spec: &spec.Spec{Type: spec.Boolean,
			Desc: "Check only the first-parent chain: commits that a merge commit brings in are not checked."}},
		{Name: "users", Spec: &spec.Spec{Type: spec.Array, MaxItems: maxExceptUsers, Noun: "users",
			Desc:  "Usernames whose pushes skip this ruleset, for example a release bot.",
			Items: &spec.Spec{Type: spec.String, Pattern: module.UsernamePattern, Check: "username"}}},
	}}},
	{Name: "rules", Spec: rulesSpec},
	{Name: "message", Spec: &spec.Spec{Type: spec.String, Desc: "Text shown to the author of a rejected commit."}},
}}

// presetSpec is an entry of commitpolicy_presets in the server config.
var presetSpec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{{Name: "rules", Spec: rulesSpec}}}

var checks = spec.Checks{
	"refs": spec.CheckRefs,
	"username": func(v *strictyaml.Value) error {
		if !module.ValidUsername(v.Str) {
			return fmt.Errorf("%q is not a GitLab username", v.Str)
		}
		return nil
	},
	"re2": func(v *strictyaml.Value) error {
		_, err := regexp.Compile(v.Str)
		return err
	},
	"regexp2": func(v *strictyaml.Value) error {
		_, err := regexp2.Compile(v.Str, regexp2.RE2)
		return err
	},
	"reference-keys": func(v *strictyaml.Value) error {
		if _, ok := v.Fields["keys"]; ok && v.Fields["location"].Str != "trailer" {
			return errors.New("keys: only for location: trailer")
		}
		return nil
	},
	// conventional: rules of the conventional format and types need format: conventional.
	"conventional": func(v *strictyaml.Value) error {
		if f, ok := v.Fields["format"]; !ok || f.Str != "free" {
			return nil
		}
		rules, ok := v.Fields["rules"]
		if !ok {
			return nil
		}
		for _, name := range rules.Keys {
			switch {
			case catalogue[name].conventional:
				return fmt.Errorf("rules.%s: needs format: conventional", name)
			case rules.Fields[name].Fields["types"] != nil:
				return fmt.Errorf("rules.%s.types: needs format: conventional", name)
			}
		}
		return nil
	},
}
