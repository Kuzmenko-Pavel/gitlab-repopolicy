package commit

import (
	"regexp"
	"strings"
)

// Modes of the trailer of a message, the values of trailer-required.
const (
	trailerOptional  = "optional"
	trailerRequired  = "required"
	trailerForbidden = "forbidden"
)

// Message is a parsed commit message. The header is the first line; the
// trailer is the last part, found by the empty lines; the body is between.
type Message struct {
	// Text is the normalised message: "\n" line ends, no trailing empty lines.
	Text   string
	Header string
	// Body is the lines between the header and the trailer, without the
	// empty lines around them; "" when there is no body.
	Body string
	// Trailer is the last part of the message; "" when there is none.
	Trailer string
	// BlankAfterHeader reports an empty line right after the header in the
	// message as stored, before trailing empty lines are dropped: "header\n\n"
	// has one, "header\n" has none.
	BlankAfterHeader bool
	// BlankBeforeTrailer reports an empty line between the body and the
	// trailer; without a body or a trailer it is false.
	BlankBeforeTrailer bool

	// Conventional parts of the header; Type is "" when the header does not
	// follow the conventional format, and Subject is then the whole header.
	Type     string
	Scope    string
	HasScope bool
	Breaking bool
	Subject  string
	// HeaderError says why the header does not follow the conventional
	// format; it is "" when it does.
	HeaderError string

	// Grammar lists why the message cannot be parsed at all; it is always an error.
	Grammar []string
}

var conventionalHeader = regexp.MustCompile(`^([A-Za-z0-9_-]+)(\(([^()\n]*)\))?(!)?: (\S.*)$`)

// Normalise turns "\r\n" and "\r" into "\n" and drops trailing empty lines:
// the GitLab UI writes "\r\n" and git always ends a message with "\n".
func Normalise(raw string) string {
	lines := strings.Split(newlines(raw), "\n")
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func newlines(raw string) string {
	return strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
}

func blank(line string) bool { return strings.TrimSpace(line) == "" }

// parseHeader reads the header; the body and the trailer are left to split.
func parseHeader(raw string) Message {
	m := Message{Text: Normalise(raw), BlankAfterHeader: blankAfterHeader(raw)}
	if strings.ContainsRune(m.Text, 0) {
		m.Grammar = append(m.Grammar, "message contains a NUL byte")
	}
	m.Header, _, _ = strings.Cut(m.Text, "\n")
	if blank(m.Header) {
		m.Grammar = append(m.Grammar, "header is empty")
		return m
	}
	m.Subject = m.Header
	g := conventionalHeader.FindStringSubmatch(m.Header)
	if g == nil {
		m.HeaderError = `expected "type(scope): subject"` + headerHint(m.Header)
		return m
	}
	m.Type, m.HasScope, m.Scope, m.Breaking, m.Subject = g[1], g[2] != "", g[3], g[4] == "!", g[5]
	if m.HasScope && blank(m.Scope) {
		m.HeaderError = "the scope in parentheses is empty"
	}
	return m
}

// split divides the lines after the header into the body and the trailer.
// The empty lines right after the header separate it. The trailer is the
// paragraph after the last other empty line. Without such a line a required
// trailer is the whole paragraph after the empty lines of the header when
// whole is set and there are such lines, and the last line otherwise; a
// trailer that is not required is then absent. A forbidden trailer is not
// looked for.
func (m *Message) split(trailer string, whole bool) {
	lines := strings.Split(m.Text, "\n")[1:]
	separated := false
	for len(lines) > 0 && blank(lines[0]) {
		lines, separated = lines[1:], true
	}
	start := len(lines)
	if trailer != trailerForbidden {
		last := -1
		for i, line := range lines {
			if blank(line) {
				last = i
			}
		}
		switch {
		case last >= 0:
			start = last + 1
		case trailer == trailerRequired && whole && separated:
			start = 0
		case trailer == trailerRequired && len(lines) > 0:
			start = len(lines) - 1
		}
	}
	body := lines[:start]
	for len(body) > 0 && blank(body[len(body)-1]) {
		body = body[:len(body)-1]
	}
	m.Body = strings.Join(body, "\n")
	m.Trailer = strings.Join(lines[start:], "\n")
	m.BlankBeforeTrailer = m.Body != "" && m.Trailer != "" && blank(lines[start-1])
}

// headerHint explains the most common mistakes of a conventional header.
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

// blankAfterHeader reports whether raw has a second line and it is empty.
func blankAfterHeader(raw string) bool {
	_, rest, ok := strings.Cut(newlines(raw), "\n")
	if !ok || rest == "" {
		return false
	}
	line, _, _ := strings.Cut(rest, "\n")
	return blank(line)
}
