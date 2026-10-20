package spec

import (
	"errors"
	"fmt"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// MaxMessage is the length limit of the message of a rule, in characters.
const MaxMessage = 1000

// Rules is the rules field: a mapping of a rule name to a rule, shared by
// every module. A file may set a rule to null to remove a preset rule.
func Rules(rule *Spec, maxRules int) *Spec {
	r := *rule
	r.Nullable = true
	return &Spec{Type: Dict, Named: "rule", KeyPattern: module.RuleNamePattern, KeyFormat: "[a-z0-9-]{1,63}",
		KeyNoun: "rule name", MaxItems: maxRules, Noun: "rules", Values: &r,
		Desc: "Rules by name. A rule of the file with the name of a preset rule overrides its fields; null removes it."}
}

// Extends is the extends field: one preset name or a list of them.
func Extends() *Spec {
	return &Spec{Type: Any, Check: "extends", Desc: "Presets of the server config, merged in this order before the file.",
		OneOf: []*Spec{
			{Type: String, Pattern: module.RuleNamePattern},
			{Type: Array, MinItems: 1, Unique: true, Items: &Spec{Type: String, Pattern: module.RuleNamePattern}},
		}}
}

// Match is the match field of a rule; withDefault allows the value default.
func Match(desc string, withDefault bool) *Spec {
	values, check := []string{"all"}, "match"
	if withDefault {
		values, check = []string{"all", "default"}, "match-default"
	}
	return &Spec{Type: Any, Check: check, Nullable: true, Desc: desc, OneOf: []*Spec{
		{Type: String, Enum: values},
		{Type: Array, MinItems: 1, Items: &Spec{Type: String, MinLength: 1}},
	}}
}

// Message is the message field of a rule, shown to the pusher it denies.
func Message() *Spec {
	return &Spec{Type: String, MinLength: 1, MaxLength: MaxMessage, Nullable: true,
		Desc: "What the pusher reads when the rule denies a push."}
}

// Help is the help field of a rule: where the pusher gets help with it,
// printed in the block of its violation.
func Help() *Spec {
	text := &Spec{Type: String, Check: "help-text", Nullable: true, MaxLength: module.MaxHelp, Desc: "Who to ask, for example #gitlab-support."}
	url := &Spec{Type: String, Check: "help-url", Nullable: true, MaxLength: module.MaxHelp, Pattern: "^https?://", Desc: "Documentation of the rule, an http(s) URL."}
	return &Spec{Type: Object, Nullable: true, Desc: "Help printed in the denial of this rule.", Fields: []Field{
		{Name: "contact", Spec: text},
		{Name: "docs_url", Spec: url},
	}}
}

// builtin are the checks of the shared nodes.
var builtin = Checks{
	"match": func(v *strictyaml.Value) error {
		_, err := module.ParseMatch(v.Interface(), false)
		return err
	},
	"match-default": func(v *strictyaml.Value) error {
		_, err := module.ParseMatch(v.Interface(), true)
		return err
	},
	"help-text": func(v *strictyaml.Value) error { return module.CheckHelpText(v.Str) },
	"help-url":  func(v *strictyaml.Value) error { return module.CheckHelpURL(v.Str) },
	"extends": func(v *strictyaml.Value) error {
		_, err := ExtendsNames(v)
		return err
	},
}

// ExtendsNames are the preset names of an extends field, nil for a missing one.
func ExtendsNames(v *strictyaml.Value) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	var items []*strictyaml.Value
	switch v.Kind {
	case strictyaml.String:
		items = []*strictyaml.Value{v}
	case strictyaml.Seq:
		if len(v.Items) == 0 {
			return nil, errors.New("the list is empty")
		}
		items = v.Items
	default:
		return nil, errors.New("expected a preset name or a list of them")
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(items))
	for _, it := range items {
		if it.Kind != strictyaml.String || !module.ValidRuleName(it.Str) {
			return nil, fmt.Errorf("%v is not a preset name such as org-ci", it.Interface())
		}
		if seen[it.Str] {
			return nil, fmt.Errorf("preset %s is listed twice", it.Str)
		}
		seen[it.Str] = true
		names = append(names, it.Str)
	}
	return names, nil
}
