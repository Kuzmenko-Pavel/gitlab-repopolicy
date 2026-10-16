package tag

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Limits of a TAGPOLICY file.
const (
	maxRules        = 50
	maxKeys         = 32
	maxPatternBytes = 256
	maxMessage      = 1000
)

// fileYAML is what parsePolicy decodes once Spec accepts the file.
type fileYAML struct {
	Version int               `yaml:"version"`
	Rules   []ruleYAML        `yaml:"rules"`
	Keys    map[string]string `yaml:"keys"`
}

type ruleYAML struct {
	Name    string   `yaml:"name"`
	Tags    string   `yaml:"tags"`
	Signers []string `yaml:"signers"`
	Message string   `yaml:"message"`
}

type policy struct {
	rules []*rule
	// keys are sorted by label.
	keys    []*key
	byLabel map[string]*key
}

type rule struct {
	name    string
	tags    *regexp.Regexp
	signers []string
	message string
}

func parsePolicy(data []byte) (*policy, error) {
	tree, err := strictyaml.DecodeTree(data, maxPolicySize)
	if err != nil {
		return nil, err
	}
	if err := spec.Validate(Spec, tree, checks(tree)); err != nil {
		return nil, err
	}
	var f fileYAML
	if err := strictyaml.Decode(data, &f, maxPolicySize); err != nil {
		return nil, err
	}
	p := &policy{byLabel: map[string]*key{}}
	labels := make([]string, 0, len(f.Keys))
	for label := range f.Keys {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		k, err := parseKey(label, f.Keys[label])
		if err != nil {
			return nil, fmt.Errorf("keys.%s: %w", label, err)
		}
		p.keys = append(p.keys, k)
		p.byLabel[label] = k
	}
	for _, y := range f.Rules {
		re, err := compileTags(y.Tags)
		if err != nil {
			return nil, fmt.Errorf("rule %s: tags: %w", y.Name, err)
		}
		p.rules = append(p.rules, &rule{name: y.Name, tags: re, signers: y.Signers, message: strings.TrimSpace(y.Message)})
	}
	return p, nil
}

func (r *rule) allows(label string) bool {
	for _, s := range r.signers {
		if s == label {
			return true
		}
	}
	return false
}

// matching are the rules whose pattern matches the tag name.
func (p *policy) matching(name string) []*rule {
	var out []*rule
	for _, r := range p.rules {
		if r.tags.MatchString(name) {
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
