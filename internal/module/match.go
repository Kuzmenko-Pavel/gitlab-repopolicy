package module

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Match is the match field of a rule: all, default, or a list of exact names
// and regular expressions with optional exclusions. Names are branch or tag
// names without refs/heads/ or refs/tags/.
type Match struct {
	All, Default bool
	include      []element
	exclude      []element
	// list is the list as written, for messages and comparison.
	list []string
}

type element struct {
	name string
	re   *regexp.Regexp
}

func (e element) matches(name string) bool {
	if e.re != nil {
		return e.re.MatchString(name)
	}
	return e.name == name
}

// ParseMatch decodes a match field. withDefault allows the value default:
// only modules that know the default branch take it.
func ParseMatch(v any, withDefault bool) (Match, error) {
	switch m := v.(type) {
	case nil:
		return Match{}, errors.New("match is required; write match: all for every name")
	case string:
		switch {
		case m == "all":
			return Match{All: true}, nil
		case m == "default" && withDefault:
			return Match{Default: true}, nil
		case m == "default":
			return Match{}, errors.New("default is not allowed here; use all or a list of names")
		}
		if withDefault {
			return Match{}, fmt.Errorf("%q is not all, default or a list; write a single name as [%s]", m, m)
		}
		return Match{}, fmt.Errorf("%q is not all or a list; write a single name as [%s]", m, m)
	case []any:
		if len(m) == 0 {
			return Match{}, errors.New("the list is empty")
		}
		var out Match
		for _, item := range m {
			s, ok := item.(string)
			if !ok {
				return Match{}, fmt.Errorf("%v is not a string", item)
			}
			if s == "all" || s == "default" {
				return Match{}, fmt.Errorf("%s is a value of match, not an element of a list; write match: %s", s, s)
			}
			body, negate := strings.CutPrefix(s, "!")
			e, err := parseElement(body, s)
			if err != nil {
				return Match{}, err
			}
			if negate {
				out.exclude = append(out.exclude, e)
			} else {
				out.include = append(out.include, e)
			}
			out.list = append(out.list, s)
		}
		return out, nil
	}
	return Match{}, errors.New("expected all, a list of names, or default where it is allowed")
}

func parseElement(body, src string) (element, error) {
	if body == "" {
		return element{}, fmt.Errorf("%q: ! must be followed by a name or a regex", src)
	}
	if strings.HasPrefix(body, "/") {
		re, err := compileSlashed(body)
		if err != nil {
			return element{}, fmt.Errorf("%q: %w", src, err)
		}
		return element{re: re}, nil
	}
	if err := checkRefName(body); err != nil {
		return element{}, fmt.Errorf("%q: %w", src, err)
	}
	return element{name: body}, nil
}

// compileSlashed compiles /re/ or /re/i: RE2, found anywhere in the name like
// =~ in GitLab CI, so anchors are written explicitly.
func compileSlashed(s string) (*regexp.Regexp, error) {
	end := strings.LastIndex(s, "/")
	if end == 0 {
		return nil, errors.New("a regex is written as /re/ or /re/i")
	}
	src, flags := s[1:end], s[end+1:]
	switch flags {
	case "":
	case "i":
		src = "(?i)" + src
	default:
		return nil, fmt.Errorf("flag %q is not supported; the only flag is i", flags)
	}
	if s[1:end] == "" {
		return nil, errors.New("the regex is empty")
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("not a valid RE2 regex: %w", err)
	}
	return re, nil
}

// checkRefName rejects exact names that git does not allow as a branch or tag
// name, with a hint for the common mix-ups with globs and regexes.
func checkRefName(name string) error {
	switch {
	case strings.HasPrefix(name, "refs/"):
		return errors.New("write the branch or tag name without refs/heads/ or refs/tags/")
	case strings.Contains(name, "*"):
		prefix, _, _ := strings.Cut(name, "*")
		return fmt.Errorf("a glob is not supported; use a regex such as /^%s/", strings.ReplaceAll(regexp.QuoteMeta(prefix), "/", `\/`))
	case strings.ContainsAny(name, `\^$+(){}|`):
		return fmt.Errorf("not a branch or tag name; did you mean the regex /%s/?", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune("?[~:", r) {
			return fmt.Errorf("%q is not allowed in a branch or tag name", r)
		}
	}
	switch {
	case name == "@", strings.Contains(name, ".."), strings.Contains(name, "@{"), strings.Contains(name, "//"),
		strings.HasPrefix(name, "/"), strings.HasSuffix(name, "/"), strings.HasSuffix(name, "."),
		strings.HasSuffix(name, ".lock"), strings.HasPrefix(name, "-"):
		return errors.New("not a valid branch or tag name")
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return errors.New("not a valid branch or tag name")
		}
	}
	return nil
}

// IsList reports whether the field is a list rather than all or default.
func (m Match) IsList() bool { return m.list != nil }

// Matches reports whether a name matches a list: at least one element that is
// not an exclusion matches, or there are only exclusions, and no exclusion
// matches. It is false for all and default, which the module resolves itself.
func (m Match) Matches(name string) bool {
	if !m.IsList() {
		return false
	}
	if len(m.include) > 0 && !slices.ContainsFunc(m.include, func(e element) bool { return e.matches(name) }) {
		return false
	}
	return !slices.ContainsFunc(m.exclude, func(e element) bool { return e.matches(name) })
}

// String is the field as written in the policy.
func (m Match) String() string {
	switch {
	case m.All:
		return "all"
	case m.Default:
		return "default"
	}
	return "[" + strings.Join(m.list, ", ") + "]"
}

// Same reports whether two fields have the same value, comparing lists as sets.
func (m Match) Same(o Match) bool {
	if m.All != o.All || m.Default != o.Default || m.IsList() != o.IsList() {
		return false
	}
	a, b := slices.Clone(m.list), slices.Clone(o.list)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

// Pattern is a regex element of a match list used on text: /re/, /re/i,
// !/re/ or !/re/i, where ! means the regex must not be found.
type Pattern struct {
	re     *regexp.Regexp
	negate bool
	src    string
}

// ParsePattern decodes a regex element; exact names are not allowed.
func ParsePattern(s string) (Pattern, error) {
	body, negate := strings.CutPrefix(s, "!")
	if !strings.HasPrefix(body, "/") {
		return Pattern{}, fmt.Errorf("%q: write a regex as /re/, /re/i, !/re/ or !/re/i", s)
	}
	re, err := compileSlashed(body)
	if err != nil {
		return Pattern{}, fmt.Errorf("%q: %w", s, err)
	}
	return Pattern{re: re, negate: negate, src: s}, nil
}

// Found reports whether the regex is found in text.
func (p Pattern) Found(text string) bool { return p.re.MatchString(text) }

// Negated reports whether the regex must not be found.
func (p Pattern) Negated() bool { return p.negate }

// String is the pattern as written in the policy.
func (p Pattern) String() string { return p.src }
