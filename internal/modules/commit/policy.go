package commit

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/dlclark/regexp2/v2"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Limits of a COMMITPOLICY file beyond its size.
const (
	maxRulesets    = 50
	maxExceptUsers = 50
)

// fileYAML is what Parse decodes once Spec accepts the file; rules stay raw
// so that a rule from a newer version is skipped on the anchor instead of
// breaking the whole policy.
type fileYAML struct {
	Version  int           `yaml:"version"`
	Mode     string        `yaml:"mode"`
	Extends  string        `yaml:"extends"`
	Rulesets []rulesetYAML `yaml:"rulesets"`
}

type rulesetYAML struct {
	Name    string                    `yaml:"name"`
	Refs    any                       `yaml:"refs"`
	Format  string                    `yaml:"format"`
	Except  exceptYAML                `yaml:"except"`
	Rules   map[string]map[string]any `yaml:"rules"`
	Message string                    `yaml:"message"`
}

type exceptYAML struct {
	MergeCommits  bool     `yaml:"merge_commits"`
	GitReverts    bool     `yaml:"git_reverts"`
	MergedCommits bool     `yaml:"merged_commits"`
	Users         []string `yaml:"users"`
}

// presetYAML is an entry of commitpolicy_presets in the server config.
type presetYAML struct {
	Rules map[string]map[string]any `yaml:"rules"`
}

type level int

const (
	levelOff level = iota
	levelWarn
	levelError
)

func (l level) String() string { return [...]string{"off", "warn", "error"}[l] }

// Policy is a parsed COMMITPOLICY.
type Policy struct {
	warnMode bool
	rulesets []*ruleset
	// unknownPreset is the extends value that the server config does not define.
	unknownPreset string
	settings      *Settings
}

type ruleset struct {
	name         string
	refs         module.RefScope
	conventional bool
	except       exceptYAML
	rules        []*rule
	unknown      []string
	message      string
}

type rule struct {
	name     string
	level    level
	types    []string
	list     []string
	enum     string
	number   int
	location string
	keys     []string
	re       *regexp.Regexp
	pattern  *regexp2.Regexp
	source   string
	examples examplesYAML
}

type examplesYAML struct {
	Valid   []string `yaml:"valid"`
	Invalid []string `yaml:"invalid"`
}

// Parse decodes a COMMITPOLICY file offline with the presets of s.
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
	p := &Policy{settings: s, warnMode: f.Mode == "warn"}
	var preset map[string]map[string]any
	if f.Extends != "" {
		var ok bool
		if preset, ok = s.presets[f.Extends]; !ok {
			p.unknownPreset = f.Extends
		}
	}
	for _, y := range f.Rulesets {
		r, err := newRuleset(y, preset)
		if err != nil {
			return nil, fmt.Errorf("ruleset %s: %w", y.Name, err)
		}
		p.rulesets = append(p.rulesets, r)
	}
	return p, nil
}

// newRuleset builds a ruleset that Spec accepted, with the rules of the preset.
func newRuleset(y rulesetYAML, preset map[string]map[string]any) (*ruleset, error) {
	r := &ruleset{name: y.Name, except: y.Except, message: strings.TrimSpace(y.Message), conventional: y.Format != "free"}
	refs, err := module.ParseRefScope(y.Refs)
	if err != nil {
		return nil, fmt.Errorf("refs: %w", err)
	}
	r.refs = refs
	// Rules of the preset come first; rules of the file override them by name.
	merged := map[string]map[string]any{}
	for name, raw := range preset {
		if r.conventional || !catalogue[name].conventional {
			merged[name] = raw
		}
	}
	maps.Copy(merged, y.Rules)
	for _, name := range slices.Sorted(maps.Keys(merged)) {
		if _, known := catalogue[name]; !known {
			r.unknown = append(r.unknown, name)
			continue
		}
		ru, err := newRule(name, merged[name], r.conventional)
		if err != nil {
			return nil, fmt.Errorf("rules.%s: %w", name, err)
		}
		if ru.level != levelOff {
			r.rules = append(r.rules, ru)
		}
	}
	return r, nil
}

var levels = map[string]level{"error": levelError, "warn": levelWarn, "off": levelOff}

// ruleYAML holds every key a rule of the catalogue may have; the rule spec
// decides which ones a rule has.
type ruleYAML struct {
	Level    string       `yaml:"level"`
	Types    []string     `yaml:"types"`
	Value    any          `yaml:"value"`
	Location string       `yaml:"location"`
	Pattern  string       `yaml:"pattern"`
	Keys     []string     `yaml:"keys"`
	Examples examplesYAML `yaml:"examples"`
}

// newRule builds a rule that the rule spec of the catalogue accepted.
func newRule(name string, raw map[string]any, conventional bool) (*rule, error) {
	var y ruleYAML
	if err := strictyaml.DecodeValue(raw, &y, module.DefaultMaxPolicySize); err != nil {
		return nil, err
	}
	if len(y.Types) > 0 && !conventional {
		return nil, errors.New("types: needs format: conventional")
	}
	r := &rule{name: name, level: levels[y.Level], types: y.Types, location: y.Location, keys: y.Keys, examples: y.Examples}
	var err error
	switch name {
	case "type-enum", "scope-enum", "trailer-required":
		err = strictyaml.DecodeValue(y.Value, &r.list, module.DefaultMaxPolicySize)
	case "header-max-length", "body-max-line-length":
		err = strictyaml.DecodeValue(y.Value, &r.number, module.DefaultMaxPolicySize)
	case "scope", "breaking-bang", "subject-case", "subject-full-stop":
		err = strictyaml.DecodeValue(y.Value, &r.enum, module.DefaultMaxPolicySize)
	case "pattern":
		var src string
		if err = strictyaml.DecodeValue(y.Value, &src, module.DefaultMaxPolicySize); err == nil {
			r.pattern, err = regexp2.Compile(src, regexp2.RE2)
			r.source = src
		}
	case "reference":
		r.re, err = regexp.Compile(y.Pattern)
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}
