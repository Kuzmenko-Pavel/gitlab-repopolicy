package module

import "regexp"

// RuleNamePattern is the name format shared by the rules of every module.
const RuleNamePattern = `^[a-z0-9-]{1,63}$`

var ruleNameFormat = regexp.MustCompile(RuleNamePattern)

// ValidRuleName reports whether s has the format of a rule name.
func ValidRuleName(s string) bool { return ruleNameFormat.MatchString(s) }
