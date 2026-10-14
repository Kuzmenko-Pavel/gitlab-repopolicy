package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// accept validates every policy file that a push changes, on any ref, as a new
// version (architecture §12.2, D23). It runs whether or not the anchor has policies.
func (r *receive) accept(ctx context.Context) error {
	for _, u := range r.updates {
		if u.Kind() == module.Delete {
			continue
		}
		changed, err := r.changedFiles(ctx, u)
		if errors.Is(err, module.ErrNotCommit) {
			continue
		}
		if err != nil {
			return err
		}
		for _, l := range r.policies {
			if changed[l.mod.File()] {
				if err := r.acceptFile(ctx, u, l); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// changedFiles reports which policy files the update changes: a file or
// anything under its path among the candidate paths of the ref. For a push too
// large to list paths it compares the file at both ends instead.
func (r *receive) changedFiles(ctx context.Context, u module.RefUpdate) (map[string]bool, error) {
	changed := map[string]bool{}
	paths, err := r.input.Changes.CandidatePaths(ctx, u)
	if errors.Is(err, module.ErrLimitExceeded) {
		return r.changedByEnds(ctx, u)
	}
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		for _, l := range r.policies {
			f := l.mod.File()
			if p == f || strings.HasPrefix(p, f+"/") {
				changed[f] = true
			}
		}
	}
	return changed, nil
}

func (r *receive) changedByEnds(ctx context.Context, u module.RefUpdate) (map[string]bool, error) {
	newC, err := r.deps.Git.Peel(ctx, u.New)
	if err != nil {
		return nil, err
	}
	base := r.anchor
	if u.Kind() == module.Update {
		if base, err = r.deps.Git.Peel(ctx, u.Old); err != nil {
			return nil, err
		}
	}
	changed := map[string]bool{}
	for _, l := range r.policies {
		f := l.mod.File()
		now, nowOK, err := r.deps.Git.Entry(ctx, newC, f)
		if err != nil {
			return nil, err
		}
		if base == "" {
			changed[f] = nowOK
			continue
		}
		was, wasOK, err := r.deps.Git.Entry(ctx, base, f)
		if err != nil {
			return nil, err
		}
		changed[f] = nowOK != wasOK || now.OID != was.OID || now.Mode != was.Mode
	}
	return changed, nil
}

// acceptFile validates <new>:<file> of one update.
func (r *receive) acceptFile(ctx context.Context, u module.RefUpdate, l *loaded) error {
	newC, err := r.deps.Git.Peel(ctx, u.New)
	if err != nil {
		return err
	}
	f, err := r.readPolicy(ctx, newC, l.mod)
	if err != nil {
		return err
	}
	base := audit.Event{Event: audit.EventPolicyChange, Module: l.mod.Name(), Ref: u.Ref, OldRev: u.Old, NewRev: u.New, PolicyBlob: f.blob}
	r.input.PolicyChanges = append(r.input.PolicyChanges, module.PolicyChange{Ref: u.Ref, File: l.mod.File(), Deleted: !f.present})
	if !f.present {
		// Removal is not a new version; who may remove it is up to the modules.
		base.Action = "delete"
		r.events = append(r.events, r.event(base))
		return nil
	}
	var problems []string
	code := module.CodeInvalidPolicyUpdate
	unknown := false
	if f.invalid != nil {
		problems = append(problems, f.invalid.Error())
	} else if p, err := parse(l.mod, f.data); err != nil {
		problems = append(problems, err.Error())
	} else {
		var warnings []string
		found := p.Validate(ctx, r.validatorFor(l))
		if vv, ok := p.(module.VersionValidator); ok {
			found = append(found, vv.ValidateVersion(l.policy, r.deps.Clock())...)
		}
		for _, pr := range found {
			switch {
			case pr.Unknown:
				unknown = true
			case pr.Warning:
				warnings = append(warnings, pr.Msg)
			default:
				problems = append(problems, pr.Msg)
				if pr.Code != "" {
					code = pr.Code
				}
			}
		}
		if w := r.coverageWarning(l); w != "" {
			warnings = append(warnings, w)
		}
		if len(warnings) > 0 && len(problems) == 0 && !unknown {
			r.addWarning(l.mod.File(), module.Violation{
				Code: module.CodeInvalidPolicyUpdate, Ref: u.Ref,
				Items: []module.Item{{Name: l.mod.File(), Details: warnings}},
			})
		}
	}
	switch {
	case len(problems) > 0:
		v := module.Violation{Code: code, Ref: u.Ref,
			Message: "Fix the file before pushing; the hook validates every new version on any branch.",
			Items:   []module.Item{{Name: l.mod.File(), Details: problems}}}
		r.addSection(l.mod.File(), v)
		r.events = append(r.events, r.event(audit.Event{Event: audit.EventDecision, Module: l.mod.Name(), Decision: "deny",
			Code: code, Ref: u.Ref, OldRev: u.Old, NewRev: u.New, PolicyBlob: f.blob,
			Violations: []audit.Violation{{Code: code, Paths: []string{l.mod.File()}, Reason: strings.Join(problems, "; ")}}}))
	case unknown:
		v := module.Violation{Code: module.CodeAuthorizationBackend, Ref: u.Ref, Message: coreMessage(module.CodeAuthorizationBackend, 0),
			Items: []module.Item{{Name: l.mod.File()}}}
		r.addSection(l.mod.File(), v)
		r.events = append(r.events, r.event(audit.Event{Event: audit.EventAuthorizationBackendFailure, Module: l.mod.Name(), Decision: "deny",
			Code: module.CodeAuthorizationBackend, Ref: u.Ref, OldRev: u.Old, NewRev: u.New, PolicyBlob: f.blob}))
	default:
		base.Action = "update"
		if !l.present {
			base.Action = "create"
		}
		r.events = append(r.events, r.event(base))
	}
	return nil
}

// validatorFor is the authorizer that knows the usernames of the anchor version (D28).
func (r *receive) validatorFor(l *loaded) module.Validator {
	var known []string
	if lister, ok := l.policy.(module.UsernameLister); ok {
		known = lister.Usernames()
	}
	return r.authz.WithKnown(known)
}

// coverageWarning is the warning for a new version of a delegated policy file
// that no rule of the PATHPOLICY on the anchor covers (architecture §12.2).
func (r *receive) coverageWarning(l *loaded) string {
	for _, other := range r.policies {
		if other == l || other.policy == nil {
			continue
		}
		if c, ok := other.policy.(module.PathCoverer); ok && !c.Covers(l.mod.File()) {
			return fmt.Sprintf("no %s rule covers %s: anyone who can push may change it", other.mod.File(), l.mod.File())
		}
	}
	return ""
}
