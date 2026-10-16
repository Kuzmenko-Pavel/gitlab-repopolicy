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
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
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

// fileYAML is what Parse decodes once Spec accepts the file.
type fileYAML struct {
	Version int        `yaml:"version"`
	Admins  []string   `yaml:"admins"`
	Rules   []ruleYAML `yaml:"rules"`
}

type ruleYAML struct {
	Name    string   `yaml:"name"`
	Paths   []string `yaml:"paths"`
	Refs    any      `yaml:"refs"`
	Allow   []string `yaml:"allow"`
	Message string   `yaml:"message"`
}

// Policy is a parsed PATHPOLICY.
type Policy struct {
	admins []string
	rules  []*rule
}

type rule struct {
	name    string
	paths   []string
	refs    module.RefScope
	allow   []string
	message string
}

// Parse decodes a PATHPOLICY file offline.
func Parse(data []byte) (*Policy, error) {
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
	p := &Policy{admins: f.Admins}
	for _, y := range f.Rules {
		refs, err := module.ParseRefScope(y.Refs)
		if err != nil {
			return nil, fmt.Errorf("rule %s: refs: %w", y.Name, err)
		}
		p.rules = append(p.rules, &rule{name: y.Name, paths: y.Paths, refs: refs, allow: y.Allow, message: strings.TrimSpace(y.Message)})
	}
	return p, nil
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
	// Refs is the ref scope as written in the policy.
	Refs string
	// Applies is false when the scope excludes the ref.
	Applies bool
	Allow   []string
	// Allowed reports whether the user is in allow.
	Allowed bool
}

// ExplainPath returns the rules that match the path, for gitlab-repopolicy
// explain; every applying rule must allow the user (AND). PATHPOLICY itself
// is changed only by admins.
func (p *Policy) ExplainPath(ctx context.Context, path, ref, username string, refs module.RefClassifier) ([]PathRule, module.Status) {
	var out []PathRule
	if isPolicyPath(path) {
		out = append(out, PathRule{Name: "admins of " + File, Refs: "all", Applies: true,
			Allow: p.admins, Allowed: p.isAdmin(username)})
	}
	for _, r := range p.rules {
		if !r.matches(path) {
			continue
		}
		applies, st := r.appliesTo(ctx, ref, refs)
		if !st.IsConfirmed() {
			return nil, st
		}
		out = append(out, PathRule{Name: r.name, Refs: r.refs.String(), Applies: applies, Allow: r.allow, Allowed: r.allows(username)})
	}
	return out, module.Confirmed()
}

// appliesTo reports whether the ref scope of the rule includes the ref.
func (r *rule) appliesTo(ctx context.Context, ref string, refs module.RefClassifier) (bool, module.Status) {
	switch r.refs.Kind {
	case module.RefsList:
		return r.refs.MatchList(ref), module.Confirmed()
	case module.RefsDefault:
		return ref == refs.Default(), module.Confirmed()
	case module.RefsProtected:
		if ref == refs.Default() {
			return true, module.Confirmed()
		}
		class, st := refs.Class(ctx, ref)
		return class != module.ClassOther, st
	}
	return true, module.Confirmed()
}
