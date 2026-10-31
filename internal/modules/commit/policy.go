package commit

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Limits of a COMMITPOLICY file beyond its size.
const (
	maxRules       = 50
	maxExceptUsers = 50
)

// ruleYAML is a merged rule; checks stay raw so that a check type of a newer
// version is skipped on the anchor instead of breaking the whole policy.
type ruleYAML struct {
	Match        any            `yaml:"match"`
	CheckHistory string         `yaml:"check_history"`
	Except       exceptYAML     `yaml:"except"`
	Checks       map[string]any `yaml:"checks"`
	Message      string         `yaml:"message"`
	Help         module.Help    `yaml:"help"`
}

type exceptYAML struct {
	MergeCommit   bool     `yaml:"merge_commit"`
	MergedCommits bool     `yaml:"merged_commits"`
	GitReverts    bool     `yaml:"git_reverts"`
	Users         []string `yaml:"users"`
}

type level int

const (
	levelWarn level = iota + 1
	levelError
)

// Policy is a parsed COMMITPOLICY merged with its presets.
type Policy struct {
	warnMode bool
	rules    []*rule
	presets  []module.PresetRef
	origins  []module.RuleOrigin
	// unusable says why the rules cannot be used: a preset is missing or
	// broken. The module is then skipped with a warning.
	unusable error
}

// rule is a rule of the file: the checks for the branches of match.
type rule struct {
	name    string
	match   module.Match
	history string
	except  exceptYAML
	checks  []*check
	unknown []string
	message string
	help    module.Help
	source  []string
}

// check is a named check of a type of the catalogue in a rule.
type check struct {
	def      checkDef
	name     string
	level    level
	types    []string
	list     []string
	enum     string
	number   int
	location string
	pattern  module.Pattern
}

// Settings come from the server config.
type Settings struct {
	presets spec.Presets
}

// NewSettings decodes the presets of the module in the server config.
func NewSettings(rawPresets map[string]any) *Settings {
	return &Settings{presets: spec.NewPresets(rawPresets, presetSpec, checks, module.DefaultMaxPolicySize)}
}

// Parse decodes a COMMITPOLICY file offline and merges the presets it
// extends; s may be nil when there is no server config. A missing or broken
// preset does not fail Parse: the policy is unusable and the module skipped.
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
		r, err := newRule(name, y, res.Origin[name])
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", name, err)
		}
		p.rules = append(p.rules, r)
	}
	return p, nil
}

// Presets are the presets the file extends, for the audit log.
func (p *Policy) Presets() []module.PresetRef { return p.presets }

// Origins are the merged rules with their layers, for validate.
func (p *Policy) Origins() []module.RuleOrigin { return p.origins }

// newRule builds a merged rule that Spec accepted.
func newRule(name string, y ruleYAML, source []string) (*rule, error) {
	m, err := module.ParseMatch(y.Match, true)
	if err != nil {
		return nil, fmt.Errorf("match: %w", err)
	}
	r := &rule{name: name, match: m, history: y.CheckHistory, except: y.Except, message: strings.TrimSpace(y.Message),
		help: y.Help, source: source}
	for _, typ := range slices.Sorted(maps.Keys(y.Checks)) {
		if _, known := checkDefOf(typ); !known {
			r.unknown = append(r.unknown, typ)
		}
	}
	for _, d := range catalogue {
		raw, ok := y.Checks[d.name]
		if !ok {
			continue
		}
		var named map[string]map[string]any
		if err := strictyaml.DecodeValue(raw, &named, module.DefaultMaxPolicySize); err != nil {
			return nil, fmt.Errorf("checks.%s: %w", d.name, err)
		}
		for _, cname := range slices.Sorted(maps.Keys(named)) {
			c, err := newCheck(d, cname, named[cname])
			if err != nil {
				return nil, fmt.Errorf("checks.%s.%s: %w", d.name, cname, err)
			}
			r.checks = append(r.checks, c)
		}
	}
	return r, nil
}

var levels = map[string]level{"error": levelError, "warn": levelWarn}

// checkYAML holds every key a check of the catalogue may have; the check
// spec decides which ones a check has.
type checkYAML struct {
	Level    string   `yaml:"level"`
	Types    []string `yaml:"types"`
	Value    any      `yaml:"value"`
	Location string   `yaml:"location"`
}

// newCheck builds a check that the spec of its type accepted.
func newCheck(d checkDef, name string, raw map[string]any) (*check, error) {
	var y checkYAML
	if err := strictyaml.DecodeValue(raw, &y, module.DefaultMaxPolicySize); err != nil {
		return nil, err
	}
	c := &check{def: d, name: name, level: levels[y.Level], types: y.Types, location: y.Location}
	var err error
	switch d.name {
	case "type-enum", "scope-enum":
		err = strictyaml.DecodeValue(y.Value, &c.list, module.DefaultMaxPolicySize)
	case "header-max-length", "body-max-length", "trailer-max-length":
		err = strictyaml.DecodeValue(y.Value, &c.number, module.DefaultMaxPolicySize)
	case "header-format", "scope", "breaking-bang", "subject-starts-lowercase", "subject-trailing-dot",
		"body-leading-blank-lines", "body-required", "trailer-leading-blank-lines", "trailer-required":
		err = strictyaml.DecodeValue(y.Value, &c.enum, module.DefaultMaxPolicySize)
	case "pattern":
		var src string
		if err = strictyaml.DecodeValue(y.Value, &src, module.DefaultMaxPolicySize); err == nil {
			c.pattern, err = module.ParsePattern(src)
		}
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// label names the check in findings: its type and its name.
func (c *check) label() string { return c.def.name + " " + c.name }

// applies reports whether the check is for commits of the type.
func (c *check) applies(typ string) bool { return len(c.types) == 0 || slices.Contains(c.types, typ) }

// headerRequired reports whether a check of the rule requires the
// conventional format of the header.
func (r *rule) headerRequired() bool {
	return slices.ContainsFunc(r.checks, func(c *check) bool { return c.def.name == "header-format" && c.enum == "required" })
}

// mode is the value of the checks of a type for commits of the type: required
// wins over forbidden, without either it is optional.
func (r *rule) mode(name, typ string) string {
	mode := "optional"
	for _, c := range r.checks {
		if c.def.name != name || !c.applies(typ) {
			continue
		}
		switch c.enum {
		case "required":
			return "required"
		case "forbidden":
			mode = "forbidden"
		}
	}
	return mode
}

// parse reads a message as the checks of the rule say. A required trailer
// takes the whole paragraph after the header when the body is not required
// and an empty line may come before the trailer: then the body is empty.
func (r *rule) parse(raw string) Message {
	m := parseHeader(raw)
	whole := r.mode("body-required", m.Type) != "required" && r.mode("trailer-leading-blank-lines", m.Type) != "forbidden"
	m.split(r.mode("trailer-required", m.Type), whole)
	return m
}

// helpOf is the help of a rule for its violation, nil without one.
func (r *rule) helpOf() []module.Help {
	if r.help == (module.Help{}) {
		return nil
	}
	return []module.Help{r.help}
}
