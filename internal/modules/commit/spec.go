package commit

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// checkDef is a check of the catalogue: its spec and whether it needs the
// conventional parser.
type checkDef struct {
	conventional bool
	spec         *spec.Spec
}

// catalogue is the checks of v1; new checks are only added.
var catalogue = map[string]checkDef{
	"type-enum":            {true, checkSpec(listValue("Allowed types."))},
	"scope":                {true, checkSpec(enumValue("optional", "required", "forbidden"))},
	"scope-enum":           {true, checkSpec(listValue("Allowed scopes."))},
	"breaking-bang":        {true, checkSpec(enumValue("allow", "forbid"))},
	"header-max-length":    {false, checkSpec(numberValue())},
	"subject-case":         {false, checkSpec(enumValue("lower-first", "sentence", "any"))},
	"subject-full-stop":    {false, checkSpec(enumValue("forbid", "require"))},
	"body-required":        {false, checkSpec()},
	"body-max-line-length": {false, checkSpec(numberValue())},
	"trailer-required":     {false, checkSpec(listValue("Trailer keys that every message must have."))},
	"pattern": {false, withCheck(checkSpec(
		spec.Field{Name: "location", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, Nullable: true,
			Enum: []string{"header", "body", "trailer", "message"}, Desc: "Where the regex is searched."}},
		spec.Field{Name: "keys", Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Noun: "trailer keys", Nullable: true, Items: nonEmpty(),
			Desc: "Trailer keys to search, for location: trailer; compared case-insensitively."}},
		spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, MinLength: 1, Nullable: true, Check: "pattern",
			Desc: "/re/ or /re/i must be found; !/re/ or !/re/i must not be found. RE2, searched anywhere like =~ in GitLab CI."}},
	), "pattern-keys")},
}

func checkSpec(value ...spec.Field) *spec.Spec {
	return &spec.Spec{Type: spec.Object, Nullable: true, Fields: append([]spec.Field{
		{Name: "level", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, Enum: []string{"error", "warn"}, Nullable: true}},
		{Name: "types", Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Noun: "types", Items: nonEmpty(), Nullable: true,
			Desc: "Apply the check only to these types."}},
	}, value...)}
}

func withCheck(s *spec.Spec, check string) *spec.Spec {
	s.Check = check
	return s
}

func nonEmpty() *spec.Spec { return &spec.Spec{Type: spec.String, MinLength: 1} }

func listValue(desc string) spec.Field {
	return spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Noun: "values", Items: nonEmpty(), Nullable: true, Desc: desc}}
}

func enumValue(values ...string) spec.Field {
	return spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, Enum: values, Nullable: true}}
}

func numberValue() spec.Field {
	return spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.Integer, Minimum: 1, Maximum: 10000, Nullable: true}}
}

// checksSpec is the checks of a rule. It is open: a check of a newer version
// is skipped on the anchor and rejected in a new version.
var checksSpec = func() *spec.Spec {
	s := &spec.Spec{Type: spec.Object, Open: true, Nullable: true, Desc: "Checks of the messages, by name; null removes a check of a preset."}
	for _, name := range slices.Sorted(maps.Keys(catalogue)) {
		s.Fields = append(s.Fields, spec.Field{Name: name, Spec: catalogue[name].spec})
	}
	return s
}()

// Spec is COMMITPOLICY version 1; new checks are only added to the catalogue.
var Spec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version of the file."}},
	{Name: "mode", Spec: &spec.Spec{Type: spec.String, Enum: []string{"warn", "enforce"},
		Desc: "warn turns every error into a warning; the default is enforce."}},
	{Name: "extends", Spec: spec.Extends()},
	{Name: "rules", Spec: spec.Rules(ruleSpec, maxRules)},
}}

// presetSpec is a preset of the server config: complete rules only.
var presetSpec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "rules", Required: true, Spec: spec.Rules(ruleSpec, maxRules)},
}}

var ruleSpec = &spec.Spec{Type: spec.Object, Check: "conventional", Desc: "Every rule that matches a branch applies (AND).", Fields: []spec.Field{
	{Name: "match", AfterMerge: true, Spec: spec.Match("all, default or a list of branch names and /regexes/: the branches the rule applies to.", true)},
	{Name: "parser", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, Enum: []string{"conventional", "free"}, Nullable: true,
		Desc: "conventional: type(scope)!: subject; free: the header is the subject."}},
	{Name: "except", Spec: &spec.Spec{Type: spec.Object, Nullable: true, Fields: []spec.Field{
		{Name: "merge_commit", Spec: &spec.Spec{Type: spec.Boolean, Nullable: true, Desc: "Skip merge commits."}},
		{Name: "merged_commits", Spec: &spec.Spec{Type: spec.Boolean, Nullable: true,
			Desc: "Check only the first-parent chain: commits that a merge commit brings in are not checked."}},
		{Name: "git_reverts", Spec: &spec.Spec{Type: spec.Boolean, Nullable: true, Desc: "Skip messages of git revert and of the GitLab revert button."}},
		{Name: "users", Spec: &spec.Spec{Type: spec.Array, MaxItems: maxExceptUsers, Noun: "users", Nullable: true,
			Desc:  "Usernames whose pushes skip this rule, for example a release bot.",
			Items: &spec.Spec{Type: spec.String, Pattern: module.UsernamePattern, Check: "username"}}},
	}}},
	{Name: "checks", Spec: checksSpec},
	{Name: "message", AfterMerge: true, Spec: spec.Message()},
	{Name: "help", Spec: spec.Help()},
}}

var checks = spec.Checks{
	"username": func(v *strictyaml.Value) error {
		if !module.ValidUsername(v.Str) {
			return fmt.Errorf("%q is not a GitLab username", v.Str)
		}
		return nil
	},
	"pattern": func(v *strictyaml.Value) error {
		_, err := module.ParsePattern(v.Str)
		return err
	},
	"pattern-keys": func(v *strictyaml.Value) error {
		if keys, ok := v.Fields["keys"]; ok && keys.Kind != strictyaml.Null {
			if loc, ok := v.Fields["location"]; ok && loc.Kind == strictyaml.String && loc.Str != "trailer" {
				return errors.New("keys: only for location: trailer")
			}
		}
		return nil
	},
	// conventional: checks of the conventional format and types need parser:
	// conventional. A file without parser takes it from a preset, so only the
	// rule that has the parser is checked.
	"conventional": func(v *strictyaml.Value) error {
		if p, ok := v.Fields["parser"]; !ok || p.Str != "free" {
			return nil
		}
		cs, ok := v.Fields["checks"]
		if !ok || cs.Kind != strictyaml.Map {
			return nil
		}
		for _, name := range cs.Keys {
			c := cs.Fields[name]
			switch {
			case c.Kind == strictyaml.Null:
			case catalogue[name].conventional:
				return fmt.Errorf("checks.%s: needs parser: conventional", name)
			case c.Kind == strictyaml.Map && c.Fields["types"] != nil && c.Fields["types"].Kind != strictyaml.Null:
				return fmt.Errorf("checks.%s.types: needs parser: conventional", name)
			}
		}
		return nil
	},
}
