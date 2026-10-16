//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs are the Ukrainian and English documents of the repository; AGENTS.md
// and its link CLAUDE.md have no translation.
func docs(t *testing.T) []string {
	t.Helper()
	repo := filepath.Join("..", "..")
	var out []string
	for _, pattern := range []string{"*.md", filepath.Join("docs", "*.md")} {
		files, err := filepath.Glob(filepath.Join(repo, pattern))
		require.NoError(t, err)
		for _, f := range files {
			if base := filepath.Base(f); base != "AGENTS.md" && base != "CLAUDE.md" {
				out = append(out, f)
			}
		}
	}
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
	}
}

var (
	mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	fence  = regexp.MustCompile("(?ms)^```.*?^```")
)

// TestDocsLinks: relative links outside code blocks point to existing files.
func TestDocsLinks(t *testing.T) {
	for _, f := range docs(t) {
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		text := fence.ReplaceAllString(string(data), "")
		for _, m := range mdLink.FindAllStringSubmatch(text, -1) {
			link := m[1]
			if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "mailto:") || strings.HasPrefix(link, "#") {
				continue
			}
			target, _, _ := strings.Cut(link, "#")
			assert.FileExists(t, filepath.Join(filepath.Dir(f), target), "%s links to a missing file: %s", f, link)
		}
	}
}
