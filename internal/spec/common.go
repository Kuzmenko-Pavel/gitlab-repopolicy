package spec

import (
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// RuleName is the name of a rule, shared by every module.
func RuleName() *Spec {
	return &Spec{Type: String, Pattern: module.RuleNamePattern, Format: "[a-z0-9-]{1,63}"}
}

// Refs is the refs field of a rule, checked by CheckRefs under the name "refs".
func Refs(desc string) *Spec {
	return &Spec{Type: Any, Check: "refs", Desc: desc, OneOf: []*Spec{
		{Type: String, Enum: []string{"all", "default", "protected"}},
		{Type: Array, MinItems: 1, Items: &Spec{Type: String, Pattern: "^refs/"}},
	}}
}

// CheckRefs is the check "refs": module.ParseRefScope accepts the value.
func CheckRefs(v *strictyaml.Value) error {
	_, err := module.ParseRefScope(v.Interface())
	return err
}
