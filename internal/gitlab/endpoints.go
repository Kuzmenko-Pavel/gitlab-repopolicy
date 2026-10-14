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

// ProjectAccess returns the access level of the token's user in the project:
// the higher of the project and the group membership (architecture §13.2).
// A project the token cannot see is NotFound.
func (c *Client) ProjectAccess(ctx context.Context, projectID int64) (int, error) {
	var p struct {
		Permissions struct {
			Project *struct {
				AccessLevel int `json:"access_level"`
			} `json:"project_access"`
			Group *struct {
				AccessLevel int `json:"access_level"`
			} `json:"group_access"`
		} `json:"permissions"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v4/projects/%d", projectID), nil, &p); err != nil {
		return 0, err
	}
	level := 0
	if p.Permissions.Project != nil {
		level = p.Permissions.Project.AccessLevel
	}
	if p.Permissions.Group != nil {
		level = max(level, p.Permissions.Group.AccessLevel)
	}
	return level, nil
}

// MaxGraphQLUsers is the number of usernames asked in one GraphQL request.
const MaxGraphQLUsers = 100

// GraphQLUser is a user returned by the users(usernames:) query.
type GraphQLUser struct {
	ID       int64
	Username string
	State    string
}

// UsersByUsernames looks usernames up with one GraphQL request per 100 names
// (architecture D28). Users that do not exist, and for a token without
// administrator rights blocked and banned users, are not in the result.
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

// MemberAccess returns the effective access level of a user in the project,
// inherited memberships included; 0 when the user is not a member.
func (c *Client) MemberAccess(ctx context.Context, projectID, userID int64) (int, error) {
	var m struct {
		AccessLevel int    `json:"access_level"`
		State       string `json:"state"`
	}
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v4/projects/%d/members/all/%d", projectID, userID), nil, &m)
	if KindOf(err) == NotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if m.State != "" && m.State != "active" {
		return 0, nil
	}
	return m.AccessLevel, nil
}

// MembersAccess returns the effective access levels of several users in one
// request; users that are not members are absent.
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
