package path

import (
	"context"
	"errors"
	"fmt"
	pathpkg "path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Limits of architecture §17.
const (
	maxRules        = 100
	maxPaths        = 50
	maxPatternBytes = 256
	maxDoubleStars  = 4
	maxAllow        = 50
	maxAdmins       = 5
	maxMessage      = 1000
)

// fileYAML is PATHPOLICY version 1; the schema is frozen (architecture §18.5).
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

type refsKind int

const (
	refsAll refsKind = iota
	refsDefault
	refsProtected
	refsList
)

// Policy is a parsed PATHPOLICY.
type Policy struct {
	admins []string
	rules  []*rule
}

type rule struct {
	name    string
	paths   []string
	refs    refsKind
	list    []string
	allow   []string
	message string
}

var (
	ruleName = regexp.MustCompile(`^[a-z0-9-]{1,63}$`)
)

// Parse decodes a PATHPOLICY file offline.
func Parse(data []byte) (*Policy, error) {
	var f fileYAML
	if err := strictyaml.Decode(data, &f, module.DefaultMaxPolicySize); err != nil {
		return nil, err
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("version: %d is not supported, use 1", f.Version)
	}
	admins, err := usernames("admins", f.Admins, 1, maxAdmins)
	if err != nil {
		return nil, err
	}
	p := &Policy{admins: admins}
	if len(f.Rules) > maxRules {
		return nil, fmt.Errorf("rules: %d rules, the limit is %d", len(f.Rules), maxRules)
	}
	seen := map[string]bool{}
	for i, ry := range f.Rules {
		r, err := parseRule(ry)
		if err != nil {
			name := ry.Name
			if !ruleName.MatchString(name) {
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

func parseRule(y ruleYAML) (*rule, error) {
	if !ruleName.MatchString(y.Name) {
		return nil, fmt.Errorf("name: %q must match [a-z0-9-]{1,63}", y.Name)
	}
	r := &rule{name: y.Name, message: strings.TrimSpace(y.Message)}
	if len(y.Paths) == 0 || len(y.Paths) > maxPaths {
		return nil, fmt.Errorf("paths: 1 to %d patterns are required", maxPaths)
	}
	for _, p := range y.Paths {
		if err := validatePattern(p); err != nil {
			return nil, fmt.Errorf("paths: %q: %w", p, err)
		}
	}
	r.paths = y.Paths
	if err := r.parseRefs(y.Refs); err != nil {
		return nil, err
	}
	allow, err := usernames("allow", y.Allow, 1, maxAllow)
	if err != nil {
		return nil, err
	}
	r.allow = allow
	if n := utf8.RuneCountInString(r.message); n > maxMessage {
		return nil, fmt.Errorf("message: %d characters, the limit is %d", n, maxMessage)
	}
	return r, nil
}

// usernames checks a list of principals: only usernames of people or bots,
// no groups or roles (architecture D29), compared case-insensitively.
func usernames(field string, list []string, low, high int) ([]string, error) {
	if len(list) < low || len(list) > high {
		return nil, fmt.Errorf("%s: %d to %d usernames are required", field, low, high)
	}
	seen := map[string]bool{}
	for _, u := range list {
		if strings.ContainsAny(u, "/@:") {
			return nil, fmt.Errorf("%s: %q is not a username; groups and roles are not supported, list the people", field, u)
		}
		if !module.ValidUsername(u) {
			return nil, fmt.Errorf("%s: %q is not a GitLab username", field, u)
		}
		key := strings.ToLower(u)
		if seen[key] {
			return nil, fmt.Errorf("%s: %q is listed twice", field, u)
		}
		seen[key] = true
	}
	return list, nil
}

func (r *rule) parseRefs(v any) error {
	switch refs := v.(type) {
	case nil:
		r.refs = refsAll
	case string:
		switch refs {
		case "all":
			r.refs = refsAll
		case "default":
			r.refs = refsDefault
		case "protected":
			r.refs = refsProtected
		default:
			return fmt.Errorf("refs: %q is not one of all, default, protected or a list of refs", refs)
		}
	case []any:
		if len(refs) == 0 {
			return errors.New("refs: the list is empty")
		}
		r.refs = refsList
		for _, item := range refs {
			s, ok := item.(string)
			if !ok || !strings.HasPrefix(s, "refs/") {
				return fmt.Errorf("refs: %v is not a ref pattern such as refs/heads/release/*", item)
			}
			if _, err := pathpkg.Match(s, ""); err != nil {
				return fmt.Errorf("refs: %q: %w", s, err)
			}
			r.list = append(r.list, s)
		}
	default:
		return errors.New("refs: expected all, default, protected or a list of refs")
	}
	return nil
}

func (r *rule) matchList(ref string) bool {
	for _, p := range r.list {
		// parseRefs checks the patterns, so Match cannot fail here.
		if ok, err := pathpkg.Match(p, ref); err == nil && ok {
			return true
		}
	}
	return false
}

// validatePattern applies architecture §9.3: relative, no "\", no ".", ".."
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

// Usernames are all principals of the policy, for D28 at acceptance.
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
// is changed only by admins (architecture §9.4).
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
		out = append(out, PathRule{Name: r.name, Refs: r.refsText(), Applies: applies, Allow: r.allow, Allowed: r.allows(username)})
	}
	return out, module.Confirmed()
}

// appliesTo reports whether the ref scope of the rule includes the ref.
func (r *rule) appliesTo(ctx context.Context, ref string, refs module.RefClassifier) (bool, module.Status) {
	switch r.refs {
	case refsList:
		return r.matchList(ref), module.Confirmed()
	case refsDefault:
		return ref == refs.Default(), module.Confirmed()
	case refsProtected:
		if ref == refs.Default() {
			return true, module.Confirmed()
		}
		class, st := refs.Class(ctx, ref)
		return class != module.ClassOther, st
	}
	return true, module.Confirmed()
}

func (r *rule) refsText() string {
	switch r.refs {
	case refsList:
		return strings.Join(r.list, ", ")
	case refsDefault:
		return "default"
	case refsProtected:
		return "protected"
	}
	return "all"
}
