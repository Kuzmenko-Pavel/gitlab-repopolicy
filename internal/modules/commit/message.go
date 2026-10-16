package commit

import (
	"regexp"
	"strings"
)

// Message is a parsed commit message.
type Message struct {
	// Text is the normalised message: "\n" line ends, no trailing empty lines.
	Text   string
	Header string
	// Body is everything between the header and the trailer block.
	Body     string
	Trailers []Trailer

	// Conventional parts of the header.
	Type     string
	Scope    string
	HasScope bool
	Breaking bool
	Subject  string

	// Grammar lists violations of the format grammar; they are always checked.
	Grammar []string
}

// Trailer is one "Key: value" or "Key #value" line of the trailer block.
type Trailer struct {
	Key   string
	Value string
}

var (
	conventionalHeader = regexp.MustCompile(`^([A-Za-z0-9_-]+)(\(([^()\n]*)\))?(!)?: (\S.*)$`)
	trailerLine        = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9-]*|BREAKING CHANGE)(: | #)(.*)$`)
)

// Normalise turns "\r\n" and "\r" into "\n" and drops trailing empty lines:
// the GitLab UI writes "\r\n" and git always ends a message with "\n".
func Normalise(raw string) string {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// ParseMessage parses raw in the conventional or the free format.
func ParseMessage(raw string, conventional bool) Message {
	m := Message{Text: Normalise(raw)}
	if strings.ContainsRune(m.Text, 0) {
		m.Grammar = append(m.Grammar, "message contains a NUL byte")
	}
	lines := strings.Split(m.Text, "\n")
	m.Header = lines[0]
	rest := lines[1:]
	if len(rest) > 0 && strings.TrimSpace(rest[0]) != "" {
		m.Grammar = append(m.Grammar, "the body must be separated from the header by an empty line")
	}
	m.Body, m.Trailers = splitTrailers(rest)
	if strings.TrimSpace(m.Header) == "" {
		m.Grammar = append(m.Grammar, "header is empty")
		return m
	}
	if conventional {
		m.parseConventional()
	} else {
		m.Subject = m.Header
	}
	return m
}

func (m *Message) parseConventional() {
	g := conventionalHeader.FindStringSubmatch(m.Header)
	if g == nil {
		m.Grammar = append(m.Grammar, `header: expected "type(scope): subject"`+headerHint(m.Header))
		return
	}
	m.Type, m.HasScope, m.Scope, m.Breaking, m.Subject = g[1], g[2] != "", g[3], g[4] == "!", g[5]
	if m.HasScope && strings.TrimSpace(m.Scope) == "" {
		m.Grammar = append(m.Grammar, "header: the scope in parentheses is empty")
	}
}

// headerHint explains the most common grammar mistakes.
func headerHint(h string) string {
	switch {
	case strings.Count(h, "(") != strings.Count(h, ")"):
		return ", the scope parenthesis is not closed"
	case strings.HasSuffix(strings.TrimSpace(h), ":"):
		return ", the subject is empty"
	case strings.Contains(h, ":") && !strings.Contains(h, ": "):
		return `, a space must follow ":"`
	case strings.Contains(h, ":  "):
		return `, exactly one space must follow ":"`
	case strings.HasPrefix(h, " "):
		return ", the header starts with a space"
	}
	return ""
}

// splitTrailers separates the trailer block, the last paragraph whose lines
// are all trailers or their continuation lines, from the body.
func splitTrailers(rest []string) (string, []Trailer) {
	start := len(rest)
	for start > 0 && strings.TrimSpace(rest[start-1]) != "" {
		start--
	}
	paragraph := rest[start:]
	var trailers []Trailer
	if len(paragraph) > 0 && start > 0 {
		for _, line := range paragraph {
			if g := trailerLine.FindStringSubmatch(line); g != nil {
				trailers = append(trailers, Trailer{Key: g[1], Value: strings.TrimSpace(g[3])})
				continue
			}
			if len(trailers) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
				trailers[len(trailers)-1].Value += " " + strings.TrimSpace(line)
				continue
			}
			trailers = nil
			break
		}
	}
	bodyLines := rest
	if trailers != nil {
		bodyLines = rest[:start]
	}
	return strings.Trim(strings.Join(bodyLines, "\n"), "\n"), trailers
}

// Trailer returns the values of trailers with key, compared case-insensitively.
func (m Message) Trailer(key string) []string {
	var out []string
	for _, t := range m.Trailers {
		if strings.EqualFold(t.Key, key) {
			out = append(out, t.Value)
		}
	}
	return out
}
