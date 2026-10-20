package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// maxBranchPages bounds the list of branches of a project: 10 000 branches.
const maxBranchPages = 100

// Branches returns the names of the branches of the project.
func (c *Client) Branches(ctx context.Context, projectID int64) ([]string, error) {
	items, err := pages[named](ctx, c, fmt.Sprintf("/api/v4/projects/%d/repository/branches", projectID), maxBranchPages)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	return names, nil
}

func (c *Client) names(ctx context.Context, path string) ([]string, error) {
	items, err := pages[named](ctx, c, path, maxPages)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	return names, nil
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

// ProjectAccess returns the access level of the token's user in the project:
// the higher of the project and the group membership.
// A project the token cannot see is NotFound.
func (c *Client) ProjectAccess(ctx context.Context, projectID int64) (int, error) {
	p, err := c.Project(ctx, strconv.FormatInt(projectID, 10))
	if err != nil {
		return 0, err
	}
	return p.Access(), nil
}

// MaxGraphQLUsers is the number of usernames asked in one GraphQL request.
const MaxGraphQLUsers = 100

// GraphQLUser is a user returned by the users(usernames:) query.
type GraphQLUser struct {
	ID       int64
	Username string
	State    string
}

// UsersByUsernames looks usernames up with one GraphQL request per 100 names.
// Users that do not exist, and for a token without administrator rights blocked
// and banned users, are not in the result.
func (c *Client) UsersByUsernames(ctx context.Context, usernames []string) ([]GraphQLUser, error) {
	var out []GraphQLUser
	for start := 0; start < len(usernames); start += MaxGraphQLUsers {
		chunk := usernames[start:min(start+MaxGraphQLUsers, len(usernames))]
		body := map[string]any{
			"query":     "query($u: [String!]!) { users(usernames: $u, first: 100) { nodes { id username state } } }",
			"variables": map[string]any{"u": chunk},
		}
		var resp struct {
			Data struct {
				Users struct {
					Nodes []struct {
						ID       string `json:"id"`
						Username string `json:"username"`
						State    string `json:"state"`
					} `json:"nodes"`
				} `json:"users"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := c.do(ctx, http.MethodPost, "/api/graphql", body, &resp); err != nil {
			return nil, err
		}
		if len(resp.Errors) > 0 {
			return nil, &Error{Kind: Unexpected, Path: "/api/graphql", Err: errors.New(resp.Errors[0].Message)}
		}
		for _, n := range resp.Data.Users.Nodes {
			id, err := globalID(n.ID, "User")
			if err != nil {
				return nil, &Error{Kind: Unexpected, Path: "/api/graphql", Err: err}
			}
			out = append(out, GraphQLUser{ID: id, Username: n.Username, State: n.State})
		}
	}
	return out, nil
}

// globalID parses "gid://gitlab/<kind>/<id>".
func globalID(gid, kind string) (int64, error) {
	rest, ok := strings.CutPrefix(gid, "gid://gitlab/"+kind+"/")
	id, err := strconv.ParseInt(rest, 10, 64)
	if !ok || err != nil || id <= 0 {
		return 0, fmt.Errorf("unexpected global ID %q", gid)
	}
	return id, nil
}

// MembersAccess returns the effective access levels of users in the project
// in one request, inherited memberships included; users that are not active
// members are absent.
func (c *Client) MembersAccess(ctx context.Context, projectID int64, userIDs []int64) (map[int64]int, error) {
	out := map[int64]int{}
	if len(userIDs) == 0 {
		return out, nil
	}
	q := url.Values{}
	for _, id := range userIDs {
		q.Add("user_ids[]", strconv.FormatInt(id, 10))
	}
	q.Set("per_page", strconv.Itoa(perPage))
	var members []struct {
		ID          int64  `json:"id"`
		AccessLevel int    `json:"access_level"`
		State       string `json:"state"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v4/projects/%d/members/all?%s", projectID, q.Encode()), nil, &members); err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.State == "" || m.State == "active" {
			out[m.ID] = max(out[m.ID], m.AccessLevel)
		}
	}
	return out, nil
}
