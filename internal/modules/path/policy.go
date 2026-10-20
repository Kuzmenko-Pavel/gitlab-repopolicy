package path

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
)

// Limits of a PATHPOLICY file.
const (
	maxRules        = 100
	maxPaths        = 50
	maxPatternBytes = 256
	maxDoubleStars  = 4
	maxAllow        = 50
	maxAdmins       = 5
	maxMessage      = 1000
)

type ruleYAML struct {
	Match   any         `yaml:"match"`
	Paths   []string    `yaml:"paths"`
	Allow   []string    `yaml:"allow"`
	Message string      `yaml:"message"`
	Help    module.Help `yaml:"help"`
}

// Policy is a parsed PATHPOLICY merged with its presets.
type Policy struct {
	admins  []string
	rules   []*rule
	presets []module.PresetRef
}

type rule struct {
	name    string
	match   module.Match
	paths   []string
	allow   []string
	message string
	help    module.Help
	// source lists the layers of the rule, "preset:<name>" or "file".
	source []string
}

// Settings come from the server config.
type Settings struct {
	presets spec.Presets
}

// NewSettings decodes the presets of the module in the server config.
func NewSettings(rawPresets map[string]any) *Settings {
	return &Settings{presets: spec.NewPresets(rawPresets, presetSpec, checks, module.DefaultMaxPolicySize)}
}

// Parse decodes a PATHPOLICY file offline and merges the presets it extends;
// s may be nil when there is no server config. A missing or broken preset
// makes the file unusable, like a broken file.
func Parse(data []byte, s *Settings) (*Policy, error) {
	if s == nil {
		s = &Settings{}
	}
	res, err := spec.Resolve(data, module.DefaultMaxPolicySize, Spec, checks, s.presets)
	if err != nil {
		return nil, err
	}
	p := &Policy{presets: res.Refs(s.presets)}
	for _, a := range res.Doc.Fields["admins"].Items {
		p.admins = append(p.admins, a.Str)
	}
	for _, name := range res.Rules() {
		var y ruleYAML
		if err := res.DecodeRule(name, &y); err != nil {
			return nil, err
		}
		m, err := module.ParseMatch(y.Match, true)
		if err != nil {
			return nil, fmt.Errorf("rule %s: match: %w", name, err)
		}
		p.rules = append(p.rules, &rule{name: name, match: m, paths: y.Paths, allow: y.Allow,
			message: strings.TrimSpace(y.Message), help: y.Help, source: res.Origin[name]})
	}
	return p, nil
}

// Presets are the presets the file extends, for the audit log.
func (p *Policy) Presets() []module.PresetRef { return p.presets }

// helpOf is the help of a rule for its violation, nil without one.
func (r *rule) helpOf() []module.Help {
	if r.help == (module.Help{}) {
		return nil
	}
	return []module.Help{r.help}
}

// validatePattern checks a path pattern: relative, no "\", no ".", ".."
// or empty segments, UTF-8, at most 256 bytes and four "**"; no negation and
// no {a,b}, unlike gitignore and CODEOWNERS.
func validatePattern(p string) error {
	switch {
	case p == "":
		return errors.New("empty pattern")
	case len(p) > maxPatternBytes:
		return fmt.Errorf("%d bytes, the limit is %d", len(p), maxPatternBytes)
	case !utf8.ValidString(p):
		return errors.New("not valid UTF-8")
	case strings.HasPrefix(p, "/"):
		return errors.New("patterns are relative to the repository root, remove the leading /")
	case strings.Contains(p, `\`):
		return errors.New(`"\" is not allowed`)
	case strings.HasPrefix(p, "!"):
		return errors.New("negation is not supported")
	case strings.ContainsAny(p, "{}"):
		return errors.New("{a,b} alternatives are not supported")
	case strings.Count(p, "**") > maxDoubleStars:
		return fmt.Errorf(`more than %d "**"`, maxDoubleStars)
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".", "..":
			return fmt.Errorf("segment %q is not allowed", seg)
		}
	}
	if !doublestar.ValidatePattern(p) {
		return errors.New("invalid glob")
	}
	return nil
}

// match reports whether path matches the pattern: anchored at the root,
// byte-wise and case-sensitive, "*" within a segment, "**" across segments.
func match(pattern, path string) bool {
	ok, err := doublestar.Match(pattern, path)
	return err == nil && ok
}

func (r *rule) matches(path string) bool {
	for _, p := range r.paths {
		if match(p, path) {
			return true
		}
	}
	return false
}

func (r *rule) allows(name string) bool {
	for _, a := range r.allow {
		if strings.EqualFold(a, name) {
			return true
		}
	}
	return false
}

func (p *Policy) isAdmin(name string) bool {
	for _, a := range p.admins {
		if strings.EqualFold(a, name) {
			return true
		}
	}
	return false
}

// Covers reports whether a rule of p guards the path.
func (*Module) Covers(p module.Policy, path string) bool {
	pol, ok := p.(*Policy)
	return ok && pol.covers(path)
}

// covers reports whether a rule guards the path; the core warns about
// delegated policy files that no rule covers.
func (p *Policy) covers(path string) bool {
	for _, r := range p.rules {
		if r.matches(path) {
			return true
		}
	}
	return false
}

// Usernames are all principals of the policy: at acceptance only the ones
// that the anchor version does not have are looked up.
func (p *Policy) Usernames() []string {
	out := append([]string{}, p.admins...)
	for _, r := range p.rules {
		out = append(out, r.allow...)
	}
	return out
}

// PathRule is a rule that matches a path, for gitlab-repopolicy explain.
type PathRule struct {
	Name string
	// Match is the match field as written in the policy.
	Match string
	// Applies is false when the rule does not apply to the ref.
	Applies bool
	Allow   []string
	// Allowed reports whether the user is in allow.
	Allowed bool
	// Source lists the layers of the rule: "preset:<name>" or "file".
	Source []string
}

// ExplainPath returns the rules that match the path, for gitlab-repopolicy
// explain; every applying rule must allow the user (AND). PATHPOLICY itself
// is changed only by admins.
func (p *Policy) ExplainPath(ctx context.Context, path, ref, username string, refs module.RefClassifier) ([]PathRule, module.Status) {
	var out []PathRule
	if isPolicyPath(path) {
		out = append(out, PathRule{Name: "admins of " + File, Match: "all", Applies: true,
			Allow: p.admins, Allowed: p.isAdmin(username), Source: []string{spec.FileLayer}})
	}
	for _, r := range p.rules {
		if !r.matches(path) {
			continue
		}
		applies, st := r.appliesTo(ctx, ref, refs)
		if !st.IsConfirmed() {
			return nil, st
		}
		out = append(out, PathRule{Name: r.name, Match: r.match.String(), Applies: applies, Allow: r.allow,
			Allowed: r.allows(username), Source: r.source})
	}
	return out, module.Confirmed()
}

// appliesTo reports whether the rule applies to the ref: match all applies to
// every branch, default to the default branch, a list to the branches it
// matches. Tags are checked by TAGPOLICY only.
func (r *rule) appliesTo(_ context.Context, ref string, refs module.RefClassifier) (bool, module.Status) {
	name, ok := strings.CutPrefix(ref, "refs/heads/")
	switch {
	case !ok:
		return false, module.Confirmed()
	case r.match.Default:
		return ref == refs.Default(), module.Confirmed()
	case r.match.IsList():
		return r.match.Matches(name), module.Confirmed()
	}
	return true, module.Confirmed()
}
