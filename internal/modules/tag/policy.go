package tag

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
)

// Limits of a TAGPOLICY file.
const (
	maxRules = 50
	maxKeys  = 32
)

type ruleYAML struct {
	Match     any         `yaml:"match"`
	Signers   []string    `yaml:"signers"`
	Protected bool        `yaml:"protected"`
	Message   string      `yaml:"message"`
	Help      module.Help `yaml:"help"`
}

type policy struct {
	rules []*rule
	// keys are sorted by label.
	keys    []*key
	byLabel map[string]*key
	presets []module.PresetRef
	origins []module.RuleOrigin
}

type rule struct {
	name    string
	match   module.Match
	signers []string
	// protected requires the tag to be protected in GitLab when it is created.
	protected bool
	message   string
	help      module.Help
	source    []string
}

// Settings come from the server config.
type Settings struct {
	presets spec.Presets
}

// NewSettings decodes the presets of the module in the server config.
func NewSettings(rawPresets map[string]any) *Settings {
	return &Settings{presets: spec.NewPresets(rawPresets, presetSpec, checks, maxPolicySize)}
}

// parsePolicy decodes a TAGPOLICY file and merges the presets it extends; a
// missing or broken preset makes the file unusable, like a broken file.
func parsePolicy(data []byte, s *Settings) (*policy, error) {
	if s == nil {
		s = &Settings{}
	}
	res, err := spec.Resolve(data, maxPolicySize, Spec, checks, s.presets)
	if err != nil {
		return nil, err
	}
	p := &policy{byLabel: map[string]*key{}, presets: res.Refs(s.presets), origins: res.RuleOrigins()}
	if keys := res.Doc.Fields["keys"]; keys != nil {
		for _, label := range slices.Sorted(slices.Values(keys.Keys)) {
			k, err := parseKey(label, keys.Fields[label].Str)
			if err != nil {
				return nil, fmt.Errorf("keys.%s: %w", label, err)
			}
			p.keys = append(p.keys, k)
			p.byLabel[label] = k
		}
	}
	for _, name := range res.Rules() {
		var y ruleYAML
		if err := res.DecodeRule(name, &y); err != nil {
			return nil, err
		}
		m, err := module.ParseMatch(y.Match, false)
		if err != nil {
			return nil, fmt.Errorf("rule %s: match: %w", name, err)
		}
		for _, label := range y.Signers {
			if _, ok := p.byLabel[label]; !ok {
				return nil, fmt.Errorf("rule %s: signers: %q is not a label in keys of the file", name, label)
			}
		}
		p.rules = append(p.rules, &rule{name: name, match: m, signers: y.Signers, protected: y.Protected,
			message: strings.TrimSpace(y.Message), help: y.Help, source: res.Origin[name]})
	}
	return p, nil
}

// Presets are the presets the file extends, for the audit log.
func (p *policy) Presets() []module.PresetRef { return p.presets }

// Origins are the merged rules with their layers, for validate.
func (p *policy) Origins() []module.RuleOrigin { return p.origins }

func (r *rule) allows(label string) bool {
	for _, s := range r.signers {
		if s == label {
			return true
		}
	}
	return false
}

// NameRules is what TAGPOLICY requires of a tag name, for validate --tag.
type NameRules struct {
	Rules []string
	// Signers are the keys every matching rule allows, as "label (format)".
	Signers   []string
	Protected bool
	Source    []string
}

// RulesFor returns the rules of a tag name in a parsed TAGPOLICY; ok is
// false when no rule matches.
func RulesFor(p module.Policy, name string) (NameRules, bool) {
	tp, isTag := p.(*policy)
	if !isTag {
		return NameRules{}, false
	}
	rules := tp.matching(name)
	if len(rules) == 0 {
		return NameRules{}, false
	}
	out := NameRules{Source: sources(rules)}
	for _, r := range rules {
		out.Rules = append(out.Rules, r.name)
		out.Protected = out.Protected || r.protected
	}
	for _, l := range allowedSigners(rules) {
		out.Signers = append(out.Signers, tp.keysText([]string{l}))
	}
	return out, true
}

// matching are the rules whose match has the tag name.
func (p *policy) matching(name string) []*rule {
	var out []*rule
	for _, r := range p.rules {
		if r.match.All || r.match.Matches(name) {
			out = append(out, r)
		}
	}
	return out
}

// ValidateVersion checks the keys of a new version against time: an expired
// OpenPGP key is rejected in an added or changed entry and only warned about
// in an entry that the anchor version has unchanged, so that somebody else's
// expired key does not block an edit. Keys that no rule
// uses are warned about.
func (p *policy) ValidateVersion(anchor module.Policy, now time.Time) []module.Problem {
	prev, ok := anchor.(*policy)
	if !ok {
		prev = nil
	}
	used := map[string]bool{}
	for _, r := range p.rules {
		for _, s := range r.signers {
			used[s] = true
		}
	}
	var problems []module.Problem
	for _, k := range p.keys {
		if !used[k.label] {
			problems = append(problems, module.Problem{Warning: true,
				Msg: fmt.Sprintf("keys.%s is not used by any rule", k.label)})
		}
		if !k.expired(now) {
			continue
		}
		msg := fmt.Sprintf("keys.%s: the key %s has no signing key valid on %s: it expired; extend it or remove the entry",
			k.label, k.fingerprint, now.UTC().Format(time.DateOnly))
		unchanged := false
		if prev != nil {
			if old, ok := prev.byLabel[k.label]; ok && old.text == k.text {
				unchanged = true
			}
		}
		problems = append(problems, module.Problem{Warning: unchanged, Msg: msg})
	}
	return problems
}
