package path

import (
	"fmt"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Spec is PATHPOLICY version 1.
var Spec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version."}},
	{Name: "extends", Spec: spec.Extends()},
	{Name: "admins", Required: true, Spec: usernameList(maxAdmins, "1 to 5 usernames that may change PATHPOLICY itself.")},
	{Name: "rules", Spec: spec.Rules(ruleSpec, maxRules)},
}}

// presetSpec is a preset of the server config: complete rules only.
var presetSpec = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "rules", Required: true, Spec: spec.Rules(ruleSpec, maxRules)},
}}

var ruleSpec = &spec.Spec{Type: spec.Object, Desc: "Every rule that matches a path must be satisfied (AND).", Fields: []spec.Field{
	{Name: "match", AfterMerge: true, Spec: spec.Match("all, default or a list of branch names and /regexes/: the branches the rule applies to.", true)},
	{Name: "check_history", Spec: spec.CheckHistory(
		"the commits that no branch or tag has yet, without the content of a path that the branch or the default branch already had",
		"the net diff of the push and every commit that enters the branch")},
	{Name: "paths", AfterMerge: true, Spec: &spec.Spec{Type: spec.Array, MinItems: 1, MaxItems: maxPaths, Noun: "patterns", Nullable: true,
		Desc: "1 to 50 doublestar patterns anchored at the repository root.",
		Items: &spec.Spec{Type: spec.String, MinLength: 1, MaxLength: maxPatternBytes, Bytes: true,
			Pattern: `^[^/!\\{}][^\\{}]*$`, Check: "path-pattern"}}},
	{Name: "allow", AfterMerge: true, Spec: nullable(usernameList(maxAllow, "1 to 50 usernames that may change the paths; no groups or roles."))},
	{Name: "message", AfterMerge: true, Spec: spec.Message()},
	{Name: "help", Spec: spec.Help()},
}}

func nullable(s *spec.Spec) *spec.Spec {
	c := *s
	c.Nullable = true
	return &c
}

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
