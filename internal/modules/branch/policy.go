package branch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
)

type ruleYAML struct {
	Match      any         `yaml:"match"`
	Priority   int         `yaml:"priority"`
	Deny       bool        `yaml:"deny"`
	UserPrefix bool        `yaml:"user_prefix"`
	Protected  bool        `yaml:"protected"`
	Message    string      `yaml:"message"`
	Help       module.Help `yaml:"help"`
}

// Policy is a parsed BRANCHPOLICY merged with its presets.
type Policy struct {
	warnMode bool
	// rules are ordered by priority: the first rule that matches a name wins.
	rules   []*rule
	presets []module.PresetRef
	origins []module.RuleOrigin
	// unusable says why the rules cannot be used: a missing or broken
	// preset, or a merge result that fails its checks. The module is then
	// skipped with a warning.
	unusable error
}

type rule struct {
	name       string
	match      module.Match
	priority   int
	deny       bool
	userPrefix bool
	protected  bool
	message    string
	help       module.Help
	source     []string
}

// Settings come from the server config.
type Settings struct {
	presets spec.Presets
}

// NewSettings decodes the presets of the module in the server config.
func NewSettings(rawPresets map[string]any) *Settings {
	return &Settings{presets: spec.NewPresets(rawPresets, presetSpec, checks, module.DefaultMaxPolicySize)}
}

// Parse decodes a BRANCHPOLICY file offline and merges the presets it
// extends; s may be nil when there is no server config.
func Parse(data []byte, s *Settings) (*Policy, error) {
	if s == nil {
		s = &Settings{}
	}
	res, err := spec.Resolve(data, module.DefaultMaxPolicySize, Spec, checks, s.presets)
	var pe *spec.PresetError
	if errors.As(err, &pe) {
		return &Policy{unusable: pe}, nil
	}
	if err != nil {
		return nil, err
	}
	mode := res.Doc.Fields["mode"]
	p := &Policy{warnMode: mode != nil && mode.Str == "warn", presets: res.Refs(s.presets), origins: res.RuleOrigins()}
	for _, name := range res.Rules() {
		var y ruleYAML
		if err := res.DecodeRule(name, &y); err != nil {
			return nil, err
		}
		m, err := module.ParseMatch(y.Match, false)
		if err != nil {
			return nil, fmt.Errorf("rule %s: match: %w", name, err)
		}
		p.rules = append(p.rules, &rule{name: name, match: m, priority: y.Priority, deny: y.Deny, userPrefix: y.UserPrefix,
			protected: y.Protected, message: strings.TrimSpace(y.Message), help: y.Help, source: res.Origin[name]})
	}
	slices.SortStableFunc(p.rules, func(a, b *rule) int { return cmp.Compare(a.priority, b.priority) })
	p.unusable = check(p.rules)
	return p, nil
}

// Presets are the presets the file extends, for the audit log.
func (p *Policy) Presets() []module.PresetRef { return p.presets }

// Origins are the merged rules with their layers, for validate.
func (p *Policy) Origins() []module.RuleOrigin { return p.origins }

// check finds what makes the merged rules unusable: a repeated priority
// leaves the order to chance, a rule after match: all or with the match of a
// rule before it is never used.
func check(rules []*rule) error {
	var errs []error
	for i, r := range rules {
		if i > 0 && rules[i-1].priority == r.priority {
			errs = append(errs, fmt.Errorf("rule %s: priority %d is also the priority of rule %s", r.name, r.priority, rules[i-1].name))
		}
		for _, before := range rules[:i] {
			switch {
			case before.match.All:
				errs = append(errs, fmt.Errorf("rule %s: comes after rule %s with match: all and is never used; give match: all the highest priority", r.name, before.name))
			case before.match.Same(r.match):
				errs = append(errs, fmt.Errorf("rule %s: match %s is the match of rule %s with a lower priority, so it is never used", r.name, r.match, before.name))
			default:
				continue
			}
			break
		}
	}
	return errors.Join(errs...)
}

// first is the rule of the class a branch name falls into, nil for none.
func first(rules []*rule, name string) *rule {
	for _, r := range rules {
		if r.match.All || r.match.Matches(name) {
			return r
		}
	}
	return nil
}

// Validate reports rules that cannot be used: a new version of the file with
// them is rejected.
func (p *Policy) Validate(context.Context, module.Validator) []module.Problem {
	if p.unusable == nil {
		return nil
	}
	var problems []module.Problem
	for _, line := range strings.Split(p.unusable.Error(), "\n") {
		problems = append(problems, module.Problem{Msg: line})
	}
	return problems
}

// NameRule is the rule of the class a branch name falls into, for validate
// --branch.
type NameRule struct {
	Rule                        string
	Match                       string
	Deny, UserPrefix, Protected bool
	Message                     string
	Source                      []string
}

// RuleFor returns the rule of a new branch name; ok is false when no rule
// matches or the rules cannot be used.
func (p *Policy) RuleFor(name string) (NameRule, bool) {
	if p.unusable != nil {
		return NameRule{}, false
	}
	r := first(p.rules, name)
	if r == nil {
		return NameRule{}, false
	}
	return NameRule{Rule: r.name, Match: r.match.String(), Deny: r.deny, UserPrefix: r.userPrefix, Protected: r.protected,
		Message: r.message, Source: r.source}, true
}

// MustProtect reports whether a branch must be protected in GitLab: its rule
// has protected. settings-check checks the existing branches with it.
func (p *Policy) MustProtect(branch string) bool {
	if p.unusable != nil {
		return false
	}
	r := first(p.rules, branch)
	return r != nil && r.protected
}

// ProtectsBranches reports whether a rule requires protected branches.
func (p *Policy) ProtectsBranches() bool {
	return p.unusable == nil && slices.ContainsFunc(p.rules, func(r *rule) bool { return r.protected })
}

// helpOf is the help of a rule for its violation, nil without one.
func (r *rule) helpOf() []module.Help {
	if r.help == (module.Help{}) {
		return nil
	}
	return []module.Help{r.help}
}
