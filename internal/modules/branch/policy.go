package branch

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

type fileYAML struct {
	Version int        `yaml:"version"`
	Extends string     `yaml:"extends"`
	Mode    string     `yaml:"mode"`
	Rules   []ruleYAML `yaml:"rules"`
}

type ruleYAML struct {
	Name       string       `yaml:"name"`
	Match      string       `yaml:"match"`
	Deny       bool         `yaml:"deny"`
	Pattern    string       `yaml:"pattern"`
	UserPrefix bool         `yaml:"user_prefix"`
	Protected  bool         `yaml:"protected"`
	Examples   examplesYAML `yaml:"examples"`
	Message    string       `yaml:"message"`
}

type examplesYAML struct {
	Valid   []string `yaml:"valid"`
	Invalid []string `yaml:"invalid"`
}

type presetYAML struct {
	Rules []ruleYAML `yaml:"rules"`
}

// Policy is a parsed BRANCHPOLICY with the rules of its preset.
type Policy struct {
	warnMode bool
	// rules are the final list: new rules of the file, then the preset with
	// the rules the file replaces by name.
	rules []*rule
	// unusable says why the final list cannot be used: a missing preset or a
	// list that fails its checks. The module is then skipped with a warning.
	unusable error
}

type rule struct {
	name       string
	match      string
	deny       bool
	pattern    *regexp.Regexp
	userPrefix bool
	protected  bool
	examples   examplesYAML
	message    string
}

func newRule(y ruleYAML) (*rule, error) {
	r := &rule{name: y.Name, match: y.Match, deny: y.Deny, userPrefix: y.UserPrefix, protected: y.Protected,
		examples: y.Examples, message: strings.TrimSpace(y.Message)}
	if y.Pattern != "" {
		re, err := regexp.Compile(`^(?:` + y.Pattern + `)$`)
		if err != nil {
			return nil, fmt.Errorf("rule %s: pattern: %w", y.Name, err)
		}
		r.pattern = re
	}
	return r, nil
}

func newRules(ys []ruleYAML) ([]*rule, error) {
	out := make([]*rule, 0, len(ys))
	for _, y := range ys {
		r, err := newRule(y)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Parse decodes a BRANCHPOLICY file offline with the presets of s.
func Parse(data []byte, s *Settings) (*Policy, error) {
	tree, err := strictyaml.DecodeTree(data, module.DefaultMaxPolicySize)
	if err != nil {
		return nil, err
	}
	if err := spec.Validate(Spec, tree, checks); err != nil {
		return nil, err
	}
	var f fileYAML
	if err := strictyaml.Decode(data, &f, module.DefaultMaxPolicySize); err != nil {
		return nil, err
	}
	own, err := newRules(f.Rules)
	if err != nil {
		return nil, err
	}
	p := &Policy{warnMode: f.Mode == "warn"}
	var preset []*rule
	if f.Extends != "" {
		var ok bool
		if preset, ok = s.presets[f.Extends]; !ok {
			p.unusable = fmt.Errorf("extends: preset %q is not defined in the server config", f.Extends)
			if err, broken := s.presetErrors[f.Extends]; broken {
				p.unusable = fmt.Errorf("extends: preset %q of the server config is invalid: %w", f.Extends, err)
			}
			return p, nil
		}
	}
	p.rules = merge(own, preset)
	p.unusable = check(p.rules)
	return p, nil
}

// merge puts the new rules of the file first, then the preset; a rule of the
// file with the name of a preset rule takes its place, so that a catch-all of
// the preset stays last when the file overrides it.
func merge(own, preset []*rule) []*rule {
	byName := map[string]*rule{}
	for _, r := range own {
		byName[r.name] = r
	}
	inPreset := map[string]bool{}
	for _, r := range preset {
		inPreset[r.name] = true
	}
	var out []*rule
	for _, r := range own {
		if !inPreset[r.name] {
			out = append(out, r)
		}
	}
	for _, r := range preset {
		if o, ok := byName[r.name]; ok {
			r = o
		}
		out = append(out, r)
	}
	return out
}

// check finds what makes a final list unusable: rules after a catch-all are
// never reached, a repeated match is shadowed, and examples show what the
// pattern and the order of the rules do with a name.
func check(rules []*rule) error {
	var errs []error
	if len(rules) > maxRules {
		errs = append(errs, fmt.Errorf("%d rules with the preset, the limit is %d", len(rules), maxRules))
	}
	seen := map[string]string{}
	for i, r := range rules {
		if i > 0 && rules[i-1].match == "*" {
			errs = append(errs, fmt.Errorf("rule %s: comes after the rule %s with match \"*\" and is never used", r.name, rules[i-1].name))
		}
		if other, ok := seen[r.match]; ok {
			errs = append(errs, fmt.Errorf("rule %s: match %q is already the match of rule %s", r.name, r.match, other))
		}
		seen[r.match] = r.name
		for _, name := range r.examples.Valid {
			switch got := first(rules, name); {
			case got != r:
				errs = append(errs, fmt.Errorf("rule %s: the valid example %q is taken by the rule %s above", r.name, name, nameOf(got)))
			case !r.pattern.MatchString(name):
				errs = append(errs, fmt.Errorf("rule %s: pattern does not match the valid example %q", r.name, name))
			}
		}
		for _, name := range r.examples.Invalid {
			if r.pattern.MatchString(name) {
				errs = append(errs, fmt.Errorf("rule %s: pattern matches the invalid example %q", r.name, name))
			}
		}
	}
	return errors.Join(errs...)
}

func nameOf(r *rule) string {
	if r == nil {
		return "none"
	}
	return r.name
}

// first is the rule of the class a branch name falls into.
func first(rules []*rule, name string) *rule {
	for _, r := range rules {
		if module.MatchPattern(r.match, name) {
			return r
		}
	}
	return nil
}

// Validate reports a final list that cannot be used: a new version of the
// file with it is rejected.
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

// ProtectedBranches are the match patterns of rules with protected: the
// project must protect them, settings-check checks that it does.
func (p *Policy) ProtectedBranches() []string {
	if p.unusable != nil {
		return nil
	}
	var out []string
	for _, r := range p.rules {
		if r.protected {
			out = append(out, r.match)
		}
	}
	return out
}
