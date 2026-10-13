package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/changes"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/refs"
)

func (r *receive) newInput() *module.Input {
	return &module.Input{
		Anchor:      r.anchor,
		Updates:     r.updates,
		Refs:        refs.New(r.defaultRef, r.req.Env.ProjectID, r.deps.API, r.guard),
		Changes:     changes.New(r.deps.Git, r.defaultRef),
		Principal:   r.req.Env.Principal,
		Principals:  identity.NewResolver(r.deps.API, r.req.Env.Principal, r.guard),
		Objects:     objects{git: r.deps.Git},
		Clock:       r.deps.Clock,
		PolicyFiles: r.policyFiles(),
	}
}

// guard runs before the first API call of the classifier: a personal project
// is not checked, so no question about it goes to GitLab (architecture §13.2).
func (r *receive) guard(ctx context.Context) (module.Status, bool) {
	if r.isPersonal(ctx) {
		return module.Unknown("personal namespace"), true
	}
	return module.Confirmed(), false
}

// objects adapts gitx to module.ObjectReader.
type objects struct{ git Git }

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
