package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Endpoints of the operational commands: selftest, token rotate,
// settings-check, coverage, explain and admins.

// TokenUser is the owner of the token as GET /user returns it.
type TokenUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	State    string `json:"state"`
	Bot      bool   `json:"bot"`
	External bool   `json:"external"`
	// IsAdmin is present only for administrators.
	IsAdmin bool `json:"is_admin"`
}

// CurrentUser returns the owner of the token.
func (c *Client) CurrentUser(ctx context.Context) (TokenUser, error) {
	var u TokenUser
	err := c.do(ctx, http.MethodGet, "/api/v4/user", nil, &u)
	return u, err
}

// PersonalToken is a personal access token as GitLab describes it.
type PersonalToken struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Scopes  []string `json:"scopes"`
	Active  bool     `json:"active"`
	Revoked bool     `json:"revoked"`
	// ExpiresAt is a date, YYYY-MM-DD; empty for a token without expiry.
	ExpiresAt string `json:"expires_at"`
	UserID    int64  `json:"user_id"`
}

// TokenSelf describes the token of the client.
func (c *Client) TokenSelf(ctx context.Context) (PersonalToken, error) {
	var t PersonalToken
	err := c.do(ctx, http.MethodGet, "/api/v4/personal_access_tokens/self", nil, &t)
	return t, err
}

// RotateToken replaces the token of the client with a new one that expires
// on the date (YYYY-MM-DD). GitLab revokes the old token at once.
func (c *Client) RotateToken(ctx context.Context, expiresAt string) (string, PersonalToken, error) {
	var resp struct {
		PersonalToken
		Token string `json:"token"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v4/personal_access_tokens/self/rotate", map[string]string{"expires_at": expiresAt}, &resp)
	if err == nil && resp.Token == "" {
		err = &Error{Kind: Unexpected, Path: "/api/v4/personal_access_tokens/self/rotate", Err: errors.New("no token in the response")}
	}
	return resp.Token, resp.PersonalToken, err
}

// Project is a project with the settings that settings-check compares.
type Project struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	Archived          bool   `json:"archived"`
	// CIConfigPath and JobTokenPush are present only for members; a
	// ci_config_path that was never set is null.
	CIConfigPath Field[string] `json:"ci_config_path"`
	JobTokenPush Field[bool]   `json:"ci_push_repository_for_job_token_allowed"`
	Namespace    struct {
		Kind string `json:"kind"`
	} `json:"namespace"`
	Permissions struct {
		Project *struct {
			AccessLevel int `json:"access_level"`
		} `json:"project_access"`
		Group *struct {
			AccessLevel int `json:"access_level"`
		} `json:"group_access"`
	} `json:"permissions"`
}

// Field is a JSON field that may be left out: GitLab omits the settings of a
// project that the token may not see. Null is present with the zero value.
type Field[T any] struct {
	Present bool
	Value   T
}

// UnmarshalJSON runs only for a field that is in the response.
func (f *Field[T]) UnmarshalJSON(data []byte) error {
	f.Present = true
	if string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, &f.Value)
}

// Access is the access level of the token's user: the higher of the project and the group membership.
func (p Project) Access() int {
	level := 0
	if p.Permissions.Project != nil {
		level = p.Permissions.Project.AccessLevel
	}
	if p.Permissions.Group != nil {
		level = max(level, p.Permissions.Group.AccessLevel)
	}
	return level
}

// Project returns a project by ID or by full path.
func (c *Client) Project(ctx context.Context, idOrPath string) (Project, error) {
	var p Project
	err := c.do(ctx, http.MethodGet, "/api/v4/projects/"+url.PathEscape(idOrPath), nil, &p)
	return p, err
}

// maxListPages bounds the lists of the operational commands: 100 000 projects or groups.
const maxListPages = 1000

// MemberProjects returns the projects the token's user is a member of,
// archived ones excluded, in ID order (keyset pagination).
func (c *Client) MemberProjects(ctx context.Context) ([]Project, error) {
	var out []Project
	after := int64(0)
	for range maxListPages {
		var page []Project
		path := fmt.Sprintf("/api/v4/projects?membership=true&archived=false&order_by=id&sort=asc&per_page=%d&id_after=%d", perPage, after)
		if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < perPage {
			return out, nil
		}
		after = page[len(page)-1].ID
	}
	return nil, &Error{Kind: Unexpected, Path: "/api/v4/projects", Err: errors.New("too many pages")}
}

// RawFile returns a file of the repository at ref; a missing file is NotFound.
func (c *Client) RawFile(ctx context.Context, projectID int64, path, ref string, limit int) ([]byte, error) {
	var data []byte
	p := fmt.Sprintf("/api/v4/projects/%d/repository/files/%s/raw?ref=%s", projectID, url.PathEscape(path), url.QueryEscape(ref))
	if err := c.do(ctx, http.MethodGet, p, nil, &data); err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, &Error{Kind: Unexpected, Path: p, Err: fmt.Errorf("larger than %d bytes", limit)}
	}
	return data, nil
}

// FileExists reports whether a file exists in the repository at ref.
func (c *Client) FileExists(ctx context.Context, projectID int64, path, ref string) (bool, error) {
	p := fmt.Sprintf("/api/v4/projects/%d/repository/files/%s?ref=%s", projectID, url.PathEscape(path), url.QueryEscape(ref))
	err := c.do(ctx, http.MethodHead, p, nil, nil)
	if KindOf(err) == NotFound {
		return false, nil
	}
	return err == nil, err
}

// Group is a group as the groups API returns it.
type Group struct {
	ID       int64  `json:"id"`
	FullPath string `json:"full_path"`
}

// TopLevelGroups returns every top-level group the token can see; for an
// administrator these are all groups of the instance.
func (c *Client) TopLevelGroups(ctx context.Context) ([]Group, error) {
	return pages[Group](ctx, c, "/api/v4/groups?top_level_only=true&all_available=true&order_by=id&sort=asc", maxListPages)
}

// GroupProjects returns the projects of a group and its subgroups, archived ones excluded.
func (c *Client) GroupProjects(ctx context.Context, groupID int64) ([]Project, error) {
	return pages[Project](ctx, c, fmt.Sprintf("/api/v4/groups/%d/projects?include_subgroups=true&archived=false&order_by=id&sort=asc", groupID), maxListPages)
}

// GroupMemberAccess returns the access level of a direct member of the group; 0 when the user is not one.
func (c *Client) GroupMemberAccess(ctx context.Context, groupID, userID int64) (int, error) {
	var m struct {
		AccessLevel int    `json:"access_level"`
		State       string `json:"state"`
	}
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v4/groups/%d/members/%d", groupID, userID), nil, &m)
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

// pages reads an offset-paginated list of at most limit pages.
func pages[T any](ctx context.Context, c *Client, path string, limit int) ([]T, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var out []T
	for page := 1; page <= limit; page++ {
		var items []T
		if err := c.do(ctx, http.MethodGet, path+sep+"per_page="+strconv.Itoa(perPage)+"&page="+strconv.Itoa(page), nil, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) < perPage {
			return out, nil
		}
	}
	return nil, &Error{Kind: Unexpected, Path: path, Err: errors.New("too many pages")}
}
