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
}

func newEvaluation(p *Policy, in *module.Input) *evaluation {
	return &evaluation{p: p, in: in, res: &module.Result{}}
}

func (e *evaluation) deny(v module.Violation) {
	e.res.Decision = module.Deny
	e.res.Touched = true
	e.res.Violations = append(e.res.Violations, v)
}

// unknown turns an unanswered question into a denial (FailClosed, D15).
func (e *evaluation) unknown(st module.Status, rule, ref string) {
	code, msg := module.CodeAuthorizationBackend, "GitLab could not confirm the authorization. Retry later; administrators are alerted."
	if st.IsLimit() {
		code, msg = module.CodeLimitExceeded, "The push exceeds the limits of the hook (time or API calls). Split it or ask an administrator."
	}
	e.deny(module.Violation{Code: code, Rule: rule, Ref: ref, Reason: st.Reason(), Message: msg})
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

// Evaluate checks every branch and tag update of the push (architecture §9, §12).
func (p *Policy) Evaluate(ctx context.Context, in *module.Input) (*module.Result, error) {
	e := newEvaluation(p, in)
	for _, u := range in.Updates {
		if u.Kind() == module.Delete {
			continue
		}
		if err := e.update(ctx, u); err != nil {
			return nil, err
		}
	}
	return e.res, nil
}

func (e *evaluation) update(ctx context.Context, u module.RefUpdate) error {
	candidates, err := e.in.Changes.CandidatePaths(ctx, u)
	if errors.Is(err, module.ErrNotCommit) {
		e.notCommit(ctx, u)
		return nil
	}
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

// notCommit denies a tag whose object does not peel to a commit when a rule
// covers the ref (architecture §12).
func (e *evaluation) notCommit(ctx context.Context, u module.RefUpdate) {
	for _, r := range e.p.rules {
		_, applies, st := e.ruleScope(ctx, r, u)
		if !st.IsConfirmed() {
			e.unknown(st, r.name, u.Ref)
			return
		}
		if applies {
			e.deny(module.Violation{Code: module.CodeTagObjectInvalid, Rule: r.name, Ref: u.Ref,
				Message: "The tag does not point to a commit, so its content cannot be checked."})
			return
		}
	}
}

// ruleScope returns the scope of a rule for the ref and whether the rule applies to it.
func (e *evaluation) ruleScope(ctx context.Context, r *rule, u module.RefUpdate) (module.Scope, bool, module.Status) {
	switch r.refs.Kind {
	case module.RefsList:
		if !r.refs.MatchList(u.Ref) {
			return module.Scope{}, false, module.Confirmed()
		}
		return module.Scope{Class: module.ClassProtected, Key: "pathpolicy/" + r.name, Protected: r.refs.MatchList}, true, module.Confirmed()
	case module.RefsDefault:
		if u.Ref != e.in.Refs.Default() {
			return module.Scope{}, false, module.Confirmed()
		}
		return module.Scope{Class: module.ClassDefault}, true, module.Confirmed()
	}
	scope, st := e.scopeOf(ctx, u.Ref)
	if !st.IsConfirmed() {
		return module.Scope{}, false, st
	}
	if r.refs.Kind == module.RefsProtected && scope.Class == module.ClassOther {
		return module.Scope{}, false, module.Confirmed()
	}
	return scope, true, module.Confirmed()
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
			Message: "Protected paths are changed only by people: a deploy key cannot change them.",
			Reason:  "principal is not a person: " + e.in.Principal.GLID})
		e.res.Events = append(e.res.Events, audit.Event{Event: audit.EventUnknownPrincipal, Ref: u.Ref, OldRev: u.Old, NewRev: u.New,
			Violations: []audit.Violation{{Rule: r.name, Paths: names}}})
	case !r.allows(person.Username):
		e.deny(module.Violation{Code: module.CodeProtectedPath, Rule: r.name, Ref: u.Ref, Items: hit,
			Message: r.text(), Reason: "not in allow: " + person.Username})
	}
	return nil
}

// text is the message of the rule, or the list of people who may change the paths.
func (r *rule) text() string {
	if r.message != "" {
		return r.message
	}
	return "Allowed to change these paths: " + strings.Join(r.allow, ", ") + "."
}

// policyChange decides on a change of PATHPOLICY itself (architecture §9.4).
func (e *evaluation) policyChange(ctx context.Context, u module.RefUpdate) {
	e.res.Touched = true
	person, st := e.in.Principals.Person(ctx)
	if !st.IsConfirmed() {
		e.unknown(st, "", u.Ref)
		return
	}
	if person.Username == "" {
		e.deny(module.Violation{Code: module.CodeUnknownPrincipal, Ref: u.Ref, Items: []module.Item{{Name: File}},
			Message: "PATHPOLICY is changed only by people: a deploy key cannot change it."})
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
	adminText := "PATHPOLICY is changed by its admins: " + strings.Join(e.p.admins, ", ") + "."
	if access < module.AccessOwner {
		e.deny(module.Violation{Code: module.CodePolicyAdmin, Ref: u.Ref, Items: []module.Item{{Name: File}},
			Message: adminText + "\nWithout an effective admin, an Owner of the project may change it (recovery).",
			Reason:  fmt.Sprintf("not an effective admin, access level %d", access)})
		return
	}
	n, st := countAdmins(ctx, e.in.Authz, e.p.admins)
	switch {
	case !st.IsConfirmed():
		e.unknown(st, "", u.Ref)
	case n > 0:
		e.deny(module.Violation{Code: module.CodePolicyAdmin, Ref: u.Ref, Items: []module.Item{{Name: File}},
			Message: adminText + "\nOwner recovery is possible only while no admin is effective.",
			Reason:  fmt.Sprintf("owner recovery refused: %d effective admins", n)})
	default:
		e.recovery(u, person, "no effective admins")
		e.deletionWarning(u)
	}
}

// recovery records an Owner recovery: allowed, printed and alerted (architecture §9.4, §16).
func (e *evaluation) recovery(u module.RefUpdate, person module.Person, reason string) {
	e.res.Touched = true
	e.res.Notices = append(e.res.Notices, fmt.Sprintf("PATHPOLICY recovery by Owner %s on %s: %s.", person.Username, u.Ref, reason))
	e.res.Events = append(e.res.Events, audit.Event{Event: audit.EventPolicyRecovery, Decision: "allow",
		Ref: u.Ref, OldRev: u.Old, NewRev: u.New, Reason: reason})
}

// deletionWarning warns when PATHPOLICY is removed while delegated files stay
// on the anchor: anyone with push may then change them (architecture §12.2).
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
		e.res.Notices = append(e.res.Notices, fmt.Sprintf("warning: PATHPOLICY is removed on %s; %s can then be changed by anyone who can push.",
			u.Ref, strings.Join(left, " and ")))
	}
}

// EvaluateAbsent is the bootstrap: without PATHPOLICY on the anchor, adding
// it to the default branch needs at least Maintainer (architecture §9.4).
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
				Message: "The first PATHPOLICY of a project is added to the default branch by a Maintainer or Owner.",
				Reason:  fmt.Sprintf("bootstrap needs Maintainer, access level %d", access)})
			continue
		}
		e.res.Events = append(e.res.Events, audit.Event{Event: audit.EventPolicyBootstrap, Decision: "allow",
			Ref: c.Ref, OldRev: u.Old, NewRev: u.New})
	}
	return e.res, nil
}

// EvaluateInvalid handles an invalid PATHPOLICY on the anchor: nothing changes
// except ref deletions and an Owner fixing or removing PATHPOLICY (architecture §14).
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
	msg := "PATHPOLICY on the default branch is invalid: " + parseErr.Error() + "\nNothing changes until an Owner of the project fixes or removes it."
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
				e.recovery(u, person, "the policy on the anchor is invalid")
			}
			return e.res, nil
		}
	}
	for _, u := range refs {
		e.deny(module.Violation{Code: module.CodeInvalidPolicy, Ref: u.Ref, Message: msg})
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
