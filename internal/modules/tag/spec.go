package tag

import (
	"fmt"
	"slices"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// labelPattern has the characters of a GitLab username, which is the recommended label.
const labelPattern = `^[A-Za-z0-9_.-]{1,255}$`

// Spec is TAGPOLICY version 1. keys are only in the file of a project:
// signers of preset rules refer to them.
var Spec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version."}},
	{Name: "extends", Spec: spec.Extends()},
	{Name: "rules", Spec: spec.Rules(ruleSpec, maxRules)},
	{Name: "keys", Spec: &spec.Spec{Type: spec.Dict, MinItems: 1, MaxItems: maxKeys, Noun: "keys",
		KeyPattern: labelPattern, KeyFormat: "[A-Za-z0-9_.-]{1,255}", Check: "distinct-keys",
		Desc:   "1 to 32 public keys by label: an armored OpenPGP public key block or an OpenSSH public key on one line. Required when there are rules after merging with presets.",
		Values: &spec.Spec{Type: spec.String, MinLength: 1, Check: "public-key"}}},
}}

// presetSpec is a preset of the server config: complete rules only.
var presetSpec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "rules", Required: true, Spec: spec.Rules(ruleSpec, maxRules)},
}}

var ruleSpec = &spec.Spec{Type: spec.Object, Desc: "Every rule that matches a tag must be satisfied (AND).", Fields: []spec.Field{
	{Name: "match", AfterMerge: true, Spec: spec.Match("all or a list of tag names and /regexes/, such as ['/^v\\d+\\.\\d+\\.\\d+$/'].", false)},
	{Name: "signers", AfterMerge: true, Spec: &spec.Spec{Type: spec.Array, MinItems: 1, MaxItems: maxKeys, Noun: "labels of keys", Unique: true, Nullable: true,
		Desc:  "Labels of keys of the file that may sign the matching tags.",
		Items: &spec.Spec{Type: spec.String, Pattern: labelPattern, Format: "[A-Za-z0-9_.-]{1,255}"}}},
	{Name: "protected", Spec: &spec.Spec{Type: spec.Boolean, Nullable: true,
		Desc: "true: a tag created under this rule must be a protected tag of the GitLab project, so that its pipeline gets protected variables and runners."}},
	{Name: "message", AfterMerge: true, Spec: spec.Message()},
	{Name: "help", Spec: spec.Help()},
}}

var checks = spec.Checks{
	"public-key": func(v *strictyaml.Value) error {
		_, err := parseKey("", v.Str)
		return err
	},
	"distinct-keys": func(v *strictyaml.Value) error {
		byFingerprint := map[string]string{}
		for _, label := range slices.Sorted(slices.Values(v.Keys)) {
			k, err := parseKey(label, v.Fields[label].Str)
			if err != nil {
				return err
			}
			if other, ok := byFingerprint[k.fingerprint]; ok {
				return fmt.Errorf("keys.%s is the same key as keys.%s (%s)", label, other, k.fingerprint)
			}
			byFingerprint[k.fingerprint] = label
		}
		return nil
	},
}
