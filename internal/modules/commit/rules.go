package commit

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// finding is one broken check of one message.
type finding struct {
	level  level
	reason string
}

// apply applies the checks of the rule to a parsed message. A message that
// the parser cannot read is always an error, whatever the checks.
func (r *rule) apply(m Message) []finding {
	var out []finding
	for _, g := range m.Grammar {
		out = append(out, finding{level: levelError, reason: g})
	}
	typeKnown := !r.conventional || m.Type != ""
	for _, c := range r.checks {
		if catalogue[c.name].conventional && !typeKnown {
			continue
		}
		if len(c.types) > 0 && !slices.Contains(c.types, m.Type) {
			continue
		}
		if reason := c.verify(m); reason != "" {
			out = append(out, finding{level: c.level, reason: reason})
		}
	}
	return out
}

// verify returns the reason the check is broken, or "".
func (r *check) verify(m Message) string {
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
	case "pattern":
		return r.checkPattern(m)
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

// checkPattern searches the regex where location says: a regex is found
// when it is found once, and an exclusion passes when it is found nowhere.
func (r *check) checkPattern(m Message) string {
	var texts []string
	where := "the " + r.location
	switch r.location {
	case "header":
		texts = []string{m.Header}
	case "body":
		texts = []string{m.Body}
	case "message":
		texts = []string{m.Text}
	case "trailer":
		where = "the trailer block"
		if len(r.keys) > 0 {
			where = "a trailer " + strings.Join(r.keys, ", ")
		}
		for _, t := range m.Trailers {
			if len(r.keys) == 0 || slices.ContainsFunc(r.keys, func(k string) bool { return strings.EqualFold(k, t.Key) }) {
				texts = append(texts, t.Value)
			}
		}
	}
	found := slices.ContainsFunc(texts, r.pattern.Found)
	switch {
	case r.pattern.Negated() && found:
		return fmt.Sprintf("pattern: %s must not be in %s", strings.TrimPrefix(r.pattern.String(), "!"), where)
	case !r.pattern.Negated() && !found:
		return fmt.Sprintf("pattern: no match of %s in %s", r.pattern, where)
	}
	return ""
}
