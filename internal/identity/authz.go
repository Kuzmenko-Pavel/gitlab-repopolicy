package identity

import (
	"context"
	"strings"
	"sync"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/refs"
)

// AuthzAPI is the part of the GitLab client the authorizer uses.
type AuthzAPI interface {
	UsersByUsernames(ctx context.Context, usernames []string) ([]gitlab.GraphQLUser, error)
	MemberAccess(ctx context.Context, projectID, userID int64) (int, error)
	MembersAccess(ctx context.Context, projectID int64, userIDs []int64) (map[int64]int, error)
}

// Authorizer implements module.Authorizer and module.Validator for one receive.
// Answers are memoised: within a receive the same question is asked once.
type Authorizer struct {
	api       AuthzAPI
	projectID int64
	pusher    module.PrincipalResolver
	guard     module.Guard
	known     map[string]bool

	mu     sync.Mutex
	users  map[string]module.User
	levels map[int64]int
}

// NewAuthorizer returns the authorizer of a project.
func NewAuthorizer(api AuthzAPI, projectID int64, pusher module.PrincipalResolver, guard module.Guard) *Authorizer {
	return &Authorizer{api: api, projectID: projectID, pusher: pusher, guard: guard,
		users: map[string]module.User{}, levels: map[int64]int{}}
}

// WithKnown returns a copy that reports the usernames as present on the anchor (D28).
func (a *Authorizer) WithKnown(usernames []string) *Authorizer {
	known := map[string]bool{}
	for _, u := range usernames {
		known[strings.ToLower(u)] = true
	}
	return &Authorizer{api: a.api, projectID: a.projectID, pusher: a.pusher, guard: a.guard, known: known,
		users: a.users, levels: a.levels}
}

// Known reports whether the username is in the anchor version of the policy.
func (a *Authorizer) Known(username string) bool { return a.known[strings.ToLower(username)] }

func (a *Authorizer) stop(ctx context.Context) (module.Status, bool) {
	if a.guard == nil {
		return module.Confirmed(), false
	}
	return a.guard(ctx)
}

// PusherAccess is the effective access level of the pushing person in the project.
func (a *Authorizer) PusherAccess(ctx context.Context) (int, module.Status) {
	person, status := a.pusher.Person(ctx)
	if !status.IsConfirmed() {
		return 0, status
	}
	if person.ID == 0 {
		return 0, module.Confirmed()
	}
	levels, status := a.AccessLevels(ctx, []int64{person.ID})
	return levels[person.ID], status
}

// Users looks usernames up with GraphQL, once per username within the receive.
func (a *Authorizer) Users(ctx context.Context, usernames []string) (map[string]module.User, module.Status) {
	out := map[string]module.User{}
	var ask []string
	a.mu.Lock()
	for _, name := range usernames {
		key := strings.ToLower(name)
		if u, ok := a.users[key]; ok {
			if u.ID != 0 {
				out[key] = u
			}
			continue
		}
		ask = append(ask, name)
	}
	a.mu.Unlock()
	if len(ask) == 0 {
		return out, module.Confirmed()
	}
	if st, stop := a.stop(ctx); stop {
		return nil, st
	}
	found, err := a.api.UsersByUsernames(ctx, ask)
	if err != nil {
		return nil, refs.StatusOf(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, name := range ask {
		a.users[strings.ToLower(name)] = module.User{}
	}
	for _, u := range found {
		key := strings.ToLower(u.Username)
		a.users[key] = module.User{ID: u.ID, Active: u.State == "active"}
	}
	for _, name := range ask {
		if u := a.users[strings.ToLower(name)]; u.ID != 0 {
			out[strings.ToLower(name)] = u
		}
	}
	return out, module.Confirmed()
}

// AccessLevels returns the effective access levels of users in the project.
func (a *Authorizer) AccessLevels(ctx context.Context, userIDs []int64) (map[int64]int, module.Status) {
	out := map[int64]int{}
	var ask []int64
	a.mu.Lock()
	for _, id := range userIDs {
		if level, ok := a.levels[id]; ok {
			out[id] = level
		} else {
			ask = append(ask, id)
		}
	}
	a.mu.Unlock()
	if len(ask) == 0 {
		return out, module.Confirmed()
	}
	if st, stop := a.stop(ctx); stop {
		return nil, st
	}
	var levels map[int64]int
	var err error
	if len(ask) == 1 {
		var level int
		level, err = a.api.MemberAccess(ctx, a.projectID, ask[0])
		levels = map[int64]int{ask[0]: level}
	} else {
		levels, err = a.api.MembersAccess(ctx, a.projectID, ask)
	}
	if err != nil {
		return nil, refs.StatusOf(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ask {
		a.levels[id] = levels[id]
		out[id] = levels[id]
	}
	return out, module.Confirmed()
}
