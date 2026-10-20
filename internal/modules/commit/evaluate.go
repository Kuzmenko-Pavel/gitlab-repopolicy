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
	commits map[string]commitInfo
	res     *module.Result
}

// Evaluate checks the commits entering every branch of the push. Tags are
// not checked: the core accepts only tags on commits of branches.
func (p *Policy) Evaluate(ctx context.Context, in *module.Input) (*module.Result, error) {
	e := &evaluation{p: p, in: in, commits: map[string]commitInfo{}, res: &module.Result{}}
	if p.unusable != nil {
		if pushesBranches(in) {
			e.res.Touched = true
			e.warn(module.Violation{Code: module.CodeInvalidPolicy,
				Title: "COMMITPOLICY cannot be used, so commit messages are not checked until it is fixed",
				Facts: []string{"problem: COMMITPOLICY " + p.unusable.Error()}})
		}
		return e.res, nil
	}
	for _, u := range in.Updates {
		if u.Kind() == module.Delete || !u.IsBranch() {
			continue
		}
		for _, r := range p.rules {
			if err := e.rule(ctx, r, u); err != nil {
				return nil, err
			}
		}
	}
	return e.res, nil
}

func pushesBranches(in *module.Input) bool {
	for _, u := range in.Updates {
		if u.Kind() != module.Delete && u.IsBranch() {
			return true
		}
	}
	return false
}

func (e *evaluation) warn(v module.Violation) {
	e.res.Decision = max(e.res.Decision, module.Warn)
	e.res.Violations = append(e.res.Violations, v)
}

// rule applies one rule to one branch.
func (e *evaluation) rule(ctx context.Context, r *rule, u module.RefUpdate) error {
	scope := module.Scope{Class: module.ClassOther, FirstParent: r.except.MergedCommits}
	name := strings.TrimPrefix(u.Ref, "refs/heads/")
	switch {
	case r.match.All:
		// A merge into the default branch brings every commit of the merged
		// branch; on other branches only commits that no ref has yet are new.
		if u.Ref == e.in.Refs.Default() {
			scope.Class = module.ClassDefault
		}
	case r.match.Default:
		if u.Ref != e.in.Refs.Default() {
			return nil
		}
		scope.Class = module.ClassDefault
	default:
		if !r.match.Matches(name) {
			return nil
		}
		scope.Class, scope.Key, scope.Protected = module.ClassProtected, "commitpolicy/"+r.name, r.listed
	}
	commits, err := e.in.Changes.EnteringCommits(ctx, u, scope, MaxEnteringCommits)
	if done, err := e.listFailed(r, u, err); done {
		return err
	}
	if len(r.unknown) > 0 {
		e.warnUnknown(r, u)
	}
	findings, err := e.check(ctx, r, commits)
	if err != nil {
		return err
	}
	e.res.Touched = e.res.Touched || len(commits) > 0
	return e.report(ctx, r, u, findings)
}

// listed reports whether a branch is in the match list of the rule.
func (r *rule) listed(ref string) bool {
	name, ok := strings.CutPrefix(ref, "refs/heads/")
	return ok && r.match.Matches(name)
}

// listFailed turns the limit of entering commits into a violation.
func (e *evaluation) listFailed(r *rule, u module.RefUpdate, err error) (bool, error) {
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, module.ErrNotCommit):
		return true, nil
	case errors.Is(err, module.ErrLimitExceeded):
		e.res.Touched = true
		v := module.Violation{Code: module.CodeLimitExceeded, Rule: r.name, Ref: u.Ref, Reason: err.Error(), Source: r.source,
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

func (e *evaluation) warnUnknown(r *rule, u module.RefUpdate) {
	e.warn(module.Violation{Code: module.CodeInvalidPolicy, Rule: r.name, Ref: u.Ref, Source: r.source,
		Title: "checks unknown to this version of gitlab-repopolicy are skipped: " + strings.Join(r.unknown, ", ")})
}

// skipped is the warning of a rule that was not applied because GitLab did
// not answer: COMMITPOLICY fails open.
func skipped(r *rule, u module.RefUpdate, st module.Status) module.Violation {
	return module.Violation{Code: module.CodeAuthorizationBackend, Rule: r.name, Ref: u.Ref, Reason: st.Reason(), Source: r.source,
		Title: "commit messages were not checked by this rule: the GitLab API did not answer"}
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

// check applies the checks of the rule to the commits, minus the exceptions.
func (e *evaluation) check(ctx context.Context, r *rule, commits []string) ([]commitFinding, error) {
	var out []commitFinding
	for _, oid := range commits {
		c, err := e.load(ctx, oid)
		if err != nil {
			return nil, err
		}
		if r.except.MergeCommit && c.parents > 1 {
			continue
		}
		m := ParseMessage(c.message, r.conventional)
		if r.except.GitReverts && isGitRevert(m) {
			continue
		}
		for _, f := range r.apply(m) {
			out = append(out, commitFinding{commit: c, header: m.Header, finding: f})
		}
	}
	return out, nil
}

// report applies except.users and the mode and records the violations.
func (e *evaluation) report(ctx context.Context, r *rule, u module.RefUpdate, findings []commitFinding) error {
	if len(findings) == 0 {
		return nil
	}
	if len(r.except.Users) > 0 {
		person, status := e.in.Principals.Person(ctx)
		if !status.IsConfirmed() {
			e.warn(skipped(r, u, status))
			return nil
		}
		for _, x := range r.except.Users {
			if person.Username != "" && strings.EqualFold(x, person.Username) {
				return nil
			}
		}
	}
	groups := map[level][]module.Item{}
	index := map[level]map[string]int{}
	var order []level
	for _, f := range findings {
		lv := f.level
		if e.p.warnMode && lv == levelError {
			lv = levelWarn
		}
		if _, ok := groups[lv]; !ok {
			order = append(order, lv)
			index[lv] = map[string]int{}
		}
		i, ok := index[lv][f.commit.oid]
		if !ok {
			i = len(groups[lv])
			index[lv][f.commit.oid] = i
			groups[lv] = append(groups[lv], module.Item{Commit: true, Name: fmt.Sprintf("%s %q", short(f.commit.oid), f.header)})
		}
		groups[lv][i].Details = append(groups[lv][i].Details, f.reason)
	}
	for _, lv := range order {
		v := r.violation(u, groups[lv], lv)
		if lv == levelError {
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
func (r *rule) violation(u module.RefUpdate, items []module.Item, lv level) module.Violation {
	n := len(items)
	v := module.Violation{Code: module.CodeCommitFormat, Rule: r.name, Ref: u.Ref, Items: items, Message: r.message,
		Help: r.helpOf(), Source: r.source}
	if lv == levelWarn {
		v.Title = fmt.Sprintf("%d commit %s not follow the format; this is a warning, it does not block the push", n, verb(n))
		v.Facts = r.format()
		v.Hint = "next time: follow the format; commits that are pushed stay as they are"
		return v
	}
	v.Title = fmt.Sprintf("%d commit %s not follow the format", n, verb(n))
	v.Facts = r.format()
	v.Hint = "how to fix: git rebase -i and reword these commits, then push again"
	if n == 1 {
		v.Hint = "how to fix: reword the commit (git commit --amend for the last one, git rebase -i for an older one), then push again"
	}
	v.Summary = fmt.Sprintf("commit %s does not follow the format of COMMITPOLICY rule %s", items[0].Name, r.name)
	if len(items[0].Details) > 0 {
		v.Summary += ": " + items[0].Details[0]
	}
	return v
}

func verb(n int) string {
	if n == 1 {
		return "message does"
	}
	return "messages do"
}

// format describes the expected header once, with the types and scopes of the checks.
func (r *rule) format() []string {
	if !r.conventional {
		return nil
	}
	out := []string{"required: type(scope): subject, for example feat(api): add refunds"}
	for _, c := range r.checks {
		switch c.name {
		case "type-enum":
			out = append(out, "types: "+strings.Join(c.list, ", "))
		case "scope-enum":
			out = append(out, "scopes: "+strings.Join(c.list, ", "))
		}
	}
	return out
}
