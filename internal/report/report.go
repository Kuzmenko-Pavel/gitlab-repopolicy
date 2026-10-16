// Package report renders what the pusher sees. A rejection goes to stderr as
// a full block for the terminal followed by a GL-HOOK-ERR line, which is all
// that the GitLab UI and API show; warnings and notices of a successful push
// go to stdout. Results of API calls are never printed: Violation.Reason is
// audit only.
package report

import (
	"fmt"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

const (
	// MaxOutput is the ceiling of the text printed to the user.
	MaxOutput = 8 << 10
	// MaxItems is the number of files or commits printed per block.
	MaxItems = 10
	prefix   = "gitlab-repopolicy"
	hookErr  = "GL-HOOK-ERR: "
)

var titles = map[string]string{
	module.CodeProtectedPath:        "you may not change these files",
	module.CodePolicyAdmin:          "only PATHPOLICY admins may change PATHPOLICY",
	module.CodeLockoutPrevented:     "the changed PATHPOLICY would have no working admin",
	module.CodeInvalidPolicy:        "the policy in the default branch is broken",
	module.CodeInvalidPolicyUpdate:  "the changed policy file is invalid",
	module.CodeAuthorizationBackend: "could not check permissions: the GitLab API did not answer",
	module.CodeUnknownPrincipal:     "only a person may change these files",
	module.CodeCommitFormat:         "commit messages do not follow the format",
	module.CodeCommitCheckTimeout:   "a rule could not check a commit message in time",
	module.CodeTagNotSigned:         "the tag must be signed",
	module.CodeTagObjectInvalid:     "the tag does not match its ref",
	module.CodeTagSignerNotAllowed:  "this key may not sign these tags",
	module.CodeTagSignatureInvalid:  "the signature cannot be accepted",
	module.CodeLimitExceeded:        "the check did not finish within the server limits",
	module.CodeInternalError:        "internal error: the push was not checked and is rejected",
}

// Title returns the user-facing title of a violation code.
func Title(code string) string {
	if t, ok := titles[code]; ok {
		return t
	}
	return "push rejected"
}

// serverSide are the codes where the pusher is not at fault: the output
// names who to ask.
var serverSide = map[string]bool{
	module.CodeAuthorizationBackend: true,
	module.CodeLimitExceeded:        true,
	module.CodeInternalError:        true,
	module.CodeInvalidPolicy:        true,
}

// CoreSection is the section name of violations that belong to no module.
const CoreSection = prefix

// Section is the outcome of one module or of the core, under the name of its file.
type Section struct {
	// Name is the policy file, for example "PATHPOLICY", or CoreSection.
	Name       string
	Violations []module.Violation
}

// Pusher is who the hook checked, from GL_ID, GL_USERNAME and GL_PROTOCOL.
type Pusher struct {
	Username string
	GLID     string
	Protocol string
}

func (p Pusher) String() string {
	kind, id := module.Principal{GLID: p.GLID}.Kind()
	switch {
	case kind == module.PrincipalKey:
		return fmt.Sprintf("%s (SSH key %d)", p.Username, id)
	case p.Protocol == "ssh":
		return p.Username + " (SSH)"
	case p.Protocol == "http":
		return p.Username + " (HTTPS)"
	case p.Protocol == "web":
		return p.Username + " (the GitLab UI or API)"
	}
	return p.Username
}

// Context is what the frame around the blocks needs.
type Context struct {
	Pusher Pusher
	// Files are the policy files on the anchor, Branch the default branch and
	// Anchor its commit before the push.
	Files  []string
	Branch string
	Anchor string
	// Contact and DocsURL come from help in the server config.
	Contact string
	DocsURL string
}

// Rejection renders the stderr of a rejected push.
func Rejection(sections []Section, c Context) string {
	blocks := merge(sections)
	var b strings.Builder
	fmt.Fprintf(&b, "%s: push rejected by the rules of this repository (%d %s)\n", prefix, len(blocks), plural(len(blocks), "problem"))
	footer := c.footer()
	summary := hookErr + prefix + ": " + summary(blocks) + "\n"
	room := MaxOutput - b.Len() - len(footer) - len(summary)
	b.WriteString(truncate(render(blocks, c), room))
	b.WriteString(footer)
	b.WriteString(summary)
	return b.String()
}

// Warnings renders the stdout of a successful push with warnings and notices.
func Warnings(sections []Section, notices []string, c Context) string {
	var b strings.Builder
	for _, n := range notices {
		for _, line := range strings.Split(strings.TrimRight(n, "\n"), "\n") {
			fmt.Fprintf(&b, "%s: %s\n", prefix, printable(line))
		}
	}
	if blocks := merge(sections); len(blocks) > 0 {
		fmt.Fprintf(&b, "%s: push accepted with %d %s\n", prefix, len(blocks), plural(len(blocks), "warning"))
		b.WriteString(render(blocks, c))
		b.WriteString(c.footer())
	}
	return truncate(b.String(), MaxOutput)
}

// footer says where the rules are and who was checked.
func (c Context) footer() string {
	var lines []string
	where := ""
	if len(c.Files) > 0 && c.Branch != "" {
		where = fmt.Sprintf("%s in %s", strings.Join(c.Files, ", "), c.Branch)
		if c.Anchor != "" {
			where += fmt.Sprintf(" (%.7s)", c.Anchor)
		}
	}
	switch {
	case where != "" && c.DocsURL != "":
		lines = append(lines, "rules: "+where+"; docs: "+c.DocsURL)
	case where != "":
		lines = append(lines, "rules: "+where)
	case c.DocsURL != "":
		lines = append(lines, "docs: "+c.DocsURL)
	}
	if c.Pusher.Username != "" {
		lines = append(lines, "checked as: "+c.Pusher.String())
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, l := range lines {
		b.WriteString(printable(l) + "\n")
	}
	return b.String()
}

// block is one problem: a violation with every ref it was found on.
type block struct {
	section string
	v       module.Violation
	refs    []string
}

// merge turns violations into blocks: the same violation on several refs of
// one push is one problem with a list of branches and tags.
func merge(sections []Section) []*block {
	var out []*block
	byKey := map[string]*block{}
	for _, s := range sections {
		for _, v := range s.Violations {
			key := s.Name + "\x00" + identity(v)
			if b, ok := byKey[key]; ok {
				if v.Ref != "" {
					b.refs = append(b.refs, v.Ref)
				}
				continue
			}
			b := &block{section: s.Name, v: v}
			if v.Ref != "" {
				b.refs = []string{v.Ref}
			}
			byKey[key] = b
			out = append(out, b)
		}
	}
	return out
}

// identity is what makes two violations the same problem: everything but the ref.
func identity(v module.Violation) string {
	var b strings.Builder
	for _, f := range []string{v.Code, v.Rule, v.RuleKind, v.Title, v.Message, v.Hint, v.Summary} {
		b.WriteString(f + "\x00")
	}
	for _, f := range v.Facts {
		b.WriteString(f + "\x01")
	}
	for _, it := range v.Items {
		b.WriteString(it.Name + "\x02" + strings.Join(it.Details, "\x03") + "\x01")
	}
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// refName is a ref as the pusher writes it: branch x, tag v1.0.0.
func refName(ref string) string {
	if name, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		return "branch " + name
	}
	if name, ok := strings.CutPrefix(ref, "refs/tags/"); ok {
		return "tag " + name
	}
	return ref
}

// head is the first line of a block without its title, for example
// `PATHPOLICY rule "ci-config", branch main`.
func (b *block) head() string {
	var parts []string
	name := ""
	if b.section != CoreSection {
		name = b.section
		if b.v.Rule != "" {
			kind := b.v.RuleKind
			if kind == "" {
				kind = "rule"
			}
			name += fmt.Sprintf(" %s %q", kind, b.v.Rule)
		}
		parts = append(parts, name)
	}
	for _, r := range b.refs {
		parts = append(parts, refName(r))
	}
	return strings.Join(parts, ", ")
}

func (b *block) title() string {
	if b.v.Title != "" {
		return b.v.Title
	}
	return Title(b.v.Code)
}

// render writes every block: the head and title, facts, items, the message
// of the policy, the hint and, for a problem the pusher cannot fix, who helps.
func render(blocks []*block, c Context) string {
	var b strings.Builder
	for _, bl := range blocks {
		b.WriteString("\n")
		if head := bl.head(); head != "" {
			fmt.Fprintf(&b, "%s: %s\n", printable(head), printable(bl.title()))
		} else {
			fmt.Fprintf(&b, "%s\n", printable(bl.title()))
		}
		lines(&b, bl.v.Facts...)
		writeItems(&b, bl.v.Items)
		lines(&b, bl.v.Message)
		lines(&b, bl.v.Hint)
		if serverSide[bl.v.Code] {
			contact := "your GitLab administrators"
			if c.Contact != "" {
				contact = c.Contact
			}
			lines(&b, "who can help: "+contact)
		}
	}
	return b.String()
}

// lines writes texts indented, one line per line of each text.
func lines(b *strings.Builder, texts ...string) {
	for _, t := range texts {
		for _, line := range strings.Split(strings.TrimSpace(t), "\n") {
			if line != "" {
				fmt.Fprintf(b, "  %s\n", printable(line))
			}
		}
	}
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

// summary is the GL-HOOK-ERR text: the first problem in full, because the UI
// shows nothing else, and the titles of the others.
func summary(blocks []*block) string {
	if len(blocks) == 0 {
		return "push rejected"
	}
	first := blocks[0]
	text := first.v.Summary
	if text == "" {
		text = first.title()
		if head := first.head(); head != "" {
			text += " (" + head + ")"
		}
	}
	var more []string
	for _, b := range blocks[1:] {
		name := b.title()
		if b.section != CoreSection {
			name = b.section + ": " + name
		}
		more = append(more, name)
	}
	const maxMore = 3
	if len(more) > maxMore {
		more = append(more[:maxMore], fmt.Sprintf("%d more", len(more)-maxMore))
	}
	if len(more) > 0 {
		text += fmt.Sprintf(" (+%d more: %s)", len(blocks)-1, strings.Join(more, "; "))
	}
	return printable(text)
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
