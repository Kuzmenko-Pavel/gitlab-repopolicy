package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/changes"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/refs"
)

func (r *receive) newInput() *module.Input {
	resolver := identity.NewResolver(r.deps.API, r.req.Env.Principal, r.guard)
	r.authz = identity.NewAuthorizer(r.deps.API, r.req.Env.ProjectID, resolver, r.guard)
	var present []string
	for _, l := range r.policies {
		if l.present {
			present = append(present, l.mod.File())
		}
	}
	// HEAD of an empty repository names a branch that does not exist yet:
	// git cannot exclude it, and there is nothing to exclude.
	existingDefault := r.defaultRef
	if r.anchor == "" {
		existingDefault = ""
	}
	return &module.Input{
		Anchor:         r.anchor,
		Updates:        r.updates,
		Refs:           refs.New(r.defaultRef, r.req.Env.ProjectID, r.deps.API, r.guard),
		Changes:        changes.New(r.deps.Git, existingDefault),
		Principal:      r.req.Env.Principal,
		Protocol:       r.req.Env.Protocol,
		Principals:     resolver,
		Objects:        objects{git: r.deps.Git},
		Clock:          r.deps.Clock,
		PolicyFiles:    r.policyFiles(),
		AnchorPolicies: present,
	}
}

// inputFor gives the authorizer only to modules that declare identity.
func (r *receive) inputFor(identityNeeded bool) *module.Input {
	if !identityNeeded {
		return r.input
	}
	in := *r.input
	in.Authz = r.authz
	return &in
}

// guard runs before the first API call of a receive. A personal project is
// not checked, so no question about it goes to GitLab; a project the service
// account cannot see, or sees below Maintainer, answers every question with
// unknown, as an API failure does.
func (r *receive) guard(ctx context.Context) (module.Status, bool) {
	if r.isPersonal(ctx) {
		return module.Unknown("personal namespace"), true
	}
	if r.serviceAccount == nil {
		st := r.checkServiceAccount(ctx)
		r.serviceAccount = &st
	}
	if !r.serviceAccount.IsConfirmed() {
		return *r.serviceAccount, true
	}
	return module.Confirmed(), false
}

func (r *receive) checkServiceAccount(ctx context.Context) module.Status {
	level, err := r.deps.API.ProjectAccess(ctx, r.req.Env.ProjectID)
	reason := ""
	switch {
	case gitlab.KindOf(err) == gitlab.NotFound:
		reason = "project_not_visible"
	case err != nil:
		return refs.StatusOf(err)
	case level < module.AccessMaintainer:
		reason = "role_below_maintainer"
	default:
		return module.Confirmed()
	}
	r.events = append(r.events, r.event(audit.Event{Event: audit.EventAuthorizationBackendFailure,
		Code: module.CodeAuthorizationBackend, Reason: reason}))
	return module.UnknownSetup(reason)
}

// objects adapts gitx to module.ObjectReader.
type objects struct{ git *gitx.Repo }

func (o objects) Read(ctx context.Context, oid string, max int) (module.Object, error) {
	info, data, err := o.git.Read(ctx, oid, int64(max))
	switch {
	case errors.Is(err, gitx.ErrMissing):
		return module.Object{}, fmt.Errorf("%s: %w", oid, module.ErrMissing)
	case errors.Is(err, gitx.ErrTooLarge):
		return module.Object{}, fmt.Errorf("%s: %w", oid, module.ErrTooLarge)
	case err != nil:
		return module.Object{}, err
	}
	return module.Object{OID: info.OID, Type: info.Type, Data: data}, nil
}
