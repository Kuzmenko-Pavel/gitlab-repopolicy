package branch

import (
	"context"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// Evaluate checks every new branch except the default one. An update or a
// deletion of a branch is not checked: existing branches stay as they are.
func (p *Policy) Evaluate(ctx context.Context, in *module.Input) (*module.Result, error) {
	if p.unusable != nil {
		if !createsBranch(in) {
			return &module.Result{}, nil
		}
		return skipped(in, "problem: "+strings.ReplaceAll(p.unusable.Error(), "\n", "; ")), nil
	}
	res := &module.Result{}
	for _, u := range in.Updates {
		if !checked(in, u) {
			continue
		}
		name := strings.TrimPrefix(u.Ref, "refs/heads/")
		r := first(p.rules, name)
		for _, v := range p.check(ctx, in, r, name) {
			v.Ref = u.Ref
			if r != nil {
				v.Rule = r.name
			}
			res.Touched = true
			res.Violations = append(res.Violations, v.Violation)
			switch {
			case v.warn || p.warnMode:
				res.Decision = max(res.Decision, module.Warn)
			default:
				res.Decision = module.Deny
			}
		}
	}
	return res, nil
}

// finding is a violation and whether it is only a warning.
type finding struct {
	module.Violation
	warn bool
}

// check applies the rule of the class of a new branch: deny, pattern,
// user_prefix, then protected; a name that breaks the class stops there.
func (p *Policy) check(ctx context.Context, in *module.Input, r *rule, name string) []finding {
	switch {
	case r == nil:
		return []finding{{Violation: module.Violation{Code: module.CodeBranchName, Reason: "no rule matches",
			Title:   "no rule of BRANCHPOLICY allows this name",
			Hint:    "how to fix: rename the branch locally (git branch -m) to a name the rules of this project allow, and push again",
			Summary: "branch " + name + " fits no BRANCHPOLICY rule; rename it (git branch -m) and push again"}}}
	case r.deny:
		return []finding{{Violation: module.Violation{Code: module.CodeBranchName, Reason: "deny", Message: r.message,
			Title:   "creating this branch is forbidden",
			Summary: "branch " + name + " is forbidden by BRANCHPOLICY rule " + r.name}}}
	case r.pattern != nil && !r.pattern.MatchString(name):
		return []finding{nameViolation(r, name, "pattern")}
	}
	if r.userPrefix {
		if f := userPrefix(ctx, in, r, name); f != nil {
			return []finding{*f}
		}
	}
	if r.protected {
		if f := protected(ctx, in, r, name); f != nil {
			return []finding{*f}
		}
	}
	return nil
}

func nameViolation(r *rule, name, reason string) finding {
	return finding{Violation: module.Violation{Code: module.CodeBranchName, Reason: reason, Message: r.message,
		Title:   "the name does not follow the branch standard",
		Hint:    "how to fix: rename the branch locally (git branch -m) and push again",
		Summary: "branch " + name + " breaks BRANCHPOLICY rule " + r.name + "; rename it (git branch -m) and push again"}}
}

// userPrefix checks that the part before the first / names an active GitLab
// user. The pusher is such a user when the prefix is their own username, so
// GitLab is asked only about someone else's prefix or a push with a deploy key.
func userPrefix(ctx context.Context, in *module.Input, r *rule, name string) *finding {
	prefix, _, ok := strings.Cut(name, "/")
	if !ok || prefix == "" {
		f := nameViolation(r, name, "no user prefix")
		f.Facts = []string{"the name starts with a GitLab username and /"}
		return &f
	}
	if kind, _ := in.Principal.Kind(); kind == module.PrincipalUser && strings.EqualFold(prefix, in.Principal.Username) {
		return nil
	}
	users, st := in.Authz.Users(ctx, []string{prefix})
	if !st.IsConfirmed() {
		return unanswered(st, "could not check that "+prefix+" is a GitLab user", name)
	}
	if u, found := users[strings.ToLower(prefix)]; found && u.Active {
		return nil
	}
	f := nameViolation(r, name, "prefix "+prefix+" is not an active user")
	f.Facts = []string{prefix + " is not a GitLab user; the name starts with a GitLab username and /"}
	return &f
}

// protected checks that GitLab protects a new branch of a class with
// protected: its pipeline gets protected variables and runners only then.
func protected(ctx context.Context, in *module.Input, r *rule, name string) *finding {
	class, st := in.Refs.Class(ctx, "refs/heads/"+name)
	switch {
	case !st.IsConfirmed():
		return unanswered(st, "could not check that the branch is protected", name)
	case class == module.ClassOther:
		return &finding{Violation: module.Violation{Code: module.CodeBranchNotProtected, Reason: "not a protected branch", Message: r.message,
			Title:   "the branch must be protected, but the project does not protect it",
			Hint:    "how to fix: a Maintainer adds " + r.match + " in Settings > Repository > Protected branches, then push again",
			Summary: "branch " + name + " must be protected: a Maintainer adds " + r.match + " in Settings > Repository > Protected branches, then push again"}}
	}
	return nil
}

// unanswered is the warning of a check that GitLab did not answer: the
// branch is accepted.
func unanswered(st module.Status, what, name string) *finding {
	v := module.Violation{Code: module.CodeAuthorizationBackend, Reason: st.Reason(),
		Title:   what + ": the GitLab API did not answer; the branch is accepted",
		Summary: "branch " + name + " is accepted, but " + what}
	if st.IsLimit() {
		v.Code, v.Title = module.CodeLimitExceeded, what+" within the server limits; the branch is accepted"
	}
	return &finding{Violation: v, warn: true}
}
