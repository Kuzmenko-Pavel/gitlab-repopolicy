package commit

import (
	"fmt"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Parts of a conventional header that a check reads. A header that is not
// parsed has no type, scope or "!", so their checks are skipped; its subject
// is the whole line, and subject checks are skipped only when the rule
// requires the conventional format.
const (
	partNone = iota
	partType
	partSubject
)

// checkDef is a check type of the catalogue: the spec of one check of the
// type and the part of a conventional header it reads.
type checkDef struct {
	name string
	part int
	spec *spec.Spec
}

// catalogue is the check types of v1 in the order of the parts of a message;
// new types are only added.
var catalogue = []checkDef{
	{"header-format", partNone, levelSpec(spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.String,
		Enum: []string{"optional", "required"}, Nullable: true,
		Desc: "required: the header follows type(scope)!: subject; optional: any header, the conventional parts are read when it follows the format."}})},
	{"header-max-length", partNone, checkSpec(numberValue("Maximum length of the header in characters."))},
	{"type-enum", partType, checkSpec(listValue("Allowed types."))},
	{"scope", partType, checkSpec(presenceValue("a scope in parentheses after the type"))},
	{"scope-enum", partType, checkSpec(listValue("Allowed scopes."))},
	{"breaking-bang", partType, checkSpec(presenceValue(`"!" after the type or the scope`))},
	{"subject-starts-lowercase", partSubject, checkSpec(presenceValue("a lowercase letter as the first character of the subject"))},
	{"subject-trailing-dot", partSubject, checkSpec(presenceValue(`"." at the end of the subject`))},
	{"body-leading-blank-lines", partNone, checkSpec(presenceValue("an empty line right after the header"))},
	{"body-required", partNone, checkSpec(presenceValue("a body between the header and the trailer"))},
	{"body-max-length", partNone, checkSpec(numberValue("Maximum length of the body in characters, line breaks included."))},
	{"trailer-leading-blank-lines", partNone, checkSpec(presenceValue("an empty line between the body and the trailer"))},
	{"trailer-required", partNone, checkSpec(presenceValue("a trailer, the last part of the message; it also says how the trailer is found"))},
	{"trailer-max-length", partNone, checkSpec(numberValue("Maximum length of the trailer in characters, line breaks included."))},
	{"pattern", partNone, checkSpec(
		spec.Field{Name: "location", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, Nullable: true,
			Enum: []string{"header", "body", "trailer", "message"}, Desc: "Where the regex is searched."}},
		spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, MinLength: 1, Nullable: true, Check: "pattern",
			Desc: "/re/ or /re/i must be found; !/re/ or !/re/i must not be found. RE2, searched anywhere like =~ in GitLab CI."}},
	)},
}

// checkDefOf returns the check type of the catalogue with the name.
func checkDefOf(name string) (checkDef, bool) {
	for _, d := range catalogue {
		if d.name == name {
			return d, true
		}
	}
	return checkDef{}, false
}

func levelField() spec.Field {
	return spec.Field{Name: "level", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, Enum: []string{"error", "warn"}, Nullable: true}}
}

// levelSpec is a check without types: the header has no type to filter by yet.
func levelSpec(value spec.Field) *spec.Spec {
	return &spec.Spec{Type: spec.Object, Nullable: true, Fields: []spec.Field{levelField(), value}}
}

func checkSpec(value ...spec.Field) *spec.Spec {
	return &spec.Spec{Type: spec.Object, Nullable: true, Fields: append([]spec.Field{
		levelField(),
		{Name: "types", Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Noun: "types", Items: nonEmpty(), Nullable: true,
			Desc: "Apply the check only to commits of these conventional types."}},
	}, value...)}
}

func nonEmpty() *spec.Spec { return &spec.Spec{Type: spec.String, MinLength: 1} }

func listValue(desc string) spec.Field {
	return spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Noun: "values", Items: nonEmpty(), Nullable: true, Desc: desc}}
}

// presenceValue is the value of a check of a feature of the message.
func presenceValue(feature string) spec.Field {
	return spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.String, Enum: []string{"optional", "required", "forbidden"}, Nullable: true,
		Desc: "Whether the message has " + feature + ": optional, required or forbidden."}}
}

func numberValue(desc string) spec.Field {
	return spec.Field{Name: "value", AfterMerge: true, Spec: &spec.Spec{Type: spec.Integer, Minimum: 1, Maximum: 10000, Nullable: true, Desc: desc}}
}

// checksSpec is the checks of a rule by type, then by name. It is open: a type
// of a newer version is skipped on the anchor and rejected in a new version.
var checksSpec = func() *spec.Spec {
	s := &spec.Spec{Type: spec.Object, Open: true, Nullable: true,
		Desc: "Checks of the messages by type, then by a name of the check; null removes a type or a check of a preset."}
	for _, d := range catalogue {
		s.Fields = append(s.Fields, spec.Field{Name: d.name, Spec: &spec.Spec{Type: spec.Dict, Nullable: true,
			KeyPattern: module.RuleNamePattern, KeyFormat: "[a-z0-9-]{1,63}", KeyNoun: "check name", Values: d.spec,
			Desc: "Checks of this type by name."}})
	}
	return s
}()

// Spec is COMMITPOLICY version 1; new check types are only added to the catalogue.
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

var ruleSpec = &spec.Spec{Type: spec.Object, Desc: "Every rule that matches a branch applies (AND).", Fields: []spec.Field{
	{Name: "match", AfterMerge: true, Spec: spec.Match("all, default or a list of branch names and /regexes/: the branches the rule applies to.", true)},
	{Name: "check_history", Spec: spec.CheckHistory("the commits that no branch or tag has yet", "every commit that enters the branch")},
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
}
