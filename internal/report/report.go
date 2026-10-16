// Package report renders what the pusher sees. A rejection
// goes to stderr as a full block for the terminal followed by GL-HOOK-ERR
// lines for the GitLab UI; warnings and notices of a successful push go to
// stdout. Results of API calls are never printed: Violation.Reason is audit only.
package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

const (
	// MaxOutput is the ceiling of the text printed to the user.
	MaxOutput = 8 << 10
	// MaxItems is the number of files or commits printed per rule.
	MaxItems = 10
	prefix   = "gitlab-repopolicy"
	hookErr  = "GL-HOOK-ERR: "
)

var titles = map[string]string{
	module.CodeProtectedPath:        "modification denied",
	module.CodePolicyAdmin:          "policy change denied",
	module.CodeLockoutPrevented:     "new policy has no effective administrators",
	module.CodeInvalidPolicy:        "policy is invalid",
	module.CodeInvalidPolicyUpdate:  "new policy version is invalid",
	module.CodeAuthorizationBackend: "unable to verify authorization",
	module.CodeUnknownPrincipal:     "principal type not allowed",
	module.CodeCommitFormat:         "commit message format",
	module.CodeCommitCheckTimeout:   "commit message check timed out",
	module.CodeTagNotSigned:         "tag is not signed",
	module.CodeTagObjectInvalid:     "tag object does not match ref",
	module.CodeTagSignerNotAllowed:  "tag signer not allowed",
	module.CodeTagSignatureInvalid:  "tag signature is invalid",
	module.CodeLimitExceeded:        "push could not be verified",
	module.CodeInternalError:        "push could not be verified",
}

// Title returns the user-facing title of a violation code.
func Title(code string) string {
	if t, ok := titles[code]; ok {
		return t
	}
	return "push rejected"
}

// CoreSection is the section name of violations that belong to no module.
const CoreSection = prefix

// Section is the outcome of one module or of the core, under the name of its file.
type Section struct {
	// Name is the policy file, for example "PATHPOLICY", or CoreSection.
	Name       string
	Violations []module.Violation
}

// Pusher identifies the pusher in the footer, for example "alice (id 314)".
type Pusher struct {
	Username string
	GLID     string
}

func (p Pusher) String() string {
	kind, id := module.Principal{GLID: p.GLID}.Kind()
	switch kind {
	case module.PrincipalUser:
		return fmt.Sprintf("%s (id %d)", p.Username, id)
	case module.PrincipalKey:
		return fmt.Sprintf("%s (key %d)", p.Username, id)
	default:
		return p.Username
	}
}

// Rejection renders the stderr of a rejected push.
func Rejection(sections []Section, who Pusher) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: push rejected (%s)\n", prefix, counts(sections))
	body := block(sections)
	footer := ""
	if who.Username != "" {
		footer = fmt.Sprintf("\nuser: %s\n", printable(who.String()))
	}
	summary := hookErr + prefix + ": " + summary(sections) + ". See push output.\n"
	room := MaxOutput - b.Len() - len(footer) - len(summary)
	b.WriteString(truncate(body, room))
	b.WriteString(footer)
	b.WriteString(summary)
	return b.String()
}

// Warnings renders the stdout of a successful push with warnings and notices.
func Warnings(sections []Section, notices []string) string {
	var b strings.Builder
	for _, n := range notices {
		for _, line := range strings.Split(strings.TrimRight(n, "\n"), "\n") {
			fmt.Fprintf(&b, "%s: %s\n", prefix, printable(line))
		}
	}
	if total(sections) > 0 {
		fmt.Fprintf(&b, "%s: warning (%s)\n", prefix, counts(sections))
		b.WriteString(block(sections))
	}
	return truncate(b.String(), MaxOutput)
}

func total(sections []Section) int {
	n := 0
	for _, s := range sections {
		n += len(s.Violations)
	}
	return n
}

// counts summarises the rejection, for example "1 path rule, 2 commits".
func counts(sections []Section) string {
	var parts []string
	for _, s := range sections {
		if len(s.Violations) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %d %s", s.Name, len(s.Violations), plural(len(s.Violations), "violation")))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// block renders every violation grouped by section and rule.
func block(sections []Section) string {
	var b strings.Builder
	for _, s := range sections {
		vs := append([]module.Violation(nil), s.Violations...)
		sort.SliceStable(vs, func(i, j int) bool { return vs[i].Rule < vs[j].Rule })
		for _, v := range vs {
			b.WriteString("\n")
			head := s.Name
			if v.Rule != "" {
				head += " [" + v.Rule + "]"
			}
			if v.Ref != "" {
				head += " " + v.Ref
			}
			fmt.Fprintf(&b, "%s: %s\n", printable(head), Title(v.Code))
			for _, line := range strings.Split(strings.TrimSpace(v.Message), "\n") {
				if line != "" {
					fmt.Fprintf(&b, "  %s\n", printable(line))
				}
			}
			writeItems(&b, v.Items)
			if v.Hint != "" {
				fmt.Fprintf(&b, "  %s\n", printable(v.Hint))
			}
		}
	}
	return b.String()
}

func writeItems(b *strings.Builder, items []module.Item) {
	if len(items) == 0 {
		return
	}
	label := "files:"
	if items[0].Commit {
		label = "commits:"
	}
	fmt.Fprintf(b, "  %s\n", label)
	for i, it := range items {
		if i == MaxItems {
			fmt.Fprintf(b, "    ... and %d more\n", len(items)-MaxItems)
			break
		}
		fmt.Fprintf(b, "    %s\n", printable(it.Name))
		for _, d := range it.Details {
			fmt.Fprintf(b, "      - %s\n", printable(d))
		}
	}
}

// printable escapes control characters so that text from a push or a policy
// file cannot forge lines such as GL-HOOK-ERR or drive the terminal of the pusher.
func printable(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			fmt.Fprintf(&b, "\\x%02x", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// summary is the one-line text for the UI, for example
// "PATHPOLICY [ci-config] modification denied; COMMITPOLICY commit message format".
func summary(sections []Section) string {
	var parts []string
	for _, s := range sections {
		seen := map[string]bool{}
		for _, v := range s.Violations {
			part := ""
			if s.Name != prefix {
				part = s.Name + " "
			}
			if v.Rule != "" {
				part += "[" + v.Rule + "] "
			}
			part += Title(v.Code)
			if !seen[part] {
				seen[part] = true
				parts = append(parts, part)
			}
		}
	}
	const maxParts = 3
	if len(parts) > maxParts {
		parts = append(parts[:maxParts], fmt.Sprintf("%d more", len(parts)-maxParts))
	}
	return printable(strings.Join(parts, "; "))
}

// truncate cuts text to at most max bytes at a line boundary and marks the cut.
func truncate(text string, max int) string {
	const mark = "... output truncated\n"
	if len(text) <= max {
		return text
	}
	if max <= len(mark) {
		return ""
	}
	cut := text[:max-len(mark)]
	if i := strings.LastIndexByte(cut, '\n'); i >= 0 {
		cut = cut[:i+1]
	}
	return cut + mark
}
