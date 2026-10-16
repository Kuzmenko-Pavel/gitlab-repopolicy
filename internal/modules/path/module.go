// Package path is the PATHPOLICY module: who may change which paths of the
// repository. It is FailClosed and the root of trust: it
// also guards itself through admins and the files of the other modules
// through its ordinary rules.
package path

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// File is the policy file of the module.
const File = "PATHPOLICY"

// Module is PATHPOLICY.
type Module struct{}

// New returns the module.
func New() *Module { return &Module{} }

func (*Module) Name() string               { return "pathpolicy" }
func (*Module) File() string               { return File }
func (*Module) OnInvalid() module.FailMode { return module.FailClosed }
func (*Module) MaxPolicySize() int         { return module.DefaultMaxPolicySize }

// Needs gives the authorizer: PATHPOLICY decides on access and asks GitLab
// about people, also for bootstrap and recovery.
func (*Module) Needs() module.Needs { return module.Needs{Identity: true} }

// Parse decodes a PATHPOLICY file offline.
func (*Module) Parse(blob []byte) (module.Policy, error) {
	p, err := Parse(blob)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Validate checks a new version: usernames added in this version exist and
// are active, and at least one admin is effective, or the new version
// would lock everyone out.
func (p *Policy) Validate(ctx context.Context, v module.Validator) []module.Problem {
	if v == nil {
		return nil
	}
	var problems []module.Problem
	var added []string
	seen := map[string]bool{}
	for _, u := range p.Usernames() {
		key := strings.ToLower(u)
		if !seen[key] && !v.Known(u) {
			added = append(added, u)
		}
		seen[key] = true
	}
	if len(added) > 0 {
		users, status := v.Users(ctx, added)
		if !status.IsConfirmed() {
			return []module.Problem{{Unknown: true, Msg: "usernames cannot be checked: " + status.Reason()}}
		}
		for _, u := range added {
			if !users[strings.ToLower(u)].Active {
				problems = append(problems, module.Problem{Msg: fmt.Sprintf("%q is not an active GitLab user", u)})
			}
		}
	}
	n, status := countAdmins(ctx, v, p.admins)
	switch {
	case !status.IsConfirmed():
		problems = append(problems, module.Problem{Unknown: true, Msg: "admins cannot be checked: " + status.Reason()})
	case n == 0:
		problems = append(problems, module.Problem{Code: module.CodeLockoutPrevented,
			Msg: "none of the admins is an active user with at least Maintainer access to the project"})
	}
	return problems
}

// countAdmins counts effective admins: admins that are
// active users with effective access of at least Maintainer. A failure is
// unknown and never zero.
func countAdmins(ctx context.Context, a module.AccessChecker, admins []string) (int, module.Status) {
	report, status := adminReport(ctx, a, admins)
	if !status.IsConfirmed() {
		return 0, status
	}
	n := 0
	for _, ad := range report {
		if ad.Effective {
			n++
		}
	}
	return n, module.Confirmed()
}

// Admin is one configured admin as gitlab-repopolicy admins shows it.
type Admin struct {
	Username string
	// Exists and Active come from GitLab; Access is the effective access level in the project.
	Exists    bool
	Active    bool
	Access    int
	Effective bool
}

// AdminReport checks every admin for gitlab-repopolicy admins; a status that
// is not confirmed means GitLab did not answer and the report is incomplete.
func (p *Policy) AdminReport(ctx context.Context, a module.AccessChecker) ([]Admin, module.Status) {
	return adminReport(ctx, a, p.admins)
}

func adminReport(ctx context.Context, a module.AccessChecker, admins []string) ([]Admin, module.Status) {
	report := make([]Admin, len(admins))
	users, status := a.Users(ctx, admins)
	if !status.IsConfirmed() {
		return nil, status
	}
	var ids []int64
	for i, name := range admins {
		u, ok := users[strings.ToLower(name)]
		report[i] = Admin{Username: name, Exists: ok, Active: u.Active}
		if u.Active && u.ID != 0 {
			ids = append(ids, u.ID)
		}
	}
	if len(ids) == 0 {
		return report, module.Confirmed()
	}
	levels, status := a.AccessLevels(ctx, ids)
	if !status.IsConfirmed() {
		return nil, status
	}
	for i, name := range admins {
		if u := users[strings.ToLower(name)]; u.Active && u.ID != 0 {
			report[i].Access = levels[u.ID]
			report[i].Effective = levels[u.ID] >= module.AccessMaintainer
		}
	}
	return report, module.Confirmed()
}
