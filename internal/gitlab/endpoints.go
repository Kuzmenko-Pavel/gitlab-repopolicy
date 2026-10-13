package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const (
	perPage = 100
	// maxPages bounds paginated lists; 1000 protected patterns are far beyond real projects.
	maxPages = 10
)

type named struct {
	Name string `json:"name"`
}

// ProtectedBranches returns the name patterns of the protected branches of the project.
func (c *Client) ProtectedBranches(ctx context.Context, projectID int64) ([]string, error) {
	return c.names(ctx, fmt.Sprintf("/api/v4/projects/%d/protected_branches", projectID))
}

// ProtectedTags returns the name patterns of the protected tags of the project.
func (c *Client) ProtectedTags(ctx context.Context, projectID int64) ([]string, error) {
	return c.names(ctx, fmt.Sprintf("/api/v4/projects/%d/protected_tags", projectID))
}

func (c *Client) names(ctx context.Context, path string) ([]string, error) {
	var names []string
	for page := 1; page <= maxPages; page++ {
		var items []named
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=%d&page=%d", path, perPage, page), nil, &items); err != nil {
			return nil, err
		}
		for _, it := range items {
			names = append(names, it.Name)
		}
		if len(items) < perPage {
			return names, nil
		}
	}
	return nil, &Error{Kind: Unexpected, Path: path, Err: errors.New("too many pages")}
}

// User is a GitLab user as the users API returns it.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	State    string `json:"state"`
}

// UsersByUsername looks a username up with GET /users?username=. Without
// administrator rights blocked and banned users are not returned.
func (c *Client) UsersByUsername(ctx context.Context, username string) ([]User, error) {
	var users []User
	err := c.do(ctx, http.MethodGet, "/api/v4/users?username="+url.QueryEscape(username), nil, &users)
	return users, err
}

// UserHasKey reports whether the SSH key keyID belongs to the user's own keys:
// GET /users/:user_id/keys/:key_id is 200 for a personal key and 404 for a deploy key.
func (c *Client) UserHasKey(ctx context.Context, userID, keyID int64) (bool, error) {
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v4/users/%d/keys/%d", userID, keyID), nil, nil)
	if KindOf(err) == NotFound {
		return false, nil
	}
	return err == nil, err
}
