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
			Title: fmt.Sprintf("COMMITPOLICY extends preset %q, which the server config does not define, so the preset is skipped", p.unknownPreset)})
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
		e.warn(skipped(rs, u, status))
		return nil
	}
	scope := module.Scope{Class: class, FirstParent: rs.except.MergedCommits}
	switch class {
	case module.ClassOther:
		return nil
	case module.ClassProtected:
		ps, status := e.in.Refs.ProtectedScope(ctx)
		if !status.IsConfirmed() {
			e.warn(skipped(rs, u, status))
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
		v := module.Violation{Code: module.CodeLimitExceeded, Rule: rs.name, RuleKind: "ruleset", Ref: u.Ref, Reason: err.Error(),
			Facts:   []string{fmt.Sprintf("limit reached: more than %d commits enter the ref", MaxEnteringCommits)},
			Hint:    "push the branch in parts, for example git push origin <commit>:refs/heads/<branch>, then the rest",
			Summary: fmt.Sprintf("more than %d commits enter the ref, COMMITPOLICY does not check pushes this large; push the branch in parts", MaxEnteringCommits)}
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
	e.warn(module.Violation{Code: module.CodeInvalidPolicy, Rule: rs.name, RuleKind: "ruleset", Ref: u.Ref,
		Title: "rules unknown to this version of gitlab-repopolicy are skipped: " + strings.Join(rs.unknown, ", ")})
}

// skipped is the warning of a ruleset that was not applied because GitLab
// did not answer: COMMITPOLICY fails open.
func skipped(rs *ruleset, u module.RefUpdate, st module.Status) module.Violation {
	return module.Violation{Code: module.CodeAuthorizationBackend, Rule: rs.name, RuleKind: "ruleset", Ref: u.Ref, Reason: st.Reason(),
		Title: "commit messages were not checked by this ruleset: the GitLab API did not answer"}
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
			e.warn(skipped(rs, u, status))
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
		v := rs.violation(u, groups[k], k.lv, k.timeout)
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

// violation tells the author what is wrong with the commits and how to fix
// them; a warning does not ask to rewrite commits that are already pushed.
func (rs *ruleset) violation(u module.RefUpdate, items []module.Item, lv level, timeout bool) module.Violation {
	n := len(items)
	v := module.Violation{Code: module.CodeCommitFormat, Rule: rs.name, RuleKind: "ruleset", Ref: u.Ref, Items: items, Message: rs.message}
	switch {
	case timeout:
		v.Code = module.CodeCommitCheckTimeout
		v.Title = `rule "pattern" could not check a commit message in time`
		v.Hint = "a different message usually passes; if ordinary messages fail too, the rule is too slow and COMMITPOLICY needs a fix"
		v.Summary = fmt.Sprintf(`rule "pattern" of COMMITPOLICY ruleset %s could not check commit %s in time`, rs.name, items[0].Name)
	case lv == levelWarn:
		v.Title = fmt.Sprintf("%d commit %s not follow the format; this is a warning, it does not block the push", n, verb(n))
		v.Facts = rs.format()
		v.Hint = "next time: follow the format; commits that are pushed stay as they are"
	default:
		v.Title = fmt.Sprintf("%d commit %s not follow the format", n, verb(n))
		v.Facts = rs.format()
		v.Hint = "how to fix: git rebase -i and reword these commits, then push again"
		if n == 1 {
			v.Hint = "how to fix: reword the commit (git commit --amend for the last one, git rebase -i for an older one), then push again"
		}
		v.Summary = fmt.Sprintf("commit %s does not follow the format of COMMITPOLICY ruleset %s", items[0].Name, rs.name)
		if len(items[0].Details) > 0 {
			v.Summary += ": " + items[0].Details[0]
		}
	}
	return v
}

func verb(n int) string {
	if n == 1 {
		return "message does"
	}
	return "messages do"
}

// format describes the expected header once, with the types and scopes of the rules.
func (rs *ruleset) format() []string {
	if !rs.conventional {
		return nil
	}
	out := []string{"required: type(scope): subject, for example feat(api): add refunds"}
	for _, r := range rs.rules {
		switch r.name {
		case "type-enum":
			out = append(out, "types: "+strings.Join(r.list, ", "))
		case "scope-enum":
			out = append(out, "scopes: "+strings.Join(r.list, ", "))
		}
	}
	return out
}
