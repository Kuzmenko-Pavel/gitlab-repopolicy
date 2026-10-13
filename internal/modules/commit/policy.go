package commit

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2/v2"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Limits of a COMMITPOLICY file beyond its size.
const (
	maxRulesets    = 50
	maxExceptUsers = 50
)

// fileYAML is the decoded file; rules stay raw so that a rule from a newer
// version is skipped on the anchor instead of breaking the whole policy.
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

type refsKind int

const (
	refsAll refsKind = iota
	refsDefault
	refsProtected
	refsList
)

type policy struct {
	warnMode bool
	rulesets []*ruleset
	// unknownPreset is the extends value that the server config does not define.
	unknownPreset string
	settings      *Settings
}

type ruleset struct {
	name         string
	refs         refsKind
	list         []string
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

// catalogue describes the rules of v1 (architecture §10.1): the value kind and
// whether the rule needs the conventional format.
var catalogue = map[string]struct {
	value        string // list, int, enum:<a>|<b>, none, regexp2
	conventional bool
}{
	"type-enum":            {"list", true},
	"scope":                {"enum:optional|required|forbidden", true},
	"scope-enum":           {"list", true},
	"breaking-bang":        {"enum:allow|forbid", true},
	"header-max-length":    {"int", false},
	"subject-case":         {"enum:lower-first|sentence|any", false},
	"subject-full-stop":    {"enum:forbid|require", false},
	"body-required":        {"none", false},
	"body-max-line-length": {"int", false},
	"reference":            {"reference", false},
	"trailer-required":     {"list", false},
	"pattern":              {"regexp2", false},
}

func parsePolicy(data []byte, s *Settings) (*policy, error) {
	var f fileYAML
	if err := strictyaml.Decode(data, &f, module.DefaultMaxPolicySize); err != nil {
		return nil, err
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("version: %d is not supported, use 1", f.Version)
	}
	p := &policy{settings: s}
	switch f.Mode {
	case "", "enforce":
	case "warn":
		p.warnMode = true
	default:
		return nil, fmt.Errorf("mode: %q is not one of warn, enforce", f.Mode)
	}
	var preset map[string]map[string]any
	if f.Extends != "" {
		var ok bool
		if preset, ok = s.presets[f.Extends]; !ok {
			p.unknownPreset = f.Extends
		}
	}
	if len(f.Rulesets) == 0 {
		return nil, errors.New("rulesets: at least one ruleset is required")
	}
	if len(f.Rulesets) > maxRulesets {
		return nil, fmt.Errorf("rulesets: %d rulesets, the limit is %d", len(f.Rulesets), maxRulesets)
	}
	names := map[string]bool{}
	for i, rs := range f.Rulesets {
		r, err := parseRuleset(rs, preset)
		if err != nil {
			name := rs.Name
			if name == "" {
				name = "#" + strconv.Itoa(i+1)
			}
			return nil, fmt.Errorf("ruleset %s: %w", name, err)
		}
		if names[r.name] {
			return nil, fmt.Errorf("ruleset %s: the name is used twice", r.name)
		}
		names[r.name] = true
		p.rulesets = append(p.rulesets, r)
	}
	return p, nil
}

func parseRuleset(y rulesetYAML, preset map[string]map[string]any) (*ruleset, error) {
	if y.Name == "" {
		return nil, errors.New("name is required")
	}
	r := &ruleset{name: y.Name, except: y.Except, message: strings.TrimSpace(y.Message)}
	switch y.Format {
	case "", "conventional":
		r.conventional = true
	case "free":
	default:
		return nil, fmt.Errorf("format: %q is not one of conventional, free", y.Format)
	}
	if err := r.parseRefs(y.Refs); err != nil {
		return nil, err
	}
	if len(y.Except.Users) > maxExceptUsers {
		return nil, fmt.Errorf("except.users: %d users, the limit is %d", len(y.Except.Users), maxExceptUsers)
	}
	for _, u := range y.Except.Users {
		if !validUsername(u) {
			return nil, fmt.Errorf("except.users: %q is not a GitLab username", u)
		}
	}
	// Rules of the preset come first; rules of the file override them by name.
	merged := map[string]map[string]any{}
	for name, spec := range preset {
		if r.conventional || !catalogue[name].conventional {
			merged[name] = spec
		}
	}
	for name, spec := range y.Rules {
		merged[name] = spec
	}
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, known := catalogue[name]; !known {
			r.unknown = append(r.unknown, name)
			continue
		}
		ru, err := parseRule(name, merged[name], r.conventional)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", name, err)
		}
		if ru.level != levelOff {
			r.rules = append(r.rules, ru)
		}
	}
	return r, nil
}

func (rs *ruleset) parseRefs(v any) error {
	switch refs := v.(type) {
	case nil:
		rs.refs = refsAll
	case string:
		switch refs {
		case "all":
			rs.refs = refsAll
		case "default":
			rs.refs = refsDefault
		case "protected":
			rs.refs = refsProtected
		default:
			return fmt.Errorf("refs: %q is not one of all, default, protected or a list of refs", refs)
		}
	case []any:
		if len(refs) == 0 {
			return errors.New("refs: the list is empty")
		}
		rs.refs = refsList
		for _, item := range refs {
			s, ok := item.(string)
			if !ok || !strings.HasPrefix(s, "refs/") {
				return fmt.Errorf("refs: %v is not a ref pattern such as refs/heads/release/*", item)
			}
			if _, err := path.Match(s, ""); err != nil {
				return fmt.Errorf("refs: %q: %w", s, err)
			}
			rs.list = append(rs.list, s)
		}
	default:
		return errors.New("refs: expected all, default, protected or a list of refs")
	}
	return nil
}

func (rs *ruleset) matchList(ref string) bool {
	for _, p := range rs.list {
		// parseRefs checks the patterns, so Match cannot fail here.
		if ok, err := path.Match(p, ref); err == nil && ok {
			return true
		}
	}
	return false
}

func parseRule(name string, spec map[string]any, conventional bool) (*rule, error) {
	def := catalogue[name]
	if def.conventional && !conventional {
		return nil, errors.New("needs format: conventional")
	}
	r := &rule{name: name}
	allowed := map[string]bool{"level": true, "types": true}
	switch {
	case def.value == "reference":
		allowed["location"], allowed["pattern"], allowed["keys"] = true, true, true
	case def.value == "regexp2":
		allowed["value"], allowed["examples"] = true, true
	case def.value != "none":
		allowed["value"] = true
	}
	for key := range spec {
		if !allowed[key] {
			return nil, fmt.Errorf("unknown key %q", key)
		}
	}
	lv, ok := spec["level"].(string)
	switch {
	case !ok:
		return nil, errors.New("level: required, one of error, warn, off")
	case lv == "error":
		r.level = levelError
	case lv == "warn":
		r.level = levelWarn
	case lv == "off":
		r.level = levelOff
	default:
		return nil, fmt.Errorf("level: %q is not one of error, warn, off", lv)
	}
	if t, ok := spec["types"]; ok {
		if !conventional {
			return nil, errors.New("types: needs format: conventional")
		}
		list, err := stringList(t)
		if err != nil {
			return nil, fmt.Errorf("types: %w", err)
		}
		r.types = list
	}
	value, hasValue := spec["value"]
	if allowed["value"] && !hasValue {
		return nil, errors.New("value: required")
	}
	switch kind := def.value; {
	case kind == "list":
		list, err := stringList(value)
		if err != nil {
			return nil, fmt.Errorf("value: %w", err)
		}
		r.list = list
	case kind == "int":
		n, ok := value.(int)
		if !ok || n < 1 || n > 10000 {
			return nil, errors.New("value: a number from 1 to 10000 is expected")
		}
		r.number = n
	case strings.HasPrefix(kind, "enum:"):
		options := strings.Split(strings.TrimPrefix(kind, "enum:"), "|")
		s, ok := value.(string)
		if !ok || !slices.Contains(options, s) {
			return nil, fmt.Errorf("value: expected one of %s", strings.Join(options, ", "))
		}
		r.enum = s
	case kind == "regexp2":
		return r, r.parsePattern(value, spec["examples"])
	case kind == "reference":
		return r, r.parseReference(spec)
	}
	return r, nil
}

func (r *rule) parsePattern(value, examples any) error {
	src, ok := value.(string)
	if !ok || src == "" {
		return errors.New("value: a regular expression is expected")
	}
	re, err := regexp2.Compile(src, regexp2.RE2)
	if err != nil {
		return fmt.Errorf("value: %w", err)
	}
	r.pattern, r.source = re, src
	if examples != nil {
		if err := strictyaml.DecodeValue(examples, &r.examples, module.DefaultMaxPolicySize); err != nil {
			return fmt.Errorf("examples: %w", err)
		}
	}
	return nil
}

func (r *rule) parseReference(spec map[string]any) error {
	loc, ok := spec["location"].(string)
	if !ok || (loc != "trailer" && loc != "body" && loc != "any") {
		return errors.New("location: required, one of trailer, body, any")
	}
	r.location = loc
	src, ok := spec["pattern"].(string)
	if !ok || src == "" {
		return errors.New("pattern: a regular expression (RE2) is required")
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return fmt.Errorf("pattern: %w", err)
	}
	r.re = re
	if keys, ok := spec["keys"]; ok {
		if loc != "trailer" {
			return errors.New("keys: only for location: trailer")
		}
		list, err := stringList(keys)
		if err != nil {
			return fmt.Errorf("keys: %w", err)
		}
		r.keys = list
	}
	return nil
}

func stringList(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return nil, errors.New("a non-empty list of strings is expected")
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok || s == "" {
			return nil, errors.New("a non-empty list of strings is expected")
		}
		out = append(out, s)
	}
	return out, nil
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,254}$`)

func validUsername(s string) bool { return usernamePattern.MatchString(s) }
