package module

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// RuleNamePattern is the name format shared by the rules of every module (architecture §9.1).
const RuleNamePattern = `^[a-z0-9-]{1,63}$`

var ruleNameFormat = regexp.MustCompile(RuleNamePattern)

// ValidRuleName reports whether s has the format of a rule name.
func ValidRuleName(s string) bool { return ruleNameFormat.MatchString(s) }

// RefKind is the kind of the refs field of a rule.
type RefKind int

// Ref kinds.
const (
	RefsAll RefKind = iota
	RefsDefault
	RefsProtected
	RefsList
)

// RefScope is the refs field of a rule: all, default, protected or a list of
// ref globs (architecture §9.1). Which changes a scope covers is up to the module.
type RefScope struct {
	Kind RefKind
	// List holds the ref globs of RefsList.
	List []string
}

// ParseRefScope decodes the refs field; a missing field is all.
func ParseRefScope(v any) (RefScope, error) {
	switch refs := v.(type) {
	case nil:
		return RefScope{Kind: RefsAll}, nil
	case string:
		switch refs {
		case "all":
			return RefScope{Kind: RefsAll}, nil
		case "default":
			return RefScope{Kind: RefsDefault}, nil
		case "protected":
			return RefScope{Kind: RefsProtected}, nil
		}
		return RefScope{}, fmt.Errorf("refs: %q is not one of all, default, protected or a list of refs", refs)
	case []any:
		if len(refs) == 0 {
			return RefScope{}, errors.New("refs: the list is empty")
		}
		s := RefScope{Kind: RefsList}
		for _, item := range refs {
			p, ok := item.(string)
			if !ok || !strings.HasPrefix(p, "refs/") {
				return RefScope{}, fmt.Errorf("refs: %v is not a ref pattern such as refs/heads/release/*", item)
			}
			if _, err := path.Match(p, ""); err != nil {
				return RefScope{}, fmt.Errorf("refs: %q: %w", p, err)
			}
			s.List = append(s.List, p)
		}
		return s, nil
	}
	return RefScope{}, errors.New("refs: expected all, default, protected or a list of refs")
}

// MatchList reports whether the ref matches a glob of the list.
func (s RefScope) MatchList(ref string) bool {
	for _, p := range s.List {
		// ParseRefScope checks the patterns, so Match cannot fail here.
		if ok, err := path.Match(p, ref); err == nil && ok {
			return true
		}
	}
	return false
}

// String is the scope as written in the policy.
func (s RefScope) String() string {
	switch s.Kind {
	case RefsList:
		return strings.Join(s.List, ", ")
	case RefsDefault:
		return "default"
	case RefsProtected:
		return "protected"
	}
	return "all"
}
