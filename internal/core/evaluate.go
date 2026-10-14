package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// evaluate runs the modules in order; the decision is the AND of all of them.
func (r *receive) evaluate(ctx context.Context) {
	for _, l := range r.policies {
		var (
			res *module.Result
			err error
		)
		switch {
		case !l.present:
			ev, ok := l.mod.(module.AbsentEvaluator)
			if !ok {
				continue
			}
			in := r.inputFor(needsIdentity(l.mod))
			res, err = call(func() (*module.Result, error) { return ev.EvaluateAbsent(ctx, in) })
		case l.invalid != nil:
			res, err = r.evaluateInvalid(ctx, l)
		default:
			in := r.inputFor(l.policy.Needs().Identity)
			res, err = call(func() (*module.Result, error) { return l.policy.Evaluate(ctx, in) })
		}
		if err != nil {
			r.moduleFailed(ctx, l, err)
			continue
		}
		r.apply(l, res)
	}
}

func needsIdentity(m module.Module) bool {
	im, ok := m.(module.IdentityModule)
	return ok && im.NeedsIdentity()
}

// call runs a module entry point; a panic becomes an error.
func call(fn func() (*module.Result, error)) (res *module.Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("module panic: %v", p)
		}
	}()
	res, err = fn()
	if err == nil && res == nil {
		err = errors.New("module returned no result")
	}
	return res, err
}

// evaluateInvalid handles an invalid policy on the anchor (architecture §14).
func (r *receive) evaluateInvalid(ctx context.Context, l *loaded) (*module.Result, error) {
	if ev, ok := l.mod.(module.InvalidEvaluator); ok {
		in := r.inputFor(needsIdentity(l.mod))
		return call(func() (*module.Result, error) { return ev.EvaluateInvalid(ctx, in, l.invalid) })
	}
	msg := fmt.Sprintf("%s on the default branch is invalid: %v", l.mod.File(), l.invalid)
	ev := audit.Event{Event: audit.EventInvalidPolicy, Module: l.mod.Name(), Code: module.CodeInvalidPolicy, Reason: l.invalid.Error()}
	if l.mod.OnInvalid() == module.FailOpen {
		ev.Decision = "warn"
		return &module.Result{Decision: module.Warn, Touched: true, Events: []audit.Event{ev},
			Violations: []module.Violation{{Code: module.CodeInvalidPolicy, Message: msg + "\nThe check is skipped until it is fixed."}}}, nil
	}
	res := &module.Result{Touched: true}
	for _, u := range r.updates {
		if u.Kind() == module.Delete {
			continue
		}
		res.Decision = module.Deny
		res.Violations = append(res.Violations, module.Violation{Code: module.CodeInvalidPolicy, Ref: u.Ref,
			Message: msg + "\nNothing can change until it is fixed on the default branch."})
	}
	ev.Decision = res.Decision.String()
	res.Events = []audit.Event{ev}
	return res, nil
}

// moduleFailed handles an error or panic of a module: FailClosed denies, FailOpen warns.
func (r *receive) moduleFailed(ctx context.Context, l *loaded, err error) {
	code := module.CodeInternalError
	if errors.Is(err, module.ErrLimitExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		code = module.CodeLimitExceeded
	}
	v := module.Violation{Code: code, Message: coreMessage(code, r.settings.Total), Reason: err.Error()}
	e := audit.Event{Event: eventFor(code), Module: l.mod.Name(), Code: code, Reason: err.Error(), PolicyBlob: l.blob}
	if l.mod.OnInvalid() == module.FailOpen {
		v.Message = "The check was skipped because of an internal error."
		if code == module.CodeLimitExceeded {
			v.Message = "The check was skipped: the push exceeds the limits."
		}
		e.Decision = "warn"
		r.addWarning(l.mod.File(), v)
	} else {
		e.Decision = "deny"
		r.addSection(l.mod.File(), v)
	}
	r.events = append(r.events, r.event(e))
}

// apply records the result of a module.
func (r *receive) apply(l *loaded, res *module.Result) {
	switch res.Decision {
	case module.Deny:
		r.addSection(l.mod.File(), res.Violations...)
	case module.Warn:
		r.addWarning(l.mod.File(), res.Violations...)
	}
	r.notices = append(r.notices, res.Notices...)
	for _, e := range res.Events {
		if e.Module == "" {
			e.Module = l.mod.Name()
		}
		if e.PolicyBlob == "" {
			e.PolicyBlob = l.blob
		}
		r.events = append(r.events, r.event(e))
	}
	if !res.Touched && len(res.Violations) == 0 {
		return
	}
	r.events = append(r.events, r.decisionEvents(l, res)...)
}

// decisionEvents are the "decision" events of a module: one per ref with
// violations, or one for the push when the rules were touched without any.
func (r *receive) decisionEvents(l *loaded, res *module.Result) []audit.Event {
	byRef := map[string][]module.Violation{}
	var order []string
	for _, v := range res.Violations {
		if _, ok := byRef[v.Ref]; !ok {
			order = append(order, v.Ref)
		}
		byRef[v.Ref] = append(byRef[v.Ref], v)
	}
	if len(order) == 0 {
		e := audit.Event{Event: audit.EventDecision, Module: l.mod.Name(), Decision: res.Decision.String(), PolicyBlob: l.blob}
		if len(r.updates) == 1 {
			u := r.updates[0]
			e.Ref, e.OldRev, e.NewRev = u.Ref, u.Old, u.New
		}
		return []audit.Event{r.event(e)}
	}
	var events []audit.Event
	for _, ref := range order {
		vs := byRef[ref]
		e := audit.Event{Event: audit.EventDecision, Module: l.mod.Name(), Decision: res.Decision.String(),
			Code: vs[0].Code, Ref: ref, PolicyBlob: l.blob}
		for _, u := range r.updates {
			if u.Ref == ref {
				e.OldRev, e.NewRev = u.Old, u.New
			}
		}
		for _, v := range vs {
			av := audit.Violation{Rule: v.Rule, Code: v.Code, Reason: v.Reason}
			for _, it := range v.Items {
				if it.Commit {
					av.Commits = append(av.Commits, it.Name)
				} else {
					av.Paths = append(av.Paths, it.Name)
				}
			}
			e.Violations = append(e.Violations, av)
		}
		events = append(events, r.event(e))
	}
	return events
}
