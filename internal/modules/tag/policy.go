package tag

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Limits of architecture §17.
const (
	maxRules        = 50
	maxKeys         = 32
	maxPatternBytes = 256
	maxMessage      = 1000
)

// fileYAML is TAGPOLICY version 1; the schema is frozen (architecture §18.5).
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

var (
	// keyLabel has the characters of a GitLab username, which is the recommended label.
	keyLabel = regexp.MustCompile(labelPattern)
)

func parsePolicy(data []byte) (*policy, error) {
	var f fileYAML
	if err := strictyaml.Decode(data, &f, maxPolicySize); err != nil {
		return nil, err
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("version: %d is not supported, use 1", f.Version)
	}
	p := &policy{byLabel: map[string]*key{}}
	if err := p.parseKeys(f.Keys); err != nil {
		return nil, err
	}
	if len(f.Rules) > maxRules {
		return nil, fmt.Errorf("rules: %d rules, the limit is %d", len(f.Rules), maxRules)
	}
	seen := map[string]bool{}
	for i, ry := range f.Rules {
		r, err := p.parseRule(ry)
		if err != nil {
			name := ry.Name
			if !module.ValidRuleName(name) {
				name = fmt.Sprintf("#%d", i+1)
			}
			return nil, fmt.Errorf("rule %s: %w", name, err)
		}
		if seen[r.name] {
			return nil, fmt.Errorf("rule %s: the name is used twice", r.name)
		}
		seen[r.name] = true
		p.rules = append(p.rules, r)
	}
	return p, nil
}

func (p *policy) parseKeys(keys map[string]string) error {
	if len(keys) == 0 || len(keys) > maxKeys {
		return fmt.Errorf("keys: 1 to %d keys are required", maxKeys)
	}
	labels := make([]string, 0, len(keys))
	for label := range keys {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	byFingerprint := map[string]string{}
	for _, label := range labels {
		if !keyLabel.MatchString(label) {
			return fmt.Errorf("keys: label %q must match [A-Za-z0-9_.-]{1,255}", label)
		}
		k, err := parseKey(label, keys[label])
		if err != nil {
			return fmt.Errorf("keys.%s: %w", label, err)
		}
		if other, ok := byFingerprint[k.fingerprint]; ok {
			return fmt.Errorf("keys.%s: the same key as keys.%s (%s)", label, other, k.fingerprint)
		}
		byFingerprint[k.fingerprint] = label
		p.keys = append(p.keys, k)
		p.byLabel[label] = k
	}
	return nil
}

func (p *policy) parseRule(y ruleYAML) (*rule, error) {
	if !module.ValidRuleName(y.Name) {
		return nil, fmt.Errorf("name: %q must match [a-z0-9-]{1,63}", y.Name)
	}
	r := &rule{name: y.Name, message: strings.TrimSpace(y.Message)}
	switch {
	case y.Tags == "":
		return nil, fmt.Errorf("tags: a regular expression is required")
	case len(y.Tags) > maxPatternBytes:
		return nil, fmt.Errorf("tags: %d bytes, the limit is %d", len(y.Tags), maxPatternBytes)
	}
	if _, err := regexp.Compile(y.Tags); err != nil {
		return nil, fmt.Errorf("tags: %w", err)
	}
	re, err := regexp.Compile(`^(?:` + y.Tags + `)$`)
	if err != nil {
		return nil, fmt.Errorf("tags: %w", err)
	}
	r.tags = re
	if len(y.Signers) == 0 || len(y.Signers) > maxKeys {
		return nil, fmt.Errorf("signers: 1 to %d labels of keys are required", maxKeys)
	}
	seen := map[string]bool{}
	for _, s := range y.Signers {
		if _, ok := p.byLabel[s]; !ok {
			return nil, fmt.Errorf("signers: %q is not a label in keys", s)
		}
		if seen[s] {
			return nil, fmt.Errorf("signers: %q is listed twice", s)
		}
		seen[s] = true
	}
	r.signers = y.Signers
	if n := utf8.RuneCountInString(r.message); n > maxMessage {
		return nil, fmt.Errorf("message: %d characters, the limit is %d", n, maxMessage)
	}
	return r, nil
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
// expired key does not block an edit (architecture §11.3). Keys that no rule
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
