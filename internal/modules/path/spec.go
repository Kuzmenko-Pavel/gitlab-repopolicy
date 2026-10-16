package path

import (
	"fmt"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Spec is PATHPOLICY version 1; the format is frozen.
var Spec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version; version 1 is frozen."}},
	{Name: "admins", Required: true, Spec: usernameList(maxAdmins, "1 to 5 usernames that may change PATHPOLICY itself.")},
	{Name: "rules", Spec: &spec.Spec{Type: spec.Array, MaxItems: maxRules, Noun: "rules", Named: "rule",
		Desc: "Path rules; every rule that matches a path must be satisfied (AND).", Items: ruleSpec}},
}}

var ruleSpec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "name", Required: true, Spec: spec.RuleName()},
	{Name: "paths", Required: true, Spec: &spec.Spec{Type: spec.Array, MinItems: 1, MaxItems: maxPaths, Noun: "patterns",
		Desc: "1 to 50 doublestar patterns anchored at the repository root.",
		Items: &spec.Spec{Type: spec.String, MinLength: 1, MaxLength: maxPatternBytes, Bytes: true,
			Pattern: `^[^/!\\{}][^\\{}]*$`, Check: "path-pattern"}}},
	{Name: "refs", Spec: spec.Refs("all (default), protected, default or a list of ref patterns such as refs/heads/release/*.")},
	{Name: "allow", Required: true, Spec: usernameList(maxAllow, "1 to 50 usernames that may change the paths; no groups or roles.")},
	{Name: "message", Spec: &spec.Spec{Type: spec.String, MaxLength: maxMessage, Desc: "Text shown when a change is denied, up to 1000 characters."}},
}}

func usernameList(high int, desc string) *spec.Spec {
	return &spec.Spec{Type: spec.Array, MinItems: 1, MaxItems: high, Noun: "usernames", Unique: true, Check: "usernames", Desc: desc,
		Items: &spec.Spec{Type: spec.String, Pattern: module.UsernamePattern, Check: "username"}}
}

var checks = spec.Checks{
	"path-pattern": func(v *strictyaml.Value) error {
		if err := validatePattern(v.Str); err != nil {
			return fmt.Errorf("%q: %w", v.Str, err)
		}
		return nil
	},
	"refs": spec.CheckRefs,
	// username accepts only usernames of people or bots, no groups or roles.
	"username": func(v *strictyaml.Value) error {
		if strings.ContainsAny(v.Str, "/@:") {
			return fmt.Errorf("%q is not a username; groups and roles are not supported, list the people", v.Str)
		}
		if !module.ValidUsername(v.Str) {
			return fmt.Errorf("%q is not a GitLab username", v.Str)
		}
		return nil
	},
	// usernames are compared case-insensitively, like GitLab does.
	"usernames": func(v *strictyaml.Value) error {
		seen := map[string]bool{}
		for _, it := range v.Items {
			key := strings.ToLower(it.Str)
			if seen[key] {
				return fmt.Errorf("%q is listed twice", it.Str)
			}
			seen[key] = true
		}
		return nil
	},
}
