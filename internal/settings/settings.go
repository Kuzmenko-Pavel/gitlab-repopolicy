// Package settings implements settings-check: it compares the settings of
// projects with PATHPOLICY or BRANCHPOLICY against the expectations of the
// server config and of BRANCHPOLICY, and checks that the principals of their
// policies still exist.
// Settings live in the database of Rails, the hook never sees them change, so
// this is detection, not prevention.
package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// API is the part of the GitLab client that settings-check uses.
type API interface {
	MemberProjects(ctx context.Context) ([]gitlab.Project, error)
	RawFile(ctx context.Context, projectID int64, path, ref string, limit int) ([]byte, error)
	ProtectedBranches(ctx context.Context, projectID int64) ([]string, error)
	Branches(ctx context.Context, projectID int64) ([]string, error)
	ProtectedTags(ctx context.Context, projectID int64) ([]string, error)
	UsersByUsernames(ctx context.Context, usernames []string) ([]gitlab.GraphQLUser, error)
}

// Checks of a project; the names appear in output and in audit violations.
const (
	CheckAccess        = "access"
	CheckDefaultBranch = "default_branch"
	CheckCIConfigPath  = "ci_config_path"
	CheckProtected     = "protected_branches"
	CheckProtectedTags = "protected_tags"
	CheckJobTokenPush  = "job_token_push"
	CheckPrincipals    = "principals"
	CheckPolicy        = "policy"
	// CheckUnavailable: GitLab did not answer, so the project is not verified.
	CheckUnavailable = "unavailable"
)

// Drift is one difference found in a project.
type Drift struct {
	Check  string
	Reason string
}

// Project is the result of one project with PATHPOLICY.
type Project struct {
	ID     int64
	Path   string
	Drifts []Drift
}

// Report of a run.
type Report struct {
	// Checked are the projects with PATHPOLICY, in ID order.
	Checked []Project
	// Skipped is the number of member projects without PATHPOLICY or in personal namespaces.
	Skipped int
}

// Drifted returns the projects with drifts.
func (r Report) Drifted() []Project {
	var out []Project
	for _, p := range r.Checked {
		if len(p.Drifts) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// Options of a run.
type Options struct {
	Expected config.SettingsCheck
	// DefaultBranches are the allowed names of the default branch; empty is not checked.
	DefaultBranches []string
	// Guard is the PATHPOLICY module, nil when it is disabled; Modules are
	// all enabled modules, whose files are read for presets, principals and
	// the protection BRANCHPOLICY requires.
	Guard   module.Root
	Modules []module.Module
}

// Run checks every project the service account is a member of, one after
// another. Only a failure
// to list the projects is an error; a project GitLab does not answer for is a
// drift of the check "unavailable", never a silent pass.
func Run(ctx context.Context, api API, o Options) (Report, error) {
	if len(o.Modules) == 0 {
		return Report{}, errors.New("every policy module is disabled")
	}
	projects, err := api.MemberProjects(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("list projects: %w", err)
	}
	var rep Report
	for _, p := range projects {
		r := check(ctx, api, o, p)
		if r == nil {
			rep.Skipped++
			continue
		}
		rep.Checked = append(rep.Checked, *r)
	}
	return rep, nil
}

// check returns nil for a project that settings-check does not cover.
func check(ctx context.Context, api API, o Options, p gitlab.Project) *Project {
	if p.Namespace.Kind == "user" || p.Archived {
		return nil
	}
	res := &Project{ID: p.ID, Path: p.PathWithNamespace}
	drift := func(c, format string, a ...any) {
		res.Drifts = append(res.Drifts, Drift{Check: c, Reason: fmt.Sprintf(format, a...)})
	}
	unavailable := func(what string, err error) *Project {
		drift(CheckUnavailable, "%s: %v", what, err)
		return res
	}
	guard, ref, found, err := guardPolicy(ctx, api, o, p)
	if err != nil {
		return unavailable("read PATHPOLICY", err)
	}
	if !found {
		// Without PATHPOLICY only the presets of the other files, the default
		// branch name and the protection BRANCHPOLICY requires are checked:
		// the other checks guard PATHPOLICY.
		ref = p.DefaultBranch
	}
	policies, err := readPolicies(ctx, api, o, p, ref, drift)
	switch {
	case err != nil:
		return unavailable("read policy files", err)
	case !found && len(policies) == 0:
		return nil
	}
	var protector module.BranchProtector
	if bp, ok := policies[branchPolicyFile].(module.BranchProtector); ok {
		protector = bp
	}
	exp := o.Expected
	if len(o.DefaultBranches) > 0 && !slices.Contains(o.DefaultBranches, p.DefaultBranch) {
		drift(CheckDefaultBranch, "the default branch is %q, expected one of %s", p.DefaultBranch, strings.Join(o.DefaultBranches, ", "))
	}
	if level := p.Access(); level < module.AccessMaintainer {
		drift(CheckAccess, "the service account has access level %d, Maintainer (40) is required to see the settings", level)
		return res
	}
	if !found {
		if r := checkBranches(ctx, api, p, protector, false, drift); r != nil {
			return unavailable("protected branches", r)
		}
		return res
	}
	if guard == nil {
		drift(CheckPolicy, "%s on %s cannot be parsed", o.Guard.File(), ref)
	} else {
		checkCIConfig(p, o.Guard, guard, drift)
	}
	if err := checkBranches(ctx, api, p, protector, true, drift); err != nil {
		return unavailable("protected branches", err)
	}
	if _, tags := policies[tagPolicyFile]; len(exp.ProtectedTags) > 0 && tags {
		have, err := api.ProtectedTags(ctx, p.ID)
		if err != nil {
			return unavailable("protected tags", err)
		}
		if missing := missingFrom(exp.ProtectedTags, have); len(missing) > 0 {
			drift(CheckProtectedTags, "not protected: %s", strings.Join(missing, ", "))
		}
	}
	if exp.JobTokenPush != nil {
		switch {
		case !p.JobTokenPush.Present:
			drift(CheckJobTokenPush, "the setting is not visible to the service account")
		case p.JobTokenPush.Value != *exp.JobTokenPush:
			drift(CheckJobTokenPush, "job token push is %t, expected %t", p.JobTokenPush.Value, *exp.JobTokenPush)
		}
	}
	if err := checkPrincipals(ctx, api, o, p, ref, drift); err != nil {
		return unavailable("principals", err)
	}
	return res
}

// checkBranches: the existing branches of the project whose BRANCHPOLICY
// rule has protected are protected and, for a project with PATHPOLICY, so is
// the default branch. A branch counts as protected when a protected branch
// pattern of the project matches its name.
func checkBranches(ctx context.Context, api API, p gitlab.Project, protector module.BranchProtector, defaultBranch bool, drift func(string, string, ...any)) error {
	required := protector != nil && protector.ProtectsBranches()
	if !required && !defaultBranch {
		return nil
	}
	have, err := api.ProtectedBranches(ctx, p.ID)
	if err != nil {
		return err
	}
	covered := func(name string) bool {
		return slices.ContainsFunc(have, func(pattern string) bool { return module.MatchPattern(pattern, name) })
	}
	var missing []string
	if defaultBranch && p.DefaultBranch != "" && !covered(p.DefaultBranch) {
		missing = append(missing, p.DefaultBranch+" (the default branch)")
	}
	if required {
		branches, err := api.Branches(ctx, p.ID)
		if err != nil {
			return err
		}
		slices.Sort(branches)
		for _, b := range branches {
			if protector.MustProtect(b) && !covered(b) {
				missing = append(missing, b+" (BRANCHPOLICY)")
			}
		}
	}
	if len(missing) > 0 {
		drift(CheckProtected, "not protected: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Files of the modules that settings-check reads besides PATHPOLICY.
const (
	branchPolicyFile = "BRANCHPOLICY"
	tagPolicyFile    = "TAGPOLICY"
)

// readPolicies reads the policy files of the enabled modules other than
// PATHPOLICY on ref, by file. A file that cannot be parsed, or whose presets
// are missing or broken, is a drift.
func readPolicies(ctx context.Context, api API, o Options, p gitlab.Project, ref string, drift func(string, string, ...any)) (map[string]module.Policy, error) {
	out := map[string]module.Policy{}
	if ref == "" {
		return out, nil
	}
	for _, m := range o.Modules {
		if o.Guard != nil && m.File() == o.Guard.File() {
			continue
		}
		data, err := api.RawFile(ctx, p.ID, m.File(), ref, m.MaxPolicySize())
		if gitlab.KindOf(err) == gitlab.NotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		pol, err := m.Parse(data)
		if err != nil {
			drift(CheckPolicy, "%s on %s cannot be parsed: %v", m.File(), ref, err)
			out[m.File()] = nil
			continue
		}
		for _, pr := range pol.Validate(ctx, nil) {
			if !pr.Warning && !pr.Unknown {
				drift(CheckPolicy, "%s on %s: %s", m.File(), ref, pr.Msg)
			}
		}
		out[m.File()] = pol
	}
	return out, nil
}

// guardPolicy reads PATHPOLICY from the default branch, or from the expected
// default branch when the current one has none: a switch of the default
// branch to one without PATHPOLICY is a drift, not a project without policy.
// The policy is nil when the file cannot be parsed.
func guardPolicy(ctx context.Context, api API, o Options, p gitlab.Project) (module.Policy, string, bool, error) {
	if o.Guard == nil {
		return nil, "", false, nil
	}
	branches := []string{p.DefaultBranch}
	for _, e := range o.DefaultBranches {
		if e != p.DefaultBranch {
			branches = append(branches, e)
		}
	}
	for _, ref := range branches {
		if ref == "" {
			continue
		}
		data, err := api.RawFile(ctx, p.ID, o.Guard.File(), ref, o.Guard.MaxPolicySize())
		if gitlab.KindOf(err) == gitlab.NotFound {
			continue
		}
		if err != nil {
			return nil, "", false, err
		}
		pol, err := o.Guard.Parse(data)
		if err != nil {
			return nil, ref, true, nil
		}
		return pol, ref, true, nil
	}
	return nil, "", false, nil
}

// defaultCIConfig is the CI file when ci_config_path is empty.
const defaultCIConfig = ".gitlab-ci.yml"

// checkCIConfig: the CI file is a file of this repository covered by a path rule.
func checkCIConfig(p gitlab.Project, root module.Root, guard module.Policy, drift func(string, string, ...any)) {
	if !p.CIConfigPath.Present {
		drift(CheckCIConfigPath, "the setting is not visible to the service account")
		return
	}
	path := p.CIConfigPath.Value
	if path == "" {
		path = defaultCIConfig
	}
	if strings.Contains(path, "@") || strings.Contains(path, "://") {
		drift(CheckCIConfigPath, "%q is outside of the repository", path)
		return
	}
	if guard == nil || !root.Covers(guard, strings.TrimPrefix(path, "/")) {
		drift(CheckCIConfigPath, "%q is not covered by a path rule", path)
	}
}

func missingFrom(want, have []string) []string {
	var missing []string
	for _, w := range want {
		if !slices.Contains(have, w) {
			missing = append(missing, w)
		}
	}
	return missing
}

// checkPrincipals checks that the usernames of every policy file on ref are active users.
func checkPrincipals(ctx context.Context, api API, o Options, p gitlab.Project, ref string, drift func(string, string, ...any)) error {
	var names []string
	seen := map[string]bool{}
	for _, m := range o.Modules {
		data, err := api.RawFile(ctx, p.ID, m.File(), ref, m.MaxPolicySize())
		if gitlab.KindOf(err) == gitlab.NotFound {
			continue
		}
		if err != nil {
			return err
		}
		pol, err := m.Parse(data)
		if err != nil {
			// PATHPOLICY and BRANCHPOLICY report their own parse errors.
			if m != o.Guard && m.File() != branchPolicyFile {
				drift(CheckPolicy, "%s on %s cannot be parsed", m.File(), ref)
			}
			continue
		}
		lister, ok := pol.(module.UsernameLister)
		if !ok {
			continue
		}
		for _, u := range lister.Usernames() {
			if key := strings.ToLower(u); !seen[key] {
				seen[key] = true
				names = append(names, u)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	users, err := api.UsersByUsernames(ctx, names)
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, u := range users {
		active[strings.ToLower(u.Username)] = u.State == "active"
	}
	var gone []string
	for _, n := range names {
		if !active[strings.ToLower(n)] {
			gone = append(gone, n)
		}
	}
	if len(gone) > 0 {
		drift(CheckPrincipals, "not active GitLab users: %s", strings.Join(gone, ", "))
	}
	return nil
}
