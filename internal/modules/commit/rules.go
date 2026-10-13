package commit

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// finding is one broken rule of one message.
type finding struct {
	level  level
	reason string
	// timeout marks a pattern that ran out of time: commit_check_timeout.
	timeout bool
}

// budget is the time left for pattern over the whole receive.
type budget struct {
	left  time.Duration
	clock func() time.Time
}

// defaultPatternTimeout bounds a match without a configured timeout, such as
// the examples of a policy (architecture §10.3, §17).
const defaultPatternTimeout = 100 * time.Millisecond

// errBudget is returned when the pattern budget of the receive is spent.
var errBudget = errors.New("the pattern time budget of the push is spent")

// check applies the ruleset to a parsed message.
func (rs *ruleset) check(m Message, b *budget, perMessage time.Duration) []finding {
	var out []finding
	for _, g := range m.Grammar {
		out = append(out, finding{level: levelError, reason: g})
	}
	typeKnown := !rs.conventional || m.Type != ""
	for _, r := range rs.rules {
		if catalogue[r.name].conventional && !typeKnown {
			continue
		}
		if len(r.types) > 0 && !slices.Contains(r.types, m.Type) {
			continue
		}
		if r.pattern != nil {
			if f, ok := r.checkPattern(m, b, perMessage); ok {
				out = append(out, f)
			}
			continue
		}
		if reason := r.checkStructural(m); reason != "" {
			out = append(out, finding{level: r.level, reason: reason})
		}
	}
	return out
}

func (r *rule) checkPattern(m Message, b *budget, perMessage time.Duration) (finding, bool) {
	limit := perMessage
	if b != nil {
		if b.left <= 0 {
			return finding{level: r.level, reason: "pattern: not checked, " + errBudget.Error(), timeout: true}, true
		}
		limit = min(limit, b.left)
	}
	var start time.Time
	if b != nil {
		start = b.clock()
	}
	ok, err := r.matchPattern(m.Text, limit)
	if b != nil {
		b.left -= b.clock().Sub(start)
	}
	if err != nil {
		return finding{level: r.level, reason: "pattern: not checked in time (" + err.Error() + ")", timeout: true}, true
	}
	if !ok {
		return finding{level: r.level, reason: "pattern: the message does not match the required format"}, true
	}
	return finding{}, false
}

// matchPattern reports whether the pattern matches at the start of text, like
// Python re.match; "$" in RE2 mode matches only at the end of the text, and
// Normalise has removed the trailing newline. A zero timeout means no limit.
func (r *rule) matchPattern(text string, timeout time.Duration) (bool, error) {
	// A policy serves one receive or one lint run at a time, so the timeout
	// is set on the compiled pattern before each match.
	r.pattern.MatchTimeout = timeout
	if timeout <= 0 {
		r.pattern.MatchTimeout = defaultPatternTimeout
	}
	m, err := r.pattern.FindStringMatch(text)
	if err != nil {
		return false, err
	}
	return m != nil && m.RuneIndex == 0, nil
}

// checkStructural returns the reason the rule is broken, or "".
func (r *rule) checkStructural(m Message) string {
	switch r.name {
	case "type-enum":
		if !slices.Contains(r.list, m.Type) {
			return fmt.Sprintf("type-enum: %q is not one of %s", m.Type, strings.Join(r.list, ", "))
		}
	case "scope":
		if r.enum == "required" && !m.HasScope {
			return "scope: a scope is required, as in type(scope): subject"
		}
		if r.enum == "forbidden" && m.HasScope {
			return fmt.Sprintf("scope: a scope is not allowed, found %q", m.Scope)
		}
	case "scope-enum":
		if !m.HasScope {
			return ""
		}
		for _, part := range splitScopes(m.Scope) {
			if !slices.Contains(r.list, part) {
				return fmt.Sprintf("scope-enum: %q is not one of %s", part, strings.Join(r.list, ", "))
			}
		}
	case "breaking-bang":
		if r.enum == "forbid" && m.Breaking {
			return `breaking-bang: "!" after the type is not allowed`
		}
	case "header-max-length":
		if n := utf8.RuneCountInString(m.Header); n > r.number {
			return fmt.Sprintf("header-max-length: the header is %d characters, the limit is %d", n, r.number)
		}
	case "subject-case":
		return checkCase(r.enum, m.Subject)
	case "subject-full-stop":
		dot := strings.HasSuffix(m.Subject, ".")
		if r.enum == "forbid" && dot {
			return "subject-full-stop: the subject must not end with a full stop"
		}
		if r.enum == "require" && !dot {
			return "subject-full-stop: the subject must end with a full stop"
		}
	case "body-required":
		if strings.TrimSpace(m.Body) == "" {
			return "body-required: the message needs a body"
		}
	case "body-max-line-length":
		return checkBodyLines(m.Body, r.number)
	case "reference":
		return r.checkReference(m)
	case "trailer-required":
		var missing []string
		for _, key := range r.list {
			if len(m.Trailer(key)) == 0 {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			return fmt.Sprintf("trailer-required: missing %s in the trailer block", strings.Join(missing, ", "))
		}
	}
	return ""
}

// splitScopes splits a scope such as "SPE, SPP" or "SUE/SUD" into its parts.
func splitScopes(scope string) []string {
	parts := strings.FieldsFunc(scope, func(r rune) bool { return r == ',' || r == '/' })
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func checkCase(mode, subject string) string {
	first, _ := utf8.DecodeRuneInString(subject)
	if !unicode.IsLetter(first) {
		return ""
	}
	switch {
	case mode == "lower-first" && unicode.IsUpper(first):
		return "subject-case: the subject must start with a lowercase letter"
	case mode == "sentence" && unicode.IsLower(first):
		return "subject-case: the subject must start with an uppercase letter"
	}
	return ""
}

// checkBodyLines skips fenced and indented code and lines with URLs.
func checkBodyLines(body string, limit int) string {
	fenced := false
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			continue
		}
		if fenced || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") || strings.Contains(line, "://") {
			continue
		}
		if n := utf8.RuneCountInString(line); n > limit {
			return fmt.Sprintf("body-max-line-length: body line %d is %d characters, the limit is %d", i+1, n, limit)
		}
	}
	return ""
}

func (r *rule) checkReference(m Message) string {
	switch r.location {
	case "trailer":
		for _, t := range m.Trailers {
			if (len(r.keys) == 0 || slices.ContainsFunc(r.keys, func(k string) bool { return strings.EqualFold(k, t.Key) })) &&
				r.re.MatchString(t.Value) {
				return ""
			}
		}
		where := "the trailer block"
		if len(r.keys) > 0 {
			where = "a trailer " + strings.Join(r.keys, ", ")
		}
		return fmt.Sprintf("reference: no reference matching %s in %s", r.re, where)
	case "body":
		if r.re.MatchString(m.Body) {
			return ""
		}
		return fmt.Sprintf("reference: no reference matching %s in the body", r.re)
	default:
		if r.re.MatchString(m.Text) {
			return ""
		}
		return fmt.Sprintf("reference: no reference matching %s in the message", r.re)
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(Normalise(s), "\n")
	return line
}
