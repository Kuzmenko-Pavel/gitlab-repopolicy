package commit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMessage(t *testing.T) {
	t.Run("crlf", func(t *testing.T) {
		m := ParseMessage("feat: add x\r\n\r\nbody line\r\n\r\nSigned-off-by: A <a@b>\r\n", true)
		assert.Empty(t, m.Grammar)
		assert.Equal(t, "add x", m.Subject)
		assert.Equal(t, "body line", m.Body)
		assert.Equal(t, []string{"A <a@b>"}, m.Trailer("signed-off-by"))
	})
	t.Run("trailing_newline", func(t *testing.T) {
		m := ParseMessage("fix: y\n\n\n", true)
		assert.Equal(t, "fix: y", m.Text)
		assert.Empty(t, m.Grammar)
	})
	t.Run("unicode_header", func(t *testing.T) {
		m := ParseMessage("feat(ядро): додати перевірку", true)
		assert.Equal(t, "ядро", m.Scope)
		assert.Equal(t, "додати перевірку", m.Subject)
		assert.Empty(t, m.Grammar)
	})
	t.Run("nul", func(t *testing.T) {
		m := ParseMessage("feat: x\x00y", true)
		assert.Contains(t, m.Grammar, "message contains a NUL byte")
	})
	t.Run("trailer_order", func(t *testing.T) {
		m := ParseMessage("feat: x\n\nbody\n\nRefs: group/project#1\nSigned-off-by: A <a@b>", true)
		assert.Len(t, m.Trailers, 2)
		assert.Equal(t, "body", m.Body)
		m = ParseMessage("feat: x\n\nSigned-off-by: A <a@b>\nRefs: #2\n  continued", true)
		assert.Equal(t, "#2 continued", m.Trailer("refs")[0])
	})
	t.Run("breaking_change", func(t *testing.T) {
		m := ParseMessage("feat!: drop v1\n\nBREAKING CHANGE: v1 is gone\nSigned-off-by: A <a@b>", true)
		assert.True(t, m.Breaking)
		assert.Equal(t, []string{"v1 is gone"}, m.Trailer("BREAKING CHANGE"))
	})
	t.Run("reference_locations", func(t *testing.T) {
		m := ParseMessage("fix: x\n\nFixes the race (group/my.project#665) seen in prod.\n\nIssue: group/project#7", true)
		assert.Contains(t, m.Body, "#665")
		assert.Equal(t, []string{"group/project#7"}, m.Trailer("Issue"))
	})
	t.Run("long_body", func(t *testing.T) {
		body := strings.Repeat("A paragraph of text.\n- item\n- item\n\n", 50) + "    code()\n"
		m := ParseMessage("docs: explain\n\n"+body, true)
		assert.Empty(t, m.Grammar)
		assert.Empty(t, m.Trailers, "a paragraph that is not all trailers is body")
		assert.Contains(t, m.Body, "code()")
	})
	t.Run("grammar", func(t *testing.T) {
		cases := map[string]string{
			"feat(: add x":           "scope parenthesis is not closed",
			" feat:  ":               "header",
			"feat:":                  "the subject is empty",
			"feat:x":                 `a space must follow ":"`,
			"feat:  two spaces":      `exactly one space must follow ":"`,
			"feat(): empty scope":    "scope in parentheses is empty",
			"feat: x\nbody directly": "separated from the header by an empty line",
			"":                       "header is empty",
		}
		for in, want := range cases {
			m := ParseMessage(in, true)
			assert.NotEmpty(t, m.Grammar, in)
			assert.Contains(t, strings.Join(m.Grammar, "; "), want, in)
		}
	})
	t.Run("free", func(t *testing.T) {
		m := ParseMessage("Anything goes here\n\nbody", false)
		assert.Empty(t, m.Grammar)
		assert.Equal(t, "Anything goes here", m.Subject)
		assert.NotEmpty(t, ParseMessage("x\nno blank line", false).Grammar)
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
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, raw string, conventional bool) {
		m := ParseMessage(raw, conventional)
		if strings.Contains(m.Text, "\r") {
			t.Fatalf("not normalised: %q", m.Text)
		}
		if conventional && len(m.Grammar) == 0 && m.Type == "" {
			t.Fatalf("conventional header without a type accepted: %q", raw)
		}
	})
}
