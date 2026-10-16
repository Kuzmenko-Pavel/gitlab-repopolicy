package commit

import (
	"context"
)

// LintFinding is a broken rule found by a local lint of a commit message.
type LintFinding struct {
	Ruleset string
	// Error is false for warnings.
	Error  bool
	Reason string
}

// LintMessage checks one message against every ruleset, whatever their refs:
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
	for _, rs := range p.rulesets {
		if rs.except.MergeCommits && c.parents > 1 {
			continue
		}
		m := ParseMessage(c.message, rs.conventional)
		if rs.except.GitReverts && isGitRevert(m) {
			continue
		}
		for _, f := range rs.check(m, nil, p.settings.PatternTimeout) {
			lv := f.level
			if p.warnMode && lv == levelError {
				lv = levelWarn
			}
			out = append(out, LintFinding{Ruleset: rs.name, Error: lv == levelError, Reason: f.reason})
		}
	}
	return out
}
