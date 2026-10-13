package commit

import (
	"context"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// LintMessage checks one message against every ruleset, whatever their refs
// (lint-commit has no ref; owner's decision, question 11). except.users does
// not apply: there is no pusher.
func (p *policy) LintMessage(_ context.Context, message string) []module.LintFinding {
	return p.lint(commitInfo{message: message})
}

// LintCommit checks a raw commit object; merge commits and reverts follow except.
func (p *policy) LintCommit(_ context.Context, object []byte) []module.LintFinding {
	return p.lint(parseCommit("", object))
}

func (p *policy) lint(c commitInfo) []module.LintFinding {
	var out []module.LintFinding
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
			out = append(out, module.LintFinding{Ruleset: rs.name, Error: lv == levelError, Reason: f.reason})
		}
	}
	return out
}
