// Package spec describes the format of a policy file once: the module parser
// validates a file against the description and internal/schema renders it as
// JSON Schema for editors. A check that JSON Schema cannot express is code
// called by name; the schema names it in x-repopolicy-check.
package spec

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Type is the YAML type of a node.
type Type int

// Types of nodes.
const (
	// Any accepts every value; Check decides, OneOf documents it.
	Any Type = iota
	Object
	Array
	// Dict is a mapping with free keys, such as the keys of TAGPOLICY.
	Dict
	String
	Integer
	Boolean
)

// Spec is one node of a policy file.
type Spec struct {
	Type Type
	Desc string

	// Fields of an Object, validated in this order.
	Fields []Field
	// Open lets an Object have keys Fields does not name; the parser
	// handles them, for example COMMITPOLICY rules of a newer version.
	Open bool

	// Items of an Array, Values of a Dict.
	Items  *Spec
	Values *Spec
	// MinItems and MaxItems bound an Array or a Dict; zero is no bound.
	MinItems, MaxItems int
	// Noun is the plural name of the items in errors, for example "usernames".
	Noun string
	// Unique rejects repeated items of an Array.
	Unique bool
	// Named items of an Array are objects with a field "name", unique in
	// the array; errors are prefixed with Named and the name, "rule ci: ...".
	// For a Dict, Named prefixes errors in a value with Named and its key.
	Named string
	// KeyPattern and KeyFormat constrain the keys of a Dict; KeyNoun names a
	// key in errors, "label" when empty.
	KeyPattern, KeyFormat, KeyNoun string

	// Nullable lets a file set the node to null: a layer that removes the
	// key it would inherit from a preset. Merging removes every null.
	Nullable bool

	// MinLength and MaxLength bound a String in characters, or in bytes with Bytes.
	MinLength, MaxLength int
	Bytes                bool
	// Pattern is a regular expression a String must match; Format shows it in
	// errors. With Check, only the check validates and reports a mismatch in
	// its own words, and Pattern is for the schema.
	Pattern, Format string
	// Enum lists the values of a String.
	Enum []string

	// Const is the only value of an Integer when not zero, such as version: 1.
	Const int
	// Minimum and Maximum bound an Integer when Maximum is not zero.
	Minimum, Maximum int

	// OneOf documents the alternatives of Any in the schema.
	OneOf []*Spec
	// Check names a check in code that runs after the structure is valid.
	Check string
}

// Field is a key of an Object.
type Field struct {
	Name     string
	Required bool
	// AfterMerge makes a field required only in the result of merging the
	// presets and the file, where a file may override a preset rule partly.
	AfterMerge bool
	Spec       *Spec
}

// Checks are the checks in code by name. A check gets the value of its node
// and returns an error without a prefix; the validator adds the location.
type Checks map[string]func(v *strictyaml.Value) error

// Validate checks a file v against s and returns the first problem, such as
// "line 4: rule ci: paths: 1 to 50 patterns are required". Fields required
// only after merging may be missing, and nullable nodes may be null.
func Validate(s *Spec, v *strictyaml.Value, checks Checks) error {
	w := &walker{checks: checks, regexps: map[string]*regexp.Regexp{}}
	return w.node(s, v, location{})
}

// ValidateMerged checks the result of merging the presets and the file, or a
// preset, which must be complete: every field required after merging is set.
func ValidateMerged(s *Spec, v *strictyaml.Value, checks Checks) error {
	w := &walker{checks: checks, regexps: map[string]*regexp.Regexp{}, merged: true}
	return w.node(s, v, location{})
}

// location is where a problem is: "rule ci: " before the dotted field path.
type location struct {
	prefix string
	field  string
}

func (l location) child(name string) location {
	if l.field == "" {
		return location{prefix: l.prefix, field: name}
	}
	return location{prefix: l.prefix, field: l.field + "." + name}
}

func (l location) errorf(line int, format string, a ...any) error {
	msg := fmt.Sprintf(format, a...)
	if l.field != "" {
		msg = l.field + ": " + msg
	}
	return &strictyaml.Error{Line: line, Msg: l.prefix + msg}
}

type walker struct {
	checks  Checks
	regexps map[string]*regexp.Regexp
	merged  bool
}

// re compiles a pattern of the spec once per validation.
func (w *walker) re(pattern string) *regexp.Regexp {
	re, ok := w.regexps[pattern]
	if !ok {
		re = regexp.MustCompile(pattern)
		w.regexps[pattern] = re
	}
	return re
}

func (w *walker) node(s *Spec, v *strictyaml.Value, loc location) error {
	if v.Kind == strictyaml.Null && s.Nullable && !w.merged {
		return nil
	}
	if err := w.structure(s, v, loc); err != nil {
		return err
	}
	if s.Check == "" {
		return nil
	}
	check, ok := w.checks[s.Check]
	if !ok {
		check, ok = builtin[s.Check]
	}
	if !ok {
		return fmt.Errorf("spec: check %q is not implemented", s.Check)
	}
	if err := check(v); err != nil {
		return loc.errorf(v.Line, "%v", err)
	}
	return nil
}

func (w *walker) structure(s *Spec, v *strictyaml.Value, loc location) error {
	switch s.Type {
	case Any:
		return nil
	case Object:
		return w.object(s, v, loc)
	case Array:
		return w.array(s, v, loc)
	case Dict:
		return w.dict(s, v, loc)
	case String:
		return w.stringValue(s, v, loc)
	case Integer:
		return integer(s, v, loc)
	case Boolean:
		if v.Kind != strictyaml.Bool {
			return loc.errorf(v.Line, "expected true or false, got %s", v.Kind)
		}
		return nil
	}
	return fmt.Errorf("spec: unknown type %d", s.Type)
}

func (w *walker) object(s *Spec, v *strictyaml.Value, loc location) error {
	if v.Kind != strictyaml.Map {
		return loc.errorf(v.Line, "expected a mapping, got %s", v.Kind)
	}
	if !s.Open {
		for _, k := range v.Keys {
			if s.field(k) == nil {
				return loc.errorf(v.KeyLines[k], "unknown field %q%s", k, suggest(k, s.fieldNames()))
			}
		}
	}
	for _, f := range s.Fields {
		fv, ok := v.Fields[f.Name]
		switch {
		case !ok && w.required(f) && f.Spec.Type == Array:
			fv = &strictyaml.Value{Kind: strictyaml.Seq, Line: v.Line}
		case !ok && w.required(f) && f.Spec.Type == Dict:
			fv = &strictyaml.Value{Kind: strictyaml.Map, Line: v.Line}
		case !ok && w.required(f):
			return loc.child(f.Name).errorf(v.Line, "required")
		case !ok:
			continue
		}
		if err := w.node(f.Spec, fv, loc.child(f.Name)); err != nil {
			return err
		}
	}
	return nil
}

func (w *walker) required(f Field) bool { return f.Required || f.AfterMerge && w.merged }

func (s *Spec) field(name string) *Field {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			return &s.Fields[i]
		}
	}
	return nil
}

func (s *Spec) fieldNames() []string {
	names := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		names = append(names, f.Name)
	}
	return names
}

func (w *walker) array(s *Spec, v *strictyaml.Value, loc location) error {
	if v.Kind != strictyaml.Seq {
		return loc.errorf(v.Line, "expected a list, got %s", v.Kind)
	}
	if err := count(s, len(v.Items), v.Line, loc); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, item := range v.Items {
		itemLoc := loc
		if s.Named != "" {
			name := w.itemName(s.Items, item, i)
			itemLoc = location{prefix: loc.prefix + s.Named + " " + name + ": "}
		}
		if err := w.node(s.Items, item, itemLoc); err != nil {
			return err
		}
		key, ok := uniqueKey(s, item)
		if !ok {
			continue
		}
		if seen[key] {
			if s.Named != "" {
				return itemLoc.errorf(item.Line, "the name is used twice")
			}
			return loc.errorf(item.Line, "%q is listed twice", key)
		}
		seen[key] = true
	}
	return nil
}

// itemName is the name of a named item for errors, or #<n> when it has no valid one.
func (w *walker) itemName(items *Spec, item *strictyaml.Value, i int) string {
	if item.Kind == strictyaml.Map {
		if n, ok := item.Fields["name"]; ok && n.Kind == strictyaml.String {
			if f := items.field("name"); f != nil && w.stringValue(f.Spec, n, location{}) == nil {
				return n.Str
			}
		}
	}
	return fmt.Sprintf("#%d", i+1)
}

func uniqueKey(s *Spec, item *strictyaml.Value) (string, bool) {
	switch {
	case s.Named != "":
		return item.Fields["name"].Str, true
	case s.Unique:
		return item.Str, true
	}
	return "", false
}

func (w *walker) dict(s *Spec, v *strictyaml.Value, loc location) error {
	if v.Kind != strictyaml.Map {
		return loc.errorf(v.Line, "expected a mapping, got %s", v.Kind)
	}
	if err := count(s, len(v.Keys), v.Line, loc); err != nil {
		return err
	}
	keys := slices.Sorted(slices.Values(v.Keys))
	noun := s.KeyNoun
	if noun == "" {
		noun = "label"
	}
	for _, k := range keys {
		if s.KeyPattern != "" && !w.re(s.KeyPattern).MatchString(k) {
			return loc.errorf(v.KeyLines[k], "%s %q must match %s", noun, k, s.KeyFormat)
		}
	}
	for _, k := range keys {
		valueLoc := loc.child(k)
		if s.Named != "" {
			valueLoc = location{prefix: loc.prefix + s.Named + " " + k + ": "}
		}
		if err := w.node(s.Values, v.Fields[k], valueLoc); err != nil {
			return err
		}
	}
	return nil
}

// count checks the number of items: "1 to 5 usernames are required" with a
// lower bound, "101 rules, the limit is 100" without one.
func count(s *Spec, n, line int, loc location) error {
	switch {
	case s.MinItems > 0 && (n < s.MinItems || s.MaxItems > 0 && n > s.MaxItems):
		switch {
		case s.MaxItems == 0 && s.MinItems == 1:
			return loc.errorf(line, "a non-empty list is required")
		case s.MaxItems == 0:
			return loc.errorf(line, "at least %d %s are required", s.MinItems, s.Noun)
		}
		return loc.errorf(line, "%d to %d %s are required", s.MinItems, s.MaxItems, s.Noun)
	case s.MaxItems > 0 && n > s.MaxItems:
		return loc.errorf(line, "%d %s, the limit is %d", n, s.Noun, s.MaxItems)
	}
	return nil
}

func (w *walker) stringValue(s *Spec, v *strictyaml.Value, loc location) error {
	if v.Kind != strictyaml.String {
		return loc.errorf(v.Line, "expected a string, got %s", v.Kind)
	}
	n, unit := utf8.RuneCountInString(v.Str), "characters"
	if s.Bytes {
		n, unit = len(v.Str), "bytes"
	}
	switch {
	case s.MinLength > 0 && n < s.MinLength:
		return loc.errorf(v.Line, "required")
	case s.MaxLength > 0 && n > s.MaxLength:
		return loc.errorf(v.Line, "%d %s, the limit is %d", n, unit, s.MaxLength)
	case len(s.Enum) > 0 && !slices.Contains(s.Enum, v.Str):
		return loc.errorf(v.Line, "%q is not one of %s", v.Str, strings.Join(s.Enum, ", "))
	case s.Pattern != "" && s.Check == "" && !w.re(s.Pattern).MatchString(v.Str):
		return loc.errorf(v.Line, "%q must match %s", v.Str, s.Format)
	}
	return nil
}

func integer(s *Spec, v *strictyaml.Value, loc location) error {
	if v.Kind != strictyaml.Int {
		return loc.errorf(v.Line, "expected a number, got %s", v.Kind)
	}
	switch {
	case s.Const != 0 && v.Int != s.Const:
		return loc.errorf(v.Line, "%d is not supported, use %d", v.Int, s.Const)
	case s.Maximum != 0 && (v.Int < s.Minimum || v.Int > s.Maximum):
		return loc.errorf(v.Line, "a number from %d to %d is expected", s.Minimum, s.Maximum)
	}
	return nil
}

// suggest returns ` (did you mean "allow"?)` for a near miss among names.
func suggest(name string, names []string) string {
	best, bestDist := "", 3
	for _, n := range names {
		if d := distance(strings.ToLower(name), n); d < bestDist {
			best, bestDist = n, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

// distance is the Levenshtein distance of two short ASCII names.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
