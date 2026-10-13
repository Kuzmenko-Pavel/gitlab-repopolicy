// Package identity answers questions about the pusher and the project that
// need GitLab: the namespace type (architecture §13.2) and, for SSH pushes,
// whether the key is personal (§9.2).
package identity

import (
	"context"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
)

// Users looks users up by username.
type Users interface {
	UsersByUsername(ctx context.Context, username string) ([]gitlab.User, error)
}

// IsPersonalProject reports whether the project lives in a personal namespace,
// which the hook does not check (architecture D25). A path of three or more
// segments is a group project: users have no subgroups. For two segments the
// first one is looked up as a username; user and group paths do not overlap.
// A failed lookup, or a blocked or banned owner that the service account
// cannot see, leaves the project in the group branch: fail closed.
func IsPersonalProject(ctx context.Context, api Users, projectPath string) bool {
	segments := strings.Split(projectPath, "/")
	if len(segments) != 2 || segments[0] == "" {
		return false
	}
	users, err := api.UsersByUsername(ctx, segments[0])
	if err != nil {
		return false
	}
	for _, u := range users {
		if strings.EqualFold(u.Username, segments[0]) {
			return true
		}
	}
	return false
}
