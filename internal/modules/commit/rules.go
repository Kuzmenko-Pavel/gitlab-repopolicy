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
// cannot be read at all is always an error, whatever the checks. A header
// that is not conventional skips the checks of its type, scope and "!", and
// the checks of its subject too when the rule requires the format.
func (r *rule) apply(m Message) []finding {
	var out []finding
	for _, g := range m.Grammar {
		out = append(out, finding{level: levelError, reason: g})
	}
	parsed := m.Type != ""
	strict := r.headerRequired()
	for _, c := range r.checks {
		switch {
		case c.def.part == partType && !parsed, c.def.part == partSubject && !parsed && strict:
			continue
		case !c.applies(m.Type):
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
		if c.enum == "required" {
			return m.HeaderError
		}
	case "header-max-length":
		return maxLength("header", m.Header, c.number)
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
	case "body-required":
		return c.presence(m.Body != "", "the message needs a body", "the message must not have a body")
	case "body-max-length":
		return maxLength("body", m.Body, c.number)
	case "trailer-leading-blank-lines":
		if m.Body == "" || m.Trailer == "" {
			return ""
		}
		return c.presence(m.BlankBeforeTrailer,
			"an empty line must separate the body and the trailer", "the line before the trailer must not be empty")
	case "trailer-required":
		return c.presence(m.Trailer != "", "the message needs a trailer, the last part of the message", "the message must not have a trailer")
	case "trailer-max-length":
		return maxLength("trailer", m.Trailer, c.number)
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

// maxLength measures a whole part of the message, line breaks included.
func maxLength(part, text string, limit int) string {
	if n := utf8.RuneCountInString(text); n > limit {
		return fmt.Sprintf("the %s is %d characters, the limit is %d", part, n, limit)
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
		texts = []string{m.Trailer}
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
