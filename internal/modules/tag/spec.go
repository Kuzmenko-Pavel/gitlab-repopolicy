package tag

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// labelPattern has the characters of a GitLab username, which is the recommended label.
const labelPattern = `^[A-Za-z0-9_.-]{1,255}$`

// Spec is TAGPOLICY version 1; the format is frozen.
// keys come before rules: signers refer to them.
var Spec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version; version 1 is frozen."}},
	{Name: "keys", Required: true, Spec: &spec.Spec{Type: spec.Dict, MinItems: 1, MaxItems: maxKeys, Noun: "keys",
		KeyPattern: labelPattern, KeyFormat: "[A-Za-z0-9_.-]{1,255}", Check: "distinct-keys",
		Desc:   "1 to 32 public keys by label: an armored OpenPGP public key block or an OpenSSH public key on one line.",
		Values: &spec.Spec{Type: spec.String, MinLength: 1, Check: "public-key"}}},
	{Name: "rules", Spec: &spec.Spec{Type: spec.Array, MaxItems: maxRules, Noun: "rules", Named: "rule",
		Desc: "Up to 50 tag rules; every rule that matches a tag must be satisfied (AND).", Items: ruleSpec}},
}}

var ruleSpec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "name", Required: true, Spec: spec.RuleName()},
	{Name: "tags", Required: true, Spec: &spec.Spec{Type: spec.String, MinLength: 1, MaxLength: maxPatternBytes, Bytes: true, Check: "tag-pattern",
		Desc: "RE2 regular expression that matches the whole tag name without refs/tags/."}},
	{Name: "signers", Required: true, Spec: &spec.Spec{Type: spec.Array, MinItems: 1, MaxItems: maxKeys, Noun: "labels of keys", Unique: true,
		Desc:  "Labels of keys that may sign the matching tags.",
		Items: &spec.Spec{Type: spec.String, Pattern: labelPattern, Check: "signer"}}},
	{Name: "protected", Spec: &spec.Spec{Type: spec.Boolean,
		Desc: "true: a tag created under this rule must be a protected tag of the GitLab project, so that its pipeline gets protected variables and runners."}},
	{Name: "message", Spec: &spec.Spec{Type: spec.String, MaxLength: maxMessage, Desc: "Text shown when a tag is denied, up to 1000 characters."}},
}}

// checks of a file whose tree is root: signers refer to its keys.
func checks(root *strictyaml.Value) spec.Checks {
	return spec.Checks{
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
		"tag-pattern": func(v *strictyaml.Value) error {
			_, err := compileTags(v.Str)
			return err
		},
		"signer": func(v *strictyaml.Value) error {
			if _, ok := root.Fields["keys"].Fields[v.Str]; !ok {
				return fmt.Errorf("%q is not a label in keys", v.Str)
			}
			return nil
		},
	}
}

// compileTags compiles the tags pattern of a rule anchored at both ends.
func compileTags(src string) (*regexp.Regexp, error) {
	if _, err := regexp.Compile(src); err != nil {
		return nil, err
	}
	return regexp.Compile(`^(?:` + src + `)$`)
}
