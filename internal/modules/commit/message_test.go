package commit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// parseMessage parses raw as a rule does, with the trailer mode and whole
// given directly instead of taken from the checks.
func parseMessage(raw, trailer string, whole bool) Message {
	m := parseHeader(raw)
	m.split(trailer, whole)
	return m
}

// layout builds a message from a layout such as "H _ B _ T": H is the
// header, B and T are lines named by their letter, _ an empty line.
func layout(s string) string {
	var lines []string
	for i, f := range strings.Fields(s) {
		switch f {
		case "H":
			lines = append(lines, "fix: round refunds")
		case "_":
			lines = append(lines, "")
		default:
			lines = append(lines, fmt.Sprintf("%s%d text", f, i))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// letters names the lines of a part by the letters of the layout.
func letters(text string) string {
	if text == "" {
		return ""
	}
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if l == "" {
			out = append(out, "_")
			continue
		}
		out = append(out, l[:1])
	}
	return strings.Join(out, " ")
}

// TestSplit: the header is the first line, the trailer is found by the empty
// lines and by trailer-required, the body is between.
func TestSplit(t *testing.T) {
	for _, tc := range []struct {
		layout, mode, body, trailer string
		whole, blankBefore          bool
	}{
		{"H _ B _ T", trailerRequired, "B", "T", false, true},
		{"H _ B _ B _ T", trailerRequired, "B _ B", "T", false, true},
		{"H _ B B _ T T", trailerRequired, "B B", "T T", false, true},
		{"H B B B T", trailerRequired, "B B B", "T", false, false},
		{"H _ B B B T", trailerRequired, "B B B", "T", false, false},
		{"H _ T", trailerRequired, "", "T", false, false},
		{"H T", trailerRequired, "", "T", false, false},
		{"H _ B", trailerRequired, "", "B", false, false},
		{"H", trailerRequired, "", "", false, false},
		{"H _ B _ T", trailerOptional, "B", "T", false, true},
		{"H B B B T", trailerOptional, "B B B T", "", false, false},
		{"H _ B B B T", trailerOptional, "B B B T", "", false, false},
		{"H _ T", trailerOptional, "T", "", false, false},
		{"H _ _ B _ _ T", trailerOptional, "B", "T", false, true},
		{"H _ B _ T", trailerForbidden, "B _ T", "", false, false},
		{"H _ B _ B _ T", trailerForbidden, "B _ B _ T", "", false, false},
		{"H _ T T", trailerRequired, "", "T T", true, false},
		{"H _ B B B T", trailerRequired, "", "B B B T", true, false},
		{"H B B B T", trailerRequired, "B B B", "T", true, false},
		{"H _ B _ T T", trailerRequired, "B", "T T", true, true},
	} {
		m := parseMessage(layout(tc.layout), tc.mode, tc.whole)
		assert.Equal(t, "fix: round refunds", m.Header)
		assert.Equal(t, tc.body, letters(m.Body), "%s %s: body", tc.layout, tc.mode)
		assert.Equal(t, tc.trailer, letters(m.Trailer), "%s %s: trailer", tc.layout, tc.mode)
		assert.Equal(t, tc.blankBefore, m.BlankBeforeTrailer, "%s %s: empty line before the trailer", tc.layout, tc.mode)
	}
}

func TestParseMessage(t *testing.T) {
	t.Run("crlf", func(t *testing.T) {
		m := parseMessage("feat: add x\r\n\r\nbody line\r\n\r\nSigned-off-by: A <a@b>\r\n", trailerOptional, false)
		assert.Empty(t, m.Grammar)
		assert.Equal(t, "add x", m.Subject)
		assert.Equal(t, "body line", m.Body)
		assert.Equal(t, "Signed-off-by: A <a@b>", m.Trailer)
		assert.True(t, m.BlankAfterHeader)
		assert.True(t, m.BlankBeforeTrailer)
	})
	t.Run("trailing_newline", func(t *testing.T) {
		m := parseMessage("fix: y\n\n\n", trailerOptional, false)
		assert.Equal(t, "fix: y", m.Text)
		assert.Empty(t, m.Grammar)
	})
	t.Run("unicode_header", func(t *testing.T) {
		m := parseMessage("feat(ядро): додати перевірку", trailerOptional, false)
		assert.Equal(t, "ядро", m.Scope)
		assert.Equal(t, "додати перевірку", m.Subject)
		assert.Empty(t, m.Grammar)
	})
	t.Run("nul", func(t *testing.T) {
		assert.Contains(t, parseMessage("feat: x\x00y", trailerOptional, false).Grammar, "message contains a NUL byte")
	})
	t.Run("free_header", func(t *testing.T) {
		m := parseMessage("PAY-123 Round refunds.", trailerOptional, false)
		assert.Empty(t, m.Grammar)
		assert.Empty(t, m.Type)
		assert.Equal(t, "PAY-123 Round refunds.", m.Subject, "a header that is not conventional is the subject")
		assert.Contains(t, m.HeaderError, `expected "type(scope): subject"`)
	})
	t.Run("breaking", func(t *testing.T) {
		assert.True(t, parseMessage("feat!: drop v1", trailerOptional, false).Breaking)
	})
	t.Run("header_error", func(t *testing.T) {
		cases := map[string]string{
			"feat(: add x":        "scope parenthesis is not closed",
			" feat:  ":            `expected "type(scope): subject"`,
			"feat:":               "the subject is empty",
			"feat:x":              `a space must follow ":"`,
			"feat:  two spaces":   `exactly one space must follow ":"`,
			"feat(): empty scope": "scope in parentheses is empty",
		}
		for in, want := range cases {
			m := parseMessage(in, trailerOptional, false)
			assert.Empty(t, m.Grammar, in)
			assert.Contains(t, m.HeaderError, want, in)
		}
		assert.Equal(t, []string{"header is empty"}, parseMessage("", trailerOptional, false).Grammar)
	})
	t.Run("blank_after_header", func(t *testing.T) {
		for raw, want := range map[string]bool{
			"feat: x": false, "feat: x\n": false, "feat: x\n\n": true, "feat: x\r\n\r\n": true,
			"feat: x\n\nbody": true, "feat: x\n  \nbody": true, "feat: x\nbody": false,
		} {
			assert.Equal(t, want, parseMessage(raw, trailerOptional, false).BlankAfterHeader, "%q", raw)
		}
	})
}

func FuzzParseMessage(f *testing.F) {
	for _, s := range []string{
		"feat(scope)!: subject\n\nbody\n\nSigned-off-by: A <a@b>\n",
		"Merge branch 'x' into 'main'",
		"Revert \"feat: x\"\n\nThis reverts commit 0123456.",
		"fix: x\r\n\r\nRefs: #1\r\n",
		"\x00",
	} {
		f.Add(s, trailerRequired)
	}
	f.Fuzz(func(t *testing.T, raw, mode string) {
		m := parseMessage(raw, mode, true)
		if strings.Contains(m.Text, "\r") {
			t.Fatalf("not normalised: %q", m.Text)
		}
		if len(m.Grammar) == 0 && m.HeaderError == "" && m.Type == "" {
			t.Fatalf("a header without a type and without an error: %q", raw)
		}
		if strings.Contains(m.Header, "\n") || strings.HasPrefix(m.Body, "\n") || strings.HasSuffix(m.Body, "\n") {
			t.Fatalf("parts are not split on lines: %q", raw)
		}
	})
}
