//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	usageCommand = regexp.MustCompile(`(?m)^  ([a-z-]+) {2,}`)
	helpFlag     = regexp.MustCompile(`(?m)^  -([a-z-]+)`)
	cliSection   = regexp.MustCompile("(?m)^### `([a-z -]+)`\n")
	flagRow      = regexp.MustCompile("(?m)^\\| `-{1,2}([a-z-]+)")
)

// subcommands are the commands that take a subcommand first; the hook does
// not print -h, its flags are listed here.
var (
	subcommands = map[string][]string{"hook": {"pre-receive"}, "token": {"rotate"}, "breakglass": {"grant", "revoke", "list"}}
	hookFlags   = []string{"config"}
)

// TestCLIDocs: the CLI reference has a section for every command and
// subcommand of the binary, and the flag table of each section lists exactly
// the flags of its -h; a section of a command the binary does not know fails.
func TestCLIDocs(t *testing.T) {
	bin := productBinary(t)
	run := func(args ...string) string {
		out, _ := exec.CommandContext(t.Context(), bin, args...).CombinedOutput()
		return string(out)
	}
	var commands []string
	for _, m := range usageCommand.FindAllStringSubmatch(run(), -1) {
		for _, sub := range subcommandsOf(m[1]) {
			commands = append(commands, strings.TrimSpace(m[1]+" "+sub))
		}
	}
	require.Contains(t, commands, "validate")
	for _, doc := range []string{"user/reference/cli.md", "user/reference/cli.en.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
		require.NoError(t, err)
		sections := map[string]string{}
		text := string(data)
		matches := cliSection.FindAllStringSubmatchIndex(text, -1)
		for i, m := range matches {
			end := len(text)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			name := text[m[2]:m[3]]
			assert.Contains(t, commands, name, "%s: a section of %q, which the binary does not know", doc, name)
			sections[name] = text[m[1]:end]
		}
		for _, c := range commands {
			section, ok := sections[c]
			if !assert.True(t, ok, "%s has no section of %q", doc, c) {
				continue
			}
			want := hookFlags
			if c != "hook pre-receive" {
				want = nil
				for _, f := range helpFlag.FindAllStringSubmatch(run(append(strings.Fields(c), "-h")...), -1) {
					want = append(want, f[1])
				}
			}
			var got []string
			for _, f := range flagRow.FindAllStringSubmatch(section, -1) {
				got = append(got, f[1])
			}
			slices.Sort(want)
			slices.Sort(got)
			assert.Equal(t, want, got, "%s: the flags of %q", doc, c)
		}
	}
}

func subcommandsOf(command string) []string {
	if subs, ok := subcommands[command]; ok {
		return subs
	}
	return []string{""}
}
