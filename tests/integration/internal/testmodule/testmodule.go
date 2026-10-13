// Package testmodule holds policy modules for the integration tests of the
// core. They exist only in test code: cases that need a real module are
// covered with PATHPOLICY, COMMITPOLICY and TAGPOLICY.
package testmodule

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// Guard is a FailClosed module with path rules, a small PATHPOLICY:
//
//	rules:
//	  - name: ci
//	    paths: [ci.yml, deploy/]   # exact path or a directory prefix ending with "/"
//	    allow: [alice]
//	    refs: all                  # all | default | protected | [refs/heads/release/*]
type Guard struct{}

// Note is a FailOpen module with the same syntax; it only warns.
type Note struct{}

// Modules returns the test modules in evaluation order.
func Modules() []module.Module { return []module.Module{Guard{}, Note{}} }

func (Guard) Name() string               { return "guardpolicy" }
func (Guard) File() string               { return "GUARDPOLICY" }
func (Guard) OnInvalid() module.FailMode { return module.FailClosed }
func (Guard) Parse(b []byte) (module.Policy, error) {
	return parse(b, module.Deny)
}

func (Note) Name() string               { return "notepolicy" }
func (Note) File() string               { return "NOTEPOLICY" }
func (Note) OnInvalid() module.FailMode { return module.FailOpen }
func (Note) Parse(b []byte) (module.Policy, error) {
	return parse(b, module.Warn)
}

type file struct {
	Rules []rule `yaml:"rules"`
}

type rule struct {
	Name  string   `yaml:"name"`
	Paths []string `yaml:"paths"`
	Allow []string `yaml:"allow"`
	Refs  any      `yaml:"refs"`
}

type policy struct {
	rules    []rule
	decision module.Decision
}

func parse(b []byte, decision module.Decision) (module.Policy, error) {
	var f file
	if err := strictyaml.Decode(b, &f, module.DefaultMaxPolicySize); err != nil {
		return nil, err
	}
	if len(f.Rules) == 0 {
		return nil, errors.New("rules: at least one rule is required")
	}
	for _, r := range f.Rules {
		if r.Name == "" || len(r.Paths) == 0 {
			return nil, errors.New("rules: every rule needs a name and paths")
		}
		switch v := r.Refs.(type) {
		case nil, string, []any:
			if s, ok := v.(string); ok && s != "all" && s != "default" && s != "protected" {
				return nil, fmt.Errorf("rule %s: unknown refs %q", r.Name, s)
			}
		default:
			return nil, fmt.Errorf("rule %s: refs must be a keyword or a list", r.Name)
		}
	}
	return &policy{rules: f.Rules, decision: decision}, nil
}

func (p *policy) Needs() module.Needs { return module.Needs{} }

// Validate rejects a user named "nobody" offline, so tests can produce an invalid new version.
func (p *policy) Validate(context.Context, module.Validator) []module.Problem {
	var problems []module.Problem
	for _, r := range p.rules {
		if slices.Contains(r.Allow, "nobody") {
			problems = append(problems, module.Problem{Msg: "rule " + r.Name + ": user nobody does not exist"})
		}
	}
	return problems
}

func (r rule) matches(path string) bool {
	for _, p := range r.Paths {
		if path == p || strings.HasSuffix(p, "/") && strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func (r rule) list() []string {
	items, ok := r.Refs.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprint(it))
	}
	return out
}

func listMatch(patterns []string, ref string) bool {
	for _, p := range patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok && strings.HasPrefix(ref, prefix) || p == ref {
			return true
		}
	}
	return false
}

func (p *policy) Evaluate(ctx context.Context, in *module.Input) (*module.Result, error) {
	res := &module.Result{}
	for _, u := range in.Updates {
		if u.Kind() == module.Delete {
			continue
		}
		cand, err := in.Changes.CandidatePaths(ctx, u)
		if errors.Is(err, module.ErrNotCommit) {
			res.Touched = true
			p.add(res, module.Violation{Code: module.CodeTagObjectInvalid, Ref: u.Ref, Message: "the ref does not point to a commit"})
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, r := range p.rules {
			if !slices.ContainsFunc(cand, r.matches) {
				continue
			}
			scope, ok, v := p.scope(ctx, in, r, u)
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
				return nil, err
			}
			var hit []module.Item
			for _, path := range paths {
				if r.matches(path) {
					hit = append(hit, module.Item{Name: path})
				}
			}
			if len(hit) == 0 {
				continue
			}
			res.Touched = true
			if slices.Contains(r.Allow, in.Principal.Username) {
				continue
			}
			p.add(res, module.Violation{Code: module.CodeProtectedPath, Rule: r.Name, Ref: u.Ref, Items: hit,
				Reason: "not in allow: " + in.Principal.Username})
		}
	}
	return res, nil
}

// scope returns the scope of the rule for the ref; ok is false when the rule
// does not apply to it, v is set when the class cannot be determined.
func (p *policy) scope(ctx context.Context, in *module.Input, r rule, u module.RefUpdate) (module.Scope, bool, *module.Violation) {
	if list := r.list(); list != nil {
		if !listMatch(list, u.Ref) {
			return module.Scope{}, false, nil
		}
		return module.Scope{Class: module.ClassProtected, Key: strings.Join(list, ","),
			Protected: func(ref string) bool { return listMatch(list, ref) }}, true, nil
	}
	if u.Ref == in.Refs.Default() {
		return module.Scope{Class: module.ClassDefault}, true, nil
	}
	if r.Refs == "default" {
		return module.Scope{}, false, nil
	}
	class, status := in.Refs.Class(ctx, u.Ref)
	if !status.IsConfirmed() {
		code := module.CodeAuthorizationBackend
		if status.IsLimit() {
			code = module.CodeLimitExceeded
		}
		return module.Scope{}, false, &module.Violation{Code: code, Rule: r.Name, Ref: u.Ref, Reason: status.Reason()}
	}
	if class == module.ClassOther {
		if r.Refs == "protected" {
			return module.Scope{}, false, nil
		}
		return module.Scope{Class: module.ClassOther}, true, nil
	}
	scope, status := in.Refs.ProtectedScope(ctx)
	if !status.IsConfirmed() {
		return module.Scope{}, false, &module.Violation{Code: module.CodeAuthorizationBackend, Rule: r.Name, Ref: u.Ref, Reason: status.Reason()}
	}
	return scope, true, nil
}

func (p *policy) add(res *module.Result, v module.Violation) {
	res.Decision = max(res.Decision, p.decision)
	res.Violations = append(res.Violations, v)
}
