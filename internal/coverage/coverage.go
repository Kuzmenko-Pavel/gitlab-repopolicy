// Package coverage finds the top-level groups where the service account is
// not a Maintainer (architecture §13.2). The service account cannot see these
// gaps itself, so an administrator runs it with their own token, which is not
// stored.
package coverage

import (
	"context"
	"fmt"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
)

// API is what coverage needs from GitLab with an administrator token.
type API interface {
	TopLevelGroups(ctx context.Context) ([]gitlab.Group, error)
	GroupMemberAccess(ctx context.Context, groupID, userID int64) (int, error)
	GroupProjects(ctx context.Context, groupID int64) ([]gitlab.Project, error)
	FileExists(ctx context.Context, projectID int64, path, ref string) (bool, error)
}

// maintainer is the access level the service account needs in every top-level
// group; same as module.AccessMaintainer, which coverage does not import.
const maintainer = 40

// Gap is a top-level group where the service account is not a Maintainer.
type Gap struct {
	Group gitlab.Group
	// Access is the access level of the service account, 0 when it is not a member.
	Access int
	// PolicyProjects are the projects with policy files on their default
	// branch: their protected changes are denied with project_not_visible.
	PolicyProjects []string
}

// Report of a run.
type Report struct {
	Groups int
	Gaps   []Gap
}

// Run checks every top-level group.
func Run(ctx context.Context, api API, serviceAccountID int64, policyFiles []string) (Report, error) {
	groups, err := api.TopLevelGroups(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("list groups: %w", err)
	}
	rep := Report{Groups: len(groups)}
	for _, g := range groups {
		level, err := api.GroupMemberAccess(ctx, g.ID, serviceAccountID)
		if err != nil {
			return rep, fmt.Errorf("group %s: %w", g.FullPath, err)
		}
		if level >= maintainer {
			continue
		}
		gap := Gap{Group: g, Access: level}
		projects, err := api.GroupProjects(ctx, g.ID)
		if err != nil {
			return rep, fmt.Errorf("group %s: %w", g.FullPath, err)
		}
		for _, p := range projects {
			if p.DefaultBranch == "" {
				continue
			}
			for _, f := range policyFiles {
				ok, err := api.FileExists(ctx, p.ID, f, p.DefaultBranch)
				if err != nil {
					return rep, fmt.Errorf("project %s: %w", p.PathWithNamespace, err)
				}
				if ok {
					gap.PolicyProjects = append(gap.PolicyProjects, p.PathWithNamespace)
					break
				}
			}
		}
		rep.Gaps = append(rep.Gaps, gap)
	}
	return rep, nil
}
