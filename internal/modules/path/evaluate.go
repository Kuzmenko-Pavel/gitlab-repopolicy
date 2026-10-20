package path

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// isPolicyPath reports a path of PATHPOLICY itself or anything under it.
func isPolicyPath(p string) bool { return p == File || strings.HasPrefix(p, File+"/") }

// evaluation is the state of one Evaluate call.
type evaluation struct {
	p   *Policy
	in  *module.Input
	res *module.Result
	// recovered are the refs of an Owner recovery in this push.
	recovered map[string]bool
}

func newEvaluation(p *Policy, in *module.Input) *evaluation {
	return &evaluation{p: p, in: in, res: &module.Result{}, recovered: map[string]bool{}}
}

func (e *evaluation) deny(v module.Violation) {
	e.res.Decision = module.Deny
	e.res.Touched = true
	e.res.Violations = append(e.res.Violations, v)
}

// unknown turns an unanswered question into a denial: PATHPOLICY fails closed.
func (e *evaluation) unknown(st module.Status, rule, ref string) {
	e.deny(Unanswered(st, rule, ref))
}

// Unanswered is the denial when GitLab did not answer a question of a rule:
// a limit of the push, a setting of the server or a failure of the API. None
// of them is the fault of the pusher.
func Unanswered(st module.Status, rule, ref string) module.Violation {
	v := module.Violation{Code: module.CodeAuthorizationBackend, Rule: rule, Ref: ref, Reason: st.Reason()}
	switch {
	case st.IsLimit():
		v.Code = module.CodeLimitExceeded
		v.Facts = []string{"limit reached: " + limitName(st.Reason())}
		v.Hint = "if the push is large, push it in parts; if it is small, the limit is too low on the server"
		v.Summary = "the check did not finish within the server limits (" + limitName(st.Reason()) + "); the push is rejected"
	case st.IsSetup():
		v.Title = "this project is not connected to gitlab-repopolicy correctly"
		v.Facts = []string{"this is a setting of the server, not a problem with your permissions; retrying will not help"}
		v.Summary = "this project is not connected to gitlab-repopolicy correctly; not a problem with your permissions, ask an instance administrator"
	default:
		v.Facts = []string{"this is a problem of the server, not of your permissions"}
		v.Hint = "retry in a few minutes"
		v.Summary = "could not check permissions: the GitLab API did not answer; not a problem with your permissions, retry in a few minutes"
	}
	return v
}

// limitName names the limit that ran out, after the error of the GitLab client.
func limitName(reason string) string {
	if strings.Contains(reason, "budget") {
		return "the number of GitLab API calls of a push (max_api_calls)"
	}
	return "the time of a push (timeouts.total)"
}

// scopeOf returns the class of the ref as a change scope; it asks GitLab only
// for refs other than the default branch.
func (e *evaluation) scopeOf(ctx context.Context, ref string) (module.Scope, module.Status) {
	if ref == e.in.Refs.Default() {
		return module.Scope{Class: module.ClassDefault}, module.Confirmed()
	}
	class, st := e.in.Refs.Class(ctx, ref)
	if !st.IsConfirmed() {
		return module.Scope{}, st
	}
	if class == module.ClassProtected {
		return e.in.Refs.ProtectedScope(ctx)
	}
	return module.Scope{Class: class}, module.Confirmed()
}

// Evaluate checks every branch update of the push. Tags are not checked: the
// core accepts only tags on commits of branches, and those commits are
// checked as part of the branches.
func (p *Policy) Evaluate(ctx context.Context, in *module.Input) (*module.Result, error) {
	e := newEvaluation(p, in)
	for _, u := range in.Updates {
		if u.Kind() == module.Delete || !u.IsBranch() {
			continue
		}
		if err := e.update(ctx, u); err != nil {
			return nil, err
		}
	}
	for i, v := range e.res.Violations {
		if v.Code == module.CodeProtectedPath && e.recovered[v.Ref] {
			// The people of the rule are the admins who left: an Owner recovers PATHPOLICY first.
			e.res.Violations[i].Hint = "a recovery push may change only PATHPOLICY: push these files after the new PATHPOLICY is in the default branch"
		}
	}
	return e.res, nil
}

func (e *evaluation) update(ctx context.Context, u module.RefUpdate) error {
	candidates, err := e.in.Changes.CandidatePaths(ctx, u)
	if err != nil {
		return err
	}
	policyTouched := false
	var touched []*rule
	for _, c := range candidates {
		policyTouched = policyTouched || isPolicyPath(c)
	}
	for _, r := range e.p.rules {
		for _, c := range candidates {
			if r.matches(c) {
				touched = append(touched, r)
				break
			}
		}
	}
	if !policyTouched && len(touched) == 0 {
		return nil
	}
	if policyTouched {
		// Invariant 3: a change of PATHPOLICY is checked on every ref.
		scope, st := e.scopeOf(ctx, u.Ref)
		if !st.IsConfirmed() {
			e.unknown(st, "", u.Ref)
			return nil
		}
		paths, err := e.in.Changes.Paths(ctx, u, scope)
		if err != nil {
			return err
		}
		for _, p := range paths {
			if isPolicyPath(p) {
				e.policyChange(ctx, u)
				break
			}
		}
	}
	for _, r := range touched {
		if err := e.rule(ctx, r, u); err != nil {
			return err
		}
	}
	return nil
}

// ruleScope returns the scope of a rule for the branch and whether the rule
// applies to it. A branch of a match list is checked like a protected branch.
func (e *evaluation) ruleScope(ctx context.Context, r *rule, u module.RefUpdate) (module.Scope, bool, module.Status) {
	switch {
	case r.match.IsList():
		if !r.protects(u.Ref) {
			return module.Scope{}, false, module.Confirmed()
		}
		return module.Scope{Class: module.ClassProtected, Key: "pathpolicy/" + r.name, Protected: r.protects}, true, module.Confirmed()
	case r.match.Default:
		if u.Ref != e.in.Refs.Default() {
			return module.Scope{}, false, module.Confirmed()
		}
		return module.Scope{Class: module.ClassDefault}, true, module.Confirmed()
	}
	scope, st := e.scopeOf(ctx, u.Ref)
	if !st.IsConfirmed() {
		return module.Scope{}, false, st
	}
	return scope, true, module.Confirmed()
}

// protects reports whether a branch is in the match list of the rule.
func (r *rule) protects(ref string) bool {
	name, ok := strings.CutPrefix(ref, "refs/heads/")
	return ok && r.match.Matches(name)
}

// rule checks one touched rule on one ref.
func (e *evaluation) rule(ctx context.Context, r *rule, u module.RefUpdate) error {
	scope, applies, st := e.ruleScope(ctx, r, u)
	if !st.IsConfirmed() {
		e.unknown(st, r.name, u.Ref)
		return nil
	}
	if !applies {
		return nil
	}
	paths, err := e.in.Changes.Paths(ctx, u, scope)
	if err != nil {
		return err
	}
	var hit []module.Item
	var names []string
	for _, p := range paths {
		if r.matches(p) {
			hit = append(hit, module.Item{Name: p})
			names = append(names, p)
		}
	}
	if len(hit) == 0 {
		return nil
	}
	e.res.Touched = true
	person, st := e.in.Principals.Person(ctx)
	switch {
	case !st.IsConfirmed():
		e.unknown(st, r.name, u.Ref)
	case person.Username == "":
		e.deny(module.Violation{Code: module.CodeUnknownPrincipal, Rule: r.name, Ref: u.Ref, Items: hit,
			Facts:   []string{e.notAPerson(), "may change them: " + strings.Join(r.allow, ", ")},
			Hint:    "push with your own SSH key or HTTPS, or with a project access token whose bot user is in the rule",
			Summary: fmt.Sprintf("%s: only people listed in PATHPOLICY rule %s may change it", filesText(names), r.name),
			Reason:  "principal is not a person: " + e.in.Principal.GLID, Help: r.helpOf(), Source: r.source})
		e.res.Events = append(e.res.Events, audit.Event{Event: audit.EventUnknownPrincipal, Ref: u.Ref, OldRev: u.Old, NewRev: u.New,
			Violations: []audit.Violation{{Rule: r.name, Paths: names}}})
	case !r.allows(person.Username):
		e.deny(module.Violation{Code: module.CodeProtectedPath, Rule: r.name, Ref: u.Ref, Items: hit,
			Facts:   []string{"may change them: " + strings.Join(r.allow, ", ")},
			Message: r.message, Hint: r.advice(),
			Summary: fmt.Sprintf("you may not change %s (PATHPOLICY rule %s; may change: %s)", filesText(names), r.name, strings.Join(r.allow, ", ")),
			Reason:  "not in allow: " + person.Username, Help: r.helpOf(), Source: r.source})
	}
	return nil
}

// advice is what to do about a denied change of the paths: a rule for every
// branch also stops a merge request from the branch.
func (r *rule) advice() string {
	if r.match.All {
		return "this rule applies to every branch: ask one of them to push the change"
	}
	return "ask one of them to make the change, or to merge it from your branch"
}

// filesText names the files of a denial in one line.
func filesText(files []string) string {
	switch len(files) {
	case 0:
		return "these files"
	case 1:
		return files[0]
	}
	return fmt.Sprintf("%s and %d more files", files[0], len(files)-1)
}

// notAPerson says why the pusher is no person, with what the pusher knows.
func (e *evaluation) notAPerson() string {
	if kind, id := e.in.Principal.Kind(); kind == module.PrincipalKey {
		return fmt.Sprintf("pushed with SSH key %d, which is not a personal key of a GitLab user", id)
	}
	return "the push is not made by a GitLab user"
}

// policyChange decides on a change of PATHPOLICY itself.
func (e *evaluation) policyChange(ctx context.Context, u module.RefUpdate) {
	e.res.Touched = true
	person, st := e.in.Principals.Person(ctx)
	if !st.IsConfirmed() {
		e.unknown(st, "", u.Ref)
		return
	}
	if person.Username == "" {
		e.deny(module.Violation{Code: module.CodeUnknownPrincipal, Ref: u.Ref, Items: []module.Item{{Name: File}},
			Title: "only PATHPOLICY admins may change PATHPOLICY", Facts: []string{e.notAPerson(), "admins: " + strings.Join(e.p.admins, ", ")},
			Summary: "only PATHPOLICY admins may change PATHPOLICY (admins: " + strings.Join(e.p.admins, ", ") + ")"})
		return
	}
	access, st := e.in.Authz.PusherAccess(ctx)
	if !st.IsConfirmed() {
		e.unknown(st, "", u.Ref)
		return
	}
	if e.p.isAdmin(person.Username) && access >= module.AccessMaintainer {
		e.deletionWarning(u)
		return
	}
	if access < module.AccessOwner {
		e.deny(e.adminOnly(u.Ref, fmt.Sprintf("not an effective admin, access level %d", access)))
		return
	}
	n, st := countAdmins(ctx, e.in.Authz, e.p.admins)
	switch {
	case !st.IsConfirmed():
		e.unknown(st, "", u.Ref)
	case n > 0:
		e.deny(e.adminOnly(u.Ref, fmt.Sprintf("owner recovery refused: %d effective admins", n)))
	default:
		e.recovery(u, person, "it has no working admins")
		e.deletionWarning(u)
	}
}

// adminOnly is the denial of a change of PATHPOLICY by somebody who is not
// one of its admins.
func (e *evaluation) adminOnly(ref, reason string) module.Violation {
	admins := strings.Join(e.p.admins, ", ")
	return module.Violation{Code: module.CodePolicyAdmin, Ref: ref, Items: []module.Item{{Name: File}},
		Facts:   []string{"admins: " + admins},
		Hint:    "ask one of them to make this change; this applies to every branch, including yours",
		Summary: "only PATHPOLICY admins may change PATHPOLICY (admins: " + admins + ")",
		Reason:  reason}
}

// recovery records an Owner recovery: allowed, printed and alerted.
func (e *evaluation) recovery(u module.RefUpdate, person module.Person, reason string) {
	e.res.Touched = true
	e.recovered[u.Ref] = true
	e.res.Notices = append(e.res.Notices, fmt.Sprintf("PATHPOLICY is changed by Owner %s because %s. This is logged.", person.Username, reason))
	e.res.Events = append(e.res.Events, audit.Event{Event: audit.EventPolicyRecovery, Decision: "allow",
		Ref: u.Ref, OldRev: u.Old, NewRev: u.New, Reason: reason})
}

// deletionWarning warns when PATHPOLICY is removed while delegated files stay
// on the anchor: anyone with push may then change them.
func (e *evaluation) deletionWarning(u module.RefUpdate) {
	deleted := false
	for _, c := range e.in.PolicyChanges {
		if c.Ref == u.Ref && c.File == File && c.Deleted {
			deleted = true
		}
	}
	if !deleted {
		return
	}
	var left []string
	for _, f := range e.in.AnchorPolicies {
		if f != File {
			left = append(left, f)
		}
	}
	if len(left) > 0 {
		e.res.Notices = append(e.res.Notices, fmt.Sprintf("push accepted; %s deletes PATHPOLICY. After it is merged into the default branch, anyone who can push may change %s, and no files are protected.",
			strings.TrimPrefix(strings.TrimPrefix(u.Ref, "refs/heads/"), "refs/tags/"), strings.Join(left, " and ")))
	}
}

// EvaluateAbsent is the bootstrap: without PATHPOLICY on the anchor, adding
// it to the default branch needs at least Maintainer.
func (*Module) EvaluateAbsent(ctx context.Context, in *module.Input) (*module.Result, error) {
	e := newEvaluation(&Policy{}, in)
	for _, c := range in.PolicyChanges {
		if c.File != File || c.Deleted || c.Ref != in.Refs.Default() {
			continue
		}
		u := updateOf(in, c.Ref)
		e.res.Touched = true
		person, st := e.in.Principals.Person(ctx)
		if !st.IsConfirmed() {
			e.unknown(st, "", c.Ref)
			continue
		}
		access := 0
		if person.Username != "" {
			if access, st = in.Authz.PusherAccess(ctx); !st.IsConfirmed() {
				e.unknown(st, "", c.Ref)
				continue
			}
		}
		if access < module.AccessMaintainer {
			e.deny(module.Violation{Code: module.CodePolicyAdmin, Ref: c.Ref, Items: []module.Item{{Name: File}},
				Title:   "the first PATHPOLICY of a project is added by a Maintainer or Owner",
				Hint:    "ask a Maintainer of the project to add it",
				Summary: "the first PATHPOLICY of a project is added to the default branch by a Maintainer or Owner",
				Reason:  fmt.Sprintf("bootstrap needs Maintainer, access level %d", access)})
			continue
		}
		e.res.Events = append(e.res.Events, audit.Event{Event: audit.EventPolicyBootstrap, Decision: "allow",
			Ref: c.Ref, OldRev: u.Old, NewRev: u.New})
	}
	return e.res, nil
}

// EvaluateInvalid handles an invalid PATHPOLICY on the anchor: nothing changes
// except ref deletions and an Owner fixing or removing PATHPOLICY.
func (*Module) EvaluateInvalid(ctx context.Context, in *module.Input, parseErr error) (*module.Result, error) {
	e := newEvaluation(&Policy{}, in)
	e.res.Events = append(e.res.Events, audit.Event{Event: audit.EventInvalidPolicy, Code: module.CodeInvalidPolicy, Reason: parseErr.Error()})
	onlyPolicy := true
	var refs []module.RefUpdate
	for _, u := range in.Updates {
		if u.Kind() == module.Delete {
			continue
		}
		refs = append(refs, u)
		paths, err := in.Changes.CandidatePaths(ctx, u)
		if err != nil && !errors.Is(err, module.ErrNotCommit) {
			return nil, err
		}
		if errors.Is(err, module.ErrNotCommit) {
			onlyPolicy = false
		}
		for _, p := range paths {
			if !isPolicyPath(p) {
				onlyPolicy = false
			}
		}
	}
	if len(refs) == 0 {
		return e.res, nil
	}
	branch := strings.TrimPrefix(in.Refs.Default(), "refs/heads/")
	broken := module.Violation{Code: module.CodeInvalidPolicy,
		Title: "PATHPOLICY in " + branch + " is broken, so no changes are accepted",
		Facts: []string{"this is not caused by your change", "problem: PATHPOLICY " + parseErr.Error()},
		Hint: "who can fix it: an Owner of the project, with a push that changes only PATHPOLICY\n" +
			"admins of PATHPOLICY cannot fix it while the file is unreadable",
		Summary: "PATHPOLICY in " + branch + " is broken, no changes are accepted; not caused by your change, an Owner of the project must fix it"}
	if onlyPolicy {
		person, st := e.in.Principals.Person(ctx)
		access := 0
		if st.IsConfirmed() && person.Username != "" {
			access, st = in.Authz.PusherAccess(ctx)
		}
		switch {
		case !st.IsConfirmed():
			e.unknown(st, "", refs[0].Ref)
			return e.res, nil
		case access >= module.AccessOwner:
			for _, u := range refs {
				e.recovery(u, person, "the PATHPOLICY in the default branch is broken")
			}
			return e.res, nil
		}
	}
	for _, u := range refs {
		v := broken
		v.Ref = u.Ref
		e.deny(v)
	}
	e.res.Events[0].Decision = "deny"
	return e.res, nil
}

func updateOf(in *module.Input, ref string) module.RefUpdate {
	for _, u := range in.Updates {
		if u.Ref == ref {
			return u
		}
	}
	return module.RefUpdate{Ref: ref}
}
