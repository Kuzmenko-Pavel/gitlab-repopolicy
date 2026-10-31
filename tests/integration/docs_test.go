//go:build integration

package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs are the Ukrainian and English documents of the repository; AGENTS.md
// and its link CLAUDE.md have no translation.
func docs(t *testing.T) []string {
	t.Helper()
	repo := filepath.Join("..", "..")
	var out []string
	files, err := filepath.Glob(filepath.Join(repo, "*.md"))
	require.NoError(t, err)
	for _, f := range files {
		if base := filepath.Base(f); base != "AGENTS.md" && base != "CLAUDE.md" {
			out = append(out, f)
		}
	}
	require.NoError(t, filepath.WalkDir(filepath.Join(repo, "docs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			out = append(out, path)
		}
		return nil
	}))
	require.NotEmpty(t, out)
	return out
}

// pairOf is the other language of a document: x.md and x.en.md.
func pairOf(f string) string {
	if base, ok := strings.CutSuffix(f, ".en.md"); ok {
		return base + ".md"
	}
	return strings.TrimSuffix(f, ".md") + ".en.md"
}

// TestDocsLanguagePairs: every document has its translation, and both start
// with the language switcher that links them.
func TestDocsLanguagePairs(t *testing.T) {
	for _, f := range docs(t) {
		pair := pairOf(f)
		if !assert.FileExists(t, pair, "%s has no language pair", f) {
			continue
		}
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		lines := strings.SplitN(string(data), "\n", 3)
		head := strings.Join(lines[:min(2, len(lines))], "\n")
		for _, link := range []string{"](" + filepath.Base(f) + ")", "](" + filepath.Base(pair) + ")"} {
			assert.Contains(t, head, link, "%s must start with the language switcher linking %s and %s", f, filepath.Base(f), filepath.Base(pair))
		}
		translation, err := os.ReadFile(pair)
		require.NoError(t, err)
		assert.Equal(t, documentStructure(string(data)), documentStructure(string(translation)), "%s and %s must retain corresponding headings and code blocks", f, pair)
	}
}

var (
	mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	fence  = regexp.MustCompile("(?ms)^```.*?^```")
)

// documentStructure compares language-neutral page structure, not wording.
func documentStructure(text string) []string {
	var out []string
	inside := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") {
			out = append(out, line)
			inside = !inside
			continue
		}
		if !inside && strings.HasPrefix(line, "#") {
			level, _, ok := strings.Cut(line, " ")
			if ok && strings.Trim(level, "#") == "" {
				out = append(out, level)
			}
		}
	}
	return out
}

// TestDocsLinks checks files and GitHub heading fragments, including AGENTS.
func TestDocsLinks(t *testing.T) {
	for _, f := range append(docs(t), filepath.Join("..", "..", "AGENTS.md")) {
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		text := fence.ReplaceAllString(string(data), "")
		for _, m := range mdLink.FindAllStringSubmatch(text, -1) {
			link := m[1]
			if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "mailto:") {
				continue
			}
			target, fragment, hasFragment := strings.Cut(link, "#")
			path := f
			if target != "" {
				path = filepath.Join(filepath.Dir(f), target)
			}
			info, err := os.Stat(path)
			if !assert.NoError(t, err, "%s links to a missing path: %s", f, link) {
				continue
			}
			if info.IsDir() || !hasFragment || fragment == "" || !strings.HasSuffix(path, ".md") {
				continue
			}
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, headingAnchors(string(data)), fragment, "%s links to a missing heading: %s", f, link)
		}
	}
}

var heading = regexp.MustCompile(`(?m)^#{1,6} (.+?) *#* *$`)

// headingAnchors follows GitHub's punctuation removal and duplicate suffixes.
func headingAnchors(text string) []string {
	matches := heading.FindAllStringSubmatch(fence.ReplaceAllString(text, ""), -1)
	out := make([]string, 0, len(matches))
	seen := map[string]int{}
	for _, m := range matches {
		var slug strings.Builder
		for _, r := range strings.ToLower(m[1]) {
			switch {
			case r == ' ':
				slug.WriteByte('-')
			case unicode.IsLetter(r), unicode.IsNumber(r), r == '-', r == '_':
				slug.WriteRune(r)
			}
		}
		base := slug.String()
		anchor := base
		if n := seen[base]; n > 0 {
			anchor += "-" + strconv.Itoa(n)
		}
		seen[base]++
		out = append(out, anchor)
	}
	return out
}

var tableSeparator = regexp.MustCompile(`^\|[ :|\-]+\|$`)

// TestDocsMarkdown catches incomplete fences and tables outside code blocks.
func TestDocsMarkdown(t *testing.T) {
	for _, doc := range docs(t) {
		data, err := os.ReadFile(doc)
		require.NoError(t, err)
		lines := strings.Split(string(data), "\n")
		inside := false
		for i := 0; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if strings.HasPrefix(line, "```") {
				inside = !inside
				continue
			}
			if inside || !strings.HasPrefix(line, "|") {
				continue
			}
			if assert.Less(t, i+1, len(lines), "%s:%d: incomplete table", doc, i+1) {
				assert.Regexp(t, tableSeparator, strings.TrimSpace(lines[i+1]), "%s:%d: table must start with header and separator", doc, i+1)
			}
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
				i++
			}
		}
		assert.False(t, inside, "%s: unclosed code fence", doc)
	}
}
