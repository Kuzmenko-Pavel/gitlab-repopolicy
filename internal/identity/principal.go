package identity

import (
	"context"
	"strings"
	"sync"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// API is the part of the GitLab client principal resolution uses.
type API interface {
	Users
	UserHasKey(ctx context.Context, userID, keyID int64) (bool, error)
}

// Resolver turns GL_ID and GL_USERNAME into the username that policies match,
// once per receive (architecture §9.2).
type Resolver struct {
	api       API
	principal module.Principal
	// guard runs before the first API call; a true result stops it.
	guard func(ctx context.Context) (module.Status, bool)

	once     sync.Once
	username string
	status   module.Status
}

// NewResolver returns a resolver of the pusher.
func NewResolver(api API, p module.Principal, guard func(ctx context.Context) (module.Status, bool)) *Resolver {
	return &Resolver{api: api, principal: p, guard: guard}
}

// Username returns the username of a person: GL_USERNAME for user-<id>; for
// key-<id> only if the key is one of the user's own keys. A deploy key or an
// unparsable GL_ID gives "" (unknown_principal). A failed lookup is unknown.
func (r *Resolver) Username(ctx context.Context) (string, module.Status) {
	r.once.Do(func() { r.username, r.status = r.resolve(ctx) })
	return r.username, r.status
}

func (r *Resolver) resolve(ctx context.Context) (string, module.Status) {
	kind, keyID := r.principal.Kind()
	switch kind {
	case module.PrincipalUser:
		return r.principal.Username, module.Confirmed()
	case module.PrincipalKey:
	default:
		return "", module.Confirmed()
	}
	if r.guard != nil {
		if st, stop := r.guard(ctx); stop {
			return "", st
		}
	}
	users, err := r.api.UsersByUsername(ctx, r.principal.Username)
	if err != nil {
		return "", statusOf(err)
	}
	var userID int64
	for _, u := range users {
		if strings.EqualFold(u.Username, r.principal.Username) {
			userID = u.ID
		}
	}
	if userID == 0 {
		// The owner of the key is not visible: blocked, banned or deleted.
		return "", module.Confirmed()
	}
	personal, err := r.api.UserHasKey(ctx, userID, keyID)
	if err != nil {
		return "", statusOf(err)
	}
	if !personal {
		return "", module.Confirmed()
	}
	return r.principal.Username, module.Confirmed()
}

func statusOf(err error) module.Status {
	switch gitlab.KindOf(err) {
	case gitlab.BudgetExceeded, gitlab.Deadline:
		return module.UnknownLimit(err.Error())
	default:
		return module.Unknown(err.Error())
	}
}
