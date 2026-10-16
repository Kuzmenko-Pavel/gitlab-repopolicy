package commit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// maxCommitObject bounds a commit object read for its message.
const maxCommitObject = 1 << 20

const hint = "fix: git rebase -i and reword, or enable squash on merge"

// commitInfo is what COMMITPOLICY needs of a commit object.
type commitInfo struct {
	oid     string
	parents int
	message string
}

// parseCommit reads the parent count and the raw message of a commit object.
func parseCommit(oid string, data []byte) commitInfo {
	c := commitInfo{oid: oid}
	header, message, _ := bytes.Cut(data, []byte("\n\n"))
	for _, line := range bytes.Split(header, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("parent ")) {
			c.parents++
		}
	}
	c.message = string(message)
	return c
}

var revertBody = regexp.MustCompile(`(?m)^This reverts commit [0-9a-f]{7,64}\.?`)

// isGitRevert recognises the message of git revert and of the GitLab revert button.
func isGitRevert(m Message) bool {
	return strings.HasPrefix(m.Header, `Revert "`) && strings.HasSuffix(m.Header, `"`) && revertBody.MatchString(m.Text)
}

// evaluation is the state of one Evaluate call.
type evaluation struct {
	p       *Policy
	in      *module.Input
	budget  *budget
	commits map[string]commitInfo
	res     *module.Result
}

// Evaluate checks the commits entering every ref of the push.
func (p *Policy) Evaluate(ctx context.Context, in *module.Input) (*module.Result, error) {
	e := &evaluation{p: p, in: in, commits: map[string]commitInfo{}, res: &module.Result{}}
	if p.settings.Clock != nil && p.settings.PatternBudget > 0 {
		e.budget = &budget{left: p.settings.PatternBudget, clock: p.settings.Clock}
	}
	if p.unknownPreset != "" {
		e.warn(module.Violation{Code: module.CodeInvalidPolicy,
			Message: fmt.Sprintf("COMMITPOLICY extends preset %q, which the server config does not define; the preset is skipped.", p.unknownPreset)})
	}
	for _, u := range in.Updates {
		if u.Kind() == module.Delete {
			continue
		}
		for _, rs := range p.rulesets {
			if err := e.ruleset(ctx, rs, u); err != nil {
				return nil, err
			}
		}
	}
	return e.res, nil
}

func (e *evaluation) warn(v module.Violation) {
	e.res.Decision = max(e.res.Decision, module.Warn)
	e.res.Violations = append(e.res.Violations, v)
}

// ruleset applies one ruleset to one ref.
func (e *evaluation) ruleset(ctx context.Context, rs *ruleset, u module.RefUpdate) error {
	scope := module.Scope{Class: module.ClassOther, FirstParent: rs.except.MergedCommits}
	switch rs.refs.Kind {
	case module.RefsDefault:
		if u.Ref != e.in.Refs.Default() {
			return nil
		}
		scope.Class = module.ClassDefault
	case module.RefsList:
		if !rs.refs.MatchList(u.Ref) {
			return nil
		}
		scope.Class, scope.Key, scope.Protected = module.ClassProtected, "commitpolicy/"+rs.name, rs.refs.MatchList
	case module.RefsProtected:
		return e.protectedRuleset(ctx, rs, u)
	}
	commits, err := e.in.Changes.EnteringCommits(ctx, u, scope, MaxEnteringCommits)
	if done, err := e.listFailed(rs, u, err); done {
		return err
	}
	if len(rs.unknown) > 0 {
		e.warnUnknown(rs, u)
	}
	findings, err := e.check(ctx, rs, commits)
	if err != nil {
		return err
	}
	e.res.Touched = e.res.Touched || len(commits) > 0
	return e.report(ctx, rs, u, findings)
}

// protectedRuleset checks the candidates first and asks GitLab for the class
// of the ref only when some commit breaks a rule.
func (e *evaluation) protectedRuleset(ctx context.Context, rs *ruleset, u module.RefUpdate) error {
	candidates, err := e.in.Changes.CandidateCommits(ctx, u, MaxEnteringCommits)
	if done, err := e.listFailed(rs, u, err); done {
		return err
	}
	findings, err := e.check(ctx, rs, candidates)
	if err != nil || len(findings) == 0 {
		return err
	}
	class, status := e.in.Refs.Class(ctx, u.Ref)
	if !status.IsConfirmed() {
		e.warn(module.Violation{Code: module.CodeAuthorizationBackend, Rule: rs.name, Ref: u.Ref, Reason: status.Reason(),
			Message: "The class of the ref is unknown; this ruleset is skipped."})
		return nil
	}
	scope := module.Scope{Class: class, FirstParent: rs.except.MergedCommits}
	switch class {
	case module.ClassOther:
		return nil
	case module.ClassProtected:
		ps, status := e.in.Refs.ProtectedScope(ctx)
		if !status.IsConfirmed() {
			e.warn(module.Violation{Code: module.CodeAuthorizationBackend, Rule: rs.name, Ref: u.Ref, Reason: status.Reason(),
				Message: "The protected refs are unknown; this ruleset is skipped."})
			return nil
		}
		ps.FirstParent = rs.except.MergedCommits
		scope = ps
	}
	exact, err := e.in.Changes.EnteringCommits(ctx, u, scope, MaxEnteringCommits)
	if done, err := e.listFailed(rs, u, err); done {
		return err
	}
	if len(rs.unknown) > 0 {
		e.warnUnknown(rs, u)
	}
	in := map[string]bool{}
	for _, c := range exact {
		in[c] = true
	}
	kept := findings[:0]
	for _, f := range findings {
		if in[f.commit.oid] {
			kept = append(kept, f)
		}
	}
	e.res.Touched = e.res.Touched || len(exact) > 0
	return e.report(ctx, rs, u, kept)
}

// listFailed turns the limit of entering commits into a violation.
func (e *evaluation) listFailed(rs *ruleset, u module.RefUpdate, err error) (bool, error) {
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, module.ErrNotCommit):
		return true, nil
	case errors.Is(err, module.ErrLimitExceeded):
		e.res.Touched = true
		v := module.Violation{Code: module.CodeLimitExceeded, Rule: rs.name, Ref: u.Ref, Reason: err.Error(),
			Message: fmt.Sprintf("More than %d commits enter the ref; COMMITPOLICY does not check pushes this large.", MaxEnteringCommits)}
		if e.p.warnMode {
			e.warn(v)
		} else {
			e.res.Decision = module.Deny
			e.res.Violations = append(e.res.Violations, v)
		}
		return true, nil
	default:
		return true, err
	}
}

func (e *evaluation) warnUnknown(rs *ruleset, u module.RefUpdate) {
	e.warn(module.Violation{Code: module.CodeInvalidPolicy, Rule: rs.name, Ref: u.Ref,
		Message: fmt.Sprintf("Rules unknown to this version of gitlab-repopolicy are skipped: %s.", strings.Join(rs.unknown, ", "))})
}

// commitFinding is a finding of a commit.
type commitFinding struct {
	commit commitInfo
	header string
	finding
}

func (e *evaluation) load(ctx context.Context, oid string) (commitInfo, error) {
	if c, ok := e.commits[oid]; ok {
		return c, nil
	}
	obj, err := e.in.Objects.Read(ctx, oid, maxCommitObject)
	var c commitInfo
	switch {
	case errors.Is(err, module.ErrTooLarge):
		c = commitInfo{oid: oid, message: "\x00commit object larger than 1 MiB"}
	case err != nil:
		return commitInfo{}, err
	case obj.Type != "commit":
		return commitInfo{}, fmt.Errorf("%s is a %s, not a commit", oid, obj.Type)
	default:
		c = parseCommit(oid, obj.Data)
	}
	e.commits[oid] = c
	return c, nil
}

// check applies the rules of the ruleset to the commits, minus the exceptions.
func (e *evaluation) check(ctx context.Context, rs *ruleset, commits []string) ([]commitFinding, error) {
	var out []commitFinding
	for _, oid := range commits {
		c, err := e.load(ctx, oid)
		if err != nil {
			return nil, err
		}
		if rs.except.MergeCommits && c.parents > 1 {
			continue
		}
		m := ParseMessage(c.message, rs.conventional)
		if rs.except.GitReverts && isGitRevert(m) {
			continue
		}
		for _, f := range rs.check(m, e.budget, e.p.settings.PatternTimeout) {
			out = append(out, commitFinding{commit: c, header: m.Header, finding: f})
		}
	}
	return out, nil
}

// report applies except.users and the mode and records the violations.
func (e *evaluation) report(ctx context.Context, rs *ruleset, u module.RefUpdate, findings []commitFinding) error {
	if len(findings) == 0 {
		return nil
	}
	if len(rs.except.Users) > 0 {
		person, status := e.in.Principals.Person(ctx)
		if !status.IsConfirmed() {
			e.warn(module.Violation{Code: module.CodeAuthorizationBackend, Rule: rs.name, Ref: u.Ref, Reason: status.Reason(),
				Message: "The pusher cannot be matched against except.users; this ruleset is skipped."})
			return nil
		}
		for _, x := range rs.except.Users {
			if person.Username != "" && strings.EqualFold(x, person.Username) {
				return nil
			}
		}
	}
	type key struct {
		lv      level
		timeout bool
	}
	groups := map[key][]module.Item{}
	index := map[key]map[string]int{}
	var order []key
	for _, f := range findings {
		lv := f.level
		if e.p.warnMode && lv == levelError {
			lv = levelWarn
		}
		k := key{lv, f.timeout}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
			index[k] = map[string]int{}
		}
		i, ok := index[k][f.commit.oid]
		if !ok {
			i = len(groups[k])
			index[k][f.commit.oid] = i
			groups[k] = append(groups[k], module.Item{Commit: true, Name: fmt.Sprintf("%s %q", short(f.commit.oid), f.header)})
		}
		groups[k][i].Details = append(groups[k][i].Details, f.reason)
	}
	for _, k := range order {
		v := module.Violation{Code: module.CodeCommitFormat, Rule: rs.name, Ref: u.Ref, Items: groups[k], Message: rs.message, Hint: hint}
		if k.timeout {
			v.Code = module.CodeCommitCheckTimeout
		}
		if k.lv == levelError {
			e.res.Decision = module.Deny
			e.res.Violations = append(e.res.Violations, v)
		} else {
			e.warn(v)
		}
	}
	return nil
}

func short(oid string) string { return oid[:min(len(oid), 9)] }
