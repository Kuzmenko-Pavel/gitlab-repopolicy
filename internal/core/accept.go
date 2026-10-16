package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// accept validates every policy file that a push changes, on any ref, as a new
// version. It runs whether or not the anchor has policies.
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
	nowEntries, err := r.entries(ctx, newC)
	if err != nil {
		return nil, err
	}
	wasEntries := map[string]gitx.Info{}
	if base != "" {
		if wasEntries, err = r.entries(ctx, base); err != nil {
			return nil, err
		}
	}
	changed := map[string]bool{}
	for _, l := range r.policies {
		f := l.mod.File()
		now, nowOK := nowEntries[f]
		was, wasOK := wasEntries[f]
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
				Title: "the changed " + l.mod.File() + " has warnings",
				Items: []module.Item{{Name: l.mod.File(), Details: warnings}},
			})
		}
	}
	switch {
	case len(problems) > 0:
		r.addSection(l.mod.File(), changedInvalid(l.mod.File(), code, u.Ref, problems))
		r.events = append(r.events, r.event(audit.Event{Event: audit.EventDecision, Module: l.mod.Name(), Decision: "deny",
			Code: code, Ref: u.Ref, OldRev: u.Old, NewRev: u.New, PolicyBlob: f.blob,
			Violations: []audit.Violation{{Code: code, Paths: []string{l.mod.File()}, Reason: strings.Join(problems, "; ")}}}))
	case unknown:
		r.addSection(l.mod.File(), module.Violation{Code: module.CodeAuthorizationBackend, Ref: u.Ref,
			Title: "could not check the changed " + l.mod.File() + ": the GitLab API did not answer",
			Facts: []string{"this is a problem of the server, not of your change"}, Hint: "retry in a few minutes",
			Summary: "could not check the changed " + l.mod.File() + ": the GitLab API did not answer; not caused by your change, retry in a few minutes"})
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

// changedInvalid is the denial of a new version of a policy file: the
// problems with their lines, and how to check the file before a push.
func changedInvalid(file, code, ref string, problems []string) module.Violation {
	v := module.Violation{Code: code, Ref: ref, Title: "the changed " + file + " is invalid",
		Items:   []module.Item{{Name: file, Details: problems}},
		Hint:    "every changed policy file is checked, on any branch\ncheck it before a push: gitlab-repopolicy validate -f " + file,
		Summary: "the changed " + file + " is invalid: " + problems[0]}
	if code == module.CodeLockoutPrevented {
		v.Title = ""
		v.Hint = "fix the admins and push again; check them before a push: gitlab-repopolicy validate --online -f " + file
	}
	for _, p := range problems {
		if strings.Contains(p, "private key") {
			v.Hint += "\nthe commit with the private key stays in the history of your branch: remove it with git rebase -i and push it nowhere"
			break
		}
	}
	return v
}

// validatorFor is the authorizer that knows the usernames of the anchor version.
func (r *receive) validatorFor(l *loaded) module.Validator {
	var known []string
	if lister, ok := l.policy.(module.UsernameLister); ok {
		known = lister.Usernames()
	}
	return r.authz.WithKnown(known)
}

// coverageWarning is the warning for a new version of a delegated policy file
// that the PATHPOLICY on the anchor does not protect: the project has none, or
// no rule covers the file.
func (r *receive) coverageWarning(l *loaded) string {
	root := r.root
	switch {
	case root == nil || root == l:
		return ""
	case !root.present:
		return fmt.Sprintf("the project has no %s on the default branch: anyone who can push may change %s", root.mod.File(), l.mod.File())
	case root.policy == nil || r.deps.Root.Covers(root.policy, l.mod.File()):
		return ""
	}
	return fmt.Sprintf("no %s rule covers %s: anyone who can push may change it", root.mod.File(), l.mod.File())
}
