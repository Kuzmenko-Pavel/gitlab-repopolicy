// Command release-notes holds the release logic of the Makefile that parses
// git data:
//
//	release-notes notes <version>           the changelog of the release tag, from Conventional Commits
//	release-notes previous-minor <version>  the last release of a lower minor version, or nothing
//
// It runs git in the current directory.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "release-notes:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: release-notes notes|previous-minor <version>")
	}
	version := args[1]
	switch args[0] {
	case "notes":
		prev, err := previousTag(ctx, version)
		if err != nil {
			return err
		}
		rng := "HEAD"
		if prev != "" {
			rng = prev + "..HEAD"
		}
		log, err := git(ctx, "log", "--reverse", "--no-merges", "--format=%h%x00%s%x00%B%x1e", rng)
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, notes(version, parseLog(log)))
		return err
	case "previous-minor":
		tags, err := git(ctx, "tag", "--list", "v*")
		if err != nil {
			return err
		}
		prev, err := previousMinor(version, strings.Fields(tags))
		if err != nil {
			return err
		}
		if prev != "" {
			_, err = fmt.Fprintln(out, prev)
		}
		return err
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: arguments are built by this program
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(data), nil
}

// previousTag is the tag the changelog starts after: the last tag for a
// release candidate, the last final release for a final release.
func previousTag(ctx context.Context, version string) (string, error) {
	args := []string{"describe", "--abbrev=0", "--match", "v*"}
	if !strings.Contains(version, "-rc.") {
		args = append(args, "--exclude", "v*-rc.*")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	data, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// No such tag yet: the changelog covers the whole history.
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("git describe: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

type commit struct {
	hash, subject, body string
}

func parseLog(log string) []commit {
	var out []commit
	for _, rec := range strings.Split(log, "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x00", 3)
		if len(f) != 3 {
			continue
		}
		out = append(out, commit{hash: f[0], subject: f[1], body: f[2]})
	}
	return out
}

var (
	bang     = regexp.MustCompile(`^[a-z]+(\([^)]*\))?!:`)
	breaking = regexp.MustCompile(`(?m)^BREAKING CHANGE:`)
	sections = []struct{ key, title string }{
		{"breaking", "Breaking changes"}, {"feat", "Features"}, {"fix", "Fixes"},
		{"perf", "Performance"}, {"deps", "Dependencies"}, {"other", "Other changes"},
	}
)

// notes renders the changelog: a breaking change is listed under
// "Breaking changes" and under its type.
func notes(version string, commits []commit) string {
	lines := map[string]string{}
	for _, c := range commits {
		line := fmt.Sprintf("- %s (%s)\n", c.subject, c.hash)
		if bang.MatchString(c.subject) || breaking.MatchString(c.body) {
			lines["breaking"] += line
		}
		lines[section(c.subject)] += line
	}
	var b strings.Builder
	b.WriteString("gitlab-repopolicy " + version + "\n")
	for _, s := range sections {
		if lines[s.key] != "" {
			fmt.Fprintf(&b, "\n%s\n\n%s", s.title, lines[s.key])
		}
	}
	return b.String()
}

func section(subject string) string {
	for _, t := range []string{"feat", "fix", "perf"} {
		if strings.HasPrefix(subject, t+":") || strings.HasPrefix(subject, t+"(") || strings.HasPrefix(subject, t+"!:") {
			return t
		}
	}
	if strings.Contains(subject, "(deps)") {
		return "deps"
	}
	return "other"
}

var final = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// previousMinor is the highest final release among tags whose minor version
// is lower than the one of version, without the v prefix.
func previousMinor(version string, tags []string) (string, error) {
	current := final.FindStringSubmatch("v" + strings.SplitN(version, "-", 2)[0])
	if current == nil {
		return "", fmt.Errorf("%s is not X.Y.Z or X.Y.Z-rc.N", version)
	}
	major, minor := atoi(current[1]), atoi(current[2])
	var best []int
	for _, t := range tags {
		m := final.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		v := []int{atoi(m[1]), atoi(m[2]), atoi(m[3])}
		if (v[0] < major || v[0] == major && v[1] < minor) && (best == nil || less(best, v)) {
			best = v
		}
	}
	if best == nil {
		return "", nil
	}
	return fmt.Sprintf("%d.%d.%d", best[0], best[1], best[2]), nil
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func less(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
