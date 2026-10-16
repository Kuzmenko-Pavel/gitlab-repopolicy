package identity

import (
	"context"
	"strings"
	"sync"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/refs"
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
	guard     module.Guard

	once   sync.Once
	person module.Person
	status module.Status
}

// NewResolver returns a resolver of the pusher.
func NewResolver(api API, p module.Principal, guard module.Guard) *Resolver {
	return &Resolver{api: api, principal: p, guard: guard}
}

// Person returns the pushing person: GL_USERNAME and the ID of user-<id>; for
// key-<id> only if the key is one of the user's own keys. A deploy key or an
// unparsable GL_ID gives a zero person (unknown_principal). A failed lookup is unknown.
func (r *Resolver) Person(ctx context.Context) (module.Person, module.Status) {
	r.once.Do(func() { r.person, r.status = r.resolve(ctx) })
	return r.person, r.status
}

func (r *Resolver) resolve(ctx context.Context) (module.Person, module.Status) {
	kind, id := r.principal.Kind()
	switch kind {
	case module.PrincipalUser:
		return module.Person{Username: r.principal.Username, ID: id}, module.Confirmed()
	case module.PrincipalKey:
	default:
		return module.Person{}, module.Confirmed()
	}
	keyID := id
	if r.guard != nil {
		if st, stop := r.guard(ctx); stop {
			return module.Person{}, st
		}
	}
	users, err := r.api.UsersByUsername(ctx, r.principal.Username)
	if err != nil {
		return module.Person{}, refs.StatusOf(err)
	}
	var userID int64
	for _, u := range users {
		if strings.EqualFold(u.Username, r.principal.Username) {
			userID = u.ID
		}
	}
	if userID == 0 {
		// The owner of the key is not visible: blocked, banned or deleted.
		return module.Person{}, module.Confirmed()
	}
	personal, err := r.api.UserHasKey(ctx, userID, keyID)
	if err != nil {
		return module.Person{}, refs.StatusOf(err)
	}
	if !personal {
		return module.Person{}, module.Confirmed()
	}
	return module.Person{Username: r.principal.Username, ID: userID}, module.Confirmed()
}
