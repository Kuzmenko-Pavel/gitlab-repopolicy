package commit

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// LintFinding is a broken check found by a local lint of a commit message.
type LintFinding struct {
	Rule string
	// Error is false for warnings.
	Error  bool
	Reason string
}

// Select returns the policy with only the named rules, for lint-commit
// --rule; an unknown name is an error.
func (p *Policy) Select(names []string) (*Policy, error) {
	selected := *p
	selected.rules = nil
	for _, name := range names {
		i := slices.IndexFunc(p.rules, func(r *rule) bool { return r.name == name })
		if i < 0 {
			known := make([]string, 0, len(p.rules))
			for _, r := range p.rules {
				known = append(known, r.name)
			}
			return nil, fmt.Errorf("rule %q is not in the file; it has %s", name, strings.Join(known, ", "))
		}
		if !slices.Contains(selected.rules, p.rules[i]) {
			selected.rules = append(selected.rules, p.rules[i])
		}
	}
	return &selected, nil
}

// Unusable says why the rules cannot be used, nil when they can: a preset of
// the file is missing or broken.
func (p *Policy) Unusable() error { return p.unusable }

// LintMessage checks one message against every rule, whatever their match:
// lint-commit has no ref. except.users does not apply: there is no pusher.
func (p *Policy) LintMessage(_ context.Context, message string) []LintFinding {
	return p.lint(commitInfo{message: message})
}

// LintCommit checks a raw commit object; merge commits and reverts follow except.
func (p *Policy) LintCommit(_ context.Context, object []byte) []LintFinding {
	return p.lint(parseCommit("", object))
}

func (p *Policy) lint(c commitInfo) []LintFinding {
	var out []LintFinding
	for _, r := range p.rules {
		if r.except.MergeCommit && c.parents > 1 {
			continue
		}
		m := ParseMessage(c.message, r.conventional)
		if r.except.GitReverts && isGitRevert(m) {
			continue
		}
		for _, f := range r.apply(m) {
			lv := f.level
			if p.warnMode && lv == levelError {
				lv = levelWarn
			}
			out = append(out, LintFinding{Rule: r.name, Error: lv == levelError, Reason: f.reason})
		}
	}
	return out
}
