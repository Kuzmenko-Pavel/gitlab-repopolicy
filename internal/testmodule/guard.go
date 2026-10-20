package testmodule

import (
	"context"
	"slices"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

func (g guard) matches(path string) bool {
	for _, p := range g.paths {
		if path == p || strings.HasSuffix(p, "/") && strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func listMatch(patterns []string, ref string) bool {
	for _, p := range patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok && strings.HasPrefix(ref, prefix) || p == ref {
			return true
		}
	}
	return false
}

func (p *Policy) evaluateGuards(ctx context.Context, in *module.Input, res *module.Result) error {
	for _, u := range in.Updates {
		// The commit of a tag is checked as part of its branch.
		if u.Kind() == module.Delete || !u.IsBranch() {
			continue
		}
		cand, err := in.Changes.CandidatePaths(ctx, u)
		if err != nil {
			return err
		}
		for _, g := range p.guards {
			if !slices.ContainsFunc(cand, g.matches) {
				continue
			}
			scope, ok, v := g.scope(ctx, in, u)
			if v != nil {
				res.Touched = true
				p.add(res, *v)
				continue
			}
			if !ok {
				continue
			}
			paths, err := in.Changes.Paths(ctx, u, scope)
			if err != nil {
				return err
			}
			var hit []module.Item
			for _, path := range paths {
				if g.matches(path) {
					hit = append(hit, module.Item{Name: path})
				}
			}
			if len(hit) == 0 {
				continue
			}
			res.Touched = true
			if slices.Contains(g.allow, in.Principal.Username) {
				continue
			}
			p.add(res, module.Violation{Code: module.CodeProtectedPath, Rule: g.name, Ref: u.Ref, Items: hit,
				Reason: "not in allow: " + in.Principal.Username})
		}
	}
	return nil
}

// scope returns the scope of the guard for the ref; ok is false when the
// guard does not apply to it, v is set when the class cannot be determined.
func (g guard) scope(ctx context.Context, in *module.Input, u module.RefUpdate) (module.Scope, bool, *module.Violation) {
	if g.list != nil {
		if !listMatch(g.list, u.Ref) {
			return module.Scope{}, false, nil
		}
		return module.Scope{Class: module.ClassProtected, Key: strings.Join(g.list, ","),
			Protected: func(ref string) bool { return listMatch(g.list, ref) }}, true, nil
	}
	if u.Ref == in.Refs.Default() {
		return module.Scope{Class: module.ClassDefault}, true, nil
	}
	if g.refs == "default" {
		return module.Scope{}, false, nil
	}
	class, status := in.Refs.Class(ctx, u.Ref)
	if !status.IsConfirmed() {
		code := module.CodeAuthorizationBackend
		if status.IsLimit() {
			code = module.CodeLimitExceeded
		}
		return module.Scope{}, false, &module.Violation{Code: code, Rule: g.name, Ref: u.Ref, Reason: status.Reason()}
	}
	if class == module.ClassOther {
		if g.refs == "protected" {
			return module.Scope{}, false, nil
		}
		return module.Scope{Class: module.ClassOther}, true, nil
	}
	scope, status := in.Refs.ProtectedScope(ctx)
	if !status.IsConfirmed() {
		return module.Scope{}, false, &module.Violation{Code: module.CodeAuthorizationBackend, Rule: g.name, Ref: u.Ref, Reason: status.Reason()}
	}
	return scope, true, nil
}

func (p *Policy) add(res *module.Result, v module.Violation) {
	res.Decision = max(res.Decision, p.decision)
	res.Violations = append(res.Violations, v)
}
