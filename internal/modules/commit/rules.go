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
// the parser cannot read at all is always an error, whatever the checks; a
// conventional header that is not parsed skips the checks of its parts.
func (r *rule) apply(m Message) []finding {
	var out []finding
	for _, g := range m.Grammar {
		out = append(out, finding{level: levelError, reason: g})
	}
	headerParsed := !r.conventional || m.Type != ""
	for _, c := range r.checks {
		if c.def.headerPart && !headerParsed {
			continue
		}
		if len(c.types) > 0 && !slices.Contains(c.types, m.Type) {
			continue
		}
		if reason := c.verify(m); reason != "" {
			out = append(out, finding{level: c.level, reason: c.label() + ": " + reason})
		}
	}
	return out
}

// verify returns the reason the check is broken, or "".
func (c *check) verify(m Message) string {
	switch c.def.name {
	case "header-format":
		return m.HeaderError
	case "header-max-length":
		if n := utf8.RuneCountInString(m.Header); n > c.number {
			return fmt.Sprintf("the header is %d characters, the limit is %d", n, c.number)
		}
	case "type-enum":
		if !slices.Contains(c.list, m.Type) {
			return fmt.Sprintf("%q is not one of %s", m.Type, strings.Join(c.list, ", "))
		}
	case "scope":
		if m.HasScope {
			return c.presence(true, "", fmt.Sprintf("a scope is not allowed, found %q", m.Scope))
		}
		return c.presence(false, "a scope is required, as in type(scope): subject", "")
	case "scope-enum":
		if !m.HasScope {
			return ""
		}
		for _, part := range splitScopes(m.Scope) {
			if !slices.Contains(c.list, part) {
				return fmt.Sprintf("%q is not one of %s", part, strings.Join(c.list, ", "))
			}
		}
	case "breaking-bang":
		return c.presence(m.Breaking, `"!" after the type is required`, `"!" after the type is not allowed`)
	case "subject-starts-lowercase":
		first, _ := utf8.DecodeRuneInString(m.Subject)
		return c.presence(unicode.IsLower(first),
			"the subject must start with a lowercase letter", "the subject must not start with a lowercase letter")
	case "subject-trailing-dot":
		return c.presence(strings.HasSuffix(m.Subject, "."),
			"the subject must end with a full stop", "the subject must not end with a full stop")
	case "body-leading-blank-lines":
		return c.presence(m.BlankAfterHeader,
			"an empty line must follow the header", "the line after the header must not be empty")
	case "body-min-line-length":
		n := utf8.RuneCountInString(strings.TrimSpace(m.Body))
		switch {
		case n >= c.number:
		case n == 0:
			return "the message needs a body"
		default:
			return fmt.Sprintf("the body is %d characters, at least %d are required", n, c.number)
		}
	case "body-max-line-length":
		return checkBodyLines(m.Body, c.number)
	case "trailer-required":
		var missing []string
		for _, key := range c.list {
			if len(m.Trailer(key)) == 0 {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			return fmt.Sprintf("missing %s in the trailer block", strings.Join(missing, ", "))
		}
	case "pattern":
		return c.checkPattern(m)
	}
	return ""
}

// presence checks a feature of the message against optional, required or
// forbidden and returns the reason the value is broken.
func (c *check) presence(present bool, whenMissing, whenPresent string) string {
	switch {
	case c.enum == "required" && !present:
		return whenMissing
	case c.enum == "forbidden" && present:
		return whenPresent
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
			return fmt.Sprintf("body line %d is %d characters, the limit is %d", i+1, n, limit)
		}
	}
	return ""
}

// checkPattern searches the regex where location says: a regex is found
// when it is found once, and an exclusion passes when it is found nowhere.
func (c *check) checkPattern(m Message) string {
	var texts []string
	where := "the " + c.location
	switch c.location {
	case "header":
		texts = []string{m.Header}
	case "body":
		texts = []string{m.Body}
	case "message":
		texts = []string{m.Text}
	case "trailer":
		where = "the trailer block"
		if len(c.keys) > 0 {
			where = "a trailer " + strings.Join(c.keys, ", ")
		}
		for _, t := range m.Trailers {
			if len(c.keys) == 0 || slices.ContainsFunc(c.keys, func(k string) bool { return strings.EqualFold(k, t.Key) }) {
				texts = append(texts, t.Value)
			}
		}
	}
	found := slices.ContainsFunc(texts, c.pattern.Found)
	switch {
	case c.pattern.Negated() && found:
		return fmt.Sprintf("%s must not be in %s", strings.TrimPrefix(c.pattern.String(), "!"), where)
	case !c.pattern.Negated() && !found:
		return fmt.Sprintf("no match of %s in %s", c.pattern, where)
	}
	return ""
}
