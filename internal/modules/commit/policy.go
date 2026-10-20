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

// ruleYAML is a merged rule; checks stay raw so that a check of a newer
// version is skipped on the anchor instead of breaking the whole policy.
type ruleYAML struct {
	Match   any                       `yaml:"match"`
	Parser  string                    `yaml:"parser"`
	Except  exceptYAML                `yaml:"except"`
	Checks  map[string]map[string]any `yaml:"checks"`
	Message string                    `yaml:"message"`
	Help    module.Help               `yaml:"help"`
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
	name         string
	match        module.Match
	conventional bool
	except       exceptYAML
	checks       []*check
	unknown      []string
	message      string
	help         module.Help
	source       []string
}

// check is a check of the catalogue in a rule.
type check struct {
	name     string
	level    level
	types    []string
	list     []string
	enum     string
	number   int
	location string
	keys     []string
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
	r := &rule{name: name, match: m, except: y.Except, message: strings.TrimSpace(y.Message),
		conventional: y.Parser == "conventional", help: y.Help, source: source}
	for _, cname := range slices.Sorted(maps.Keys(y.Checks)) {
		if _, known := catalogue[cname]; !known {
			r.unknown = append(r.unknown, cname)
			continue
		}
		c, err := newCheck(cname, y.Checks[cname], r.conventional)
		if err != nil {
			return nil, fmt.Errorf("checks.%s: %w", cname, err)
		}
		r.checks = append(r.checks, c)
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
	Keys     []string `yaml:"keys"`
}

// newCheck builds a check that the check spec of the catalogue accepted.
func newCheck(name string, raw map[string]any, conventional bool) (*check, error) {
	var y checkYAML
	if err := strictyaml.DecodeValue(raw, &y, module.DefaultMaxPolicySize); err != nil {
		return nil, err
	}
	if len(y.Types) > 0 && !conventional {
		return nil, errors.New("types: needs parser: conventional")
	}
	c := &check{name: name, level: levels[y.Level], types: y.Types, location: y.Location, keys: y.Keys}
	var err error
	switch name {
	case "type-enum", "scope-enum", "trailer-required":
		err = strictyaml.DecodeValue(y.Value, &c.list, module.DefaultMaxPolicySize)
	case "header-max-length", "body-max-line-length":
		err = strictyaml.DecodeValue(y.Value, &c.number, module.DefaultMaxPolicySize)
	case "scope", "breaking-bang", "subject-case", "subject-full-stop":
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

// helpOf is the help of a rule for its violation, nil without one.
func (r *rule) helpOf() []module.Help {
	if r.help == (module.Help{}) {
		return nil
	}
	return []module.Help{r.help}
}
