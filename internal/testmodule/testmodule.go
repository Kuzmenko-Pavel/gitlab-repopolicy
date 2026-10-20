// Package testmodule is the policy module of the tests of the core, in unit
// and integration tests. It exists only in test code: cases that need a real
// module use PATHPOLICY, COMMITPOLICY and TAGPOLICY.
//
// A policy file has one instruction per line:
//
//	deny       deny the first update: rule r, item a
//	panic      panic in Evaluate
//	slow       wait for the deadline of the receive
//	touch      report the policy as touched
//	classify   ask GitLab for the class of the first ref; deny when it does not answer
//	sign       report a verified signature for every update
//	bad-new    reject a new version at acceptance
//	versioned  report the anchor version at acceptance
//	preset <name>  report the preset as extended, with the digest "sha-<name>"
//	guard <rule> <path>,... allow <user>,... [refs all|default|protected|<ref>,...]
//	           a change of the paths needs a user in allow; a path ending
//	           with "/" is a directory, a ref ending with "*" a prefix
//
// Any other line makes the file invalid. guard denies in a FailClosed module
// and warns in a FailOpen one; a guard that allows "nobody" is rejected at acceptance.
package testmodule

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// Module is the test module with its own file and fail mode.
type Module struct {
	FileName string
	Mode     module.FailMode
}

func (m Module) Name() string               { return strings.ToLower(m.FileName) }
func (m Module) File() string               { return m.FileName }
func (m Module) OnInvalid() module.FailMode { return m.Mode }
func (Module) MaxPolicySize() int           { return module.DefaultMaxPolicySize }
func (Module) Needs() module.Needs          { return module.Needs{} }

// EvaluateInvalid denies every change of branch and tag content when the
// module fails closed and only warns when it fails open.
func (m Module) EvaluateInvalid(_ context.Context, in *module.Input, parseErr error) (*module.Result, error) {
	msg := fmt.Sprintf("%s on the default branch is invalid: %v", m.FileName, parseErr)
	ev := audit.Event{Event: audit.EventInvalidPolicy, Code: module.CodeInvalidPolicy, Reason: parseErr.Error()}
	if m.Mode == module.FailOpen {
		ev.Decision = "warn"
		return &module.Result{Decision: module.Warn, Touched: true, Events: []audit.Event{ev},
			Violations: []module.Violation{{Code: module.CodeInvalidPolicy, Message: msg + "\nThe check is skipped until it is fixed."}}}, nil
	}
	res := &module.Result{Touched: true}
	for _, u := range in.Updates {
		if u.Kind() == module.Delete {
			continue
		}
		res.Decision = module.Deny
		res.Violations = append(res.Violations, module.Violation{Code: module.CodeInvalidPolicy, Ref: u.Ref,
			Message: msg + "\nNothing can change until it is fixed on the default branch."})
	}
	ev.Decision = res.Decision.String()
	res.Events = []audit.Event{ev}
	return res, nil
}

// Policy is a parsed file of the test module.
type Policy struct {
	instructions []string
	guards       []guard
	decision     module.Decision
	presets      []module.PresetRef
}

// Presets are the presets of the preset instructions.
func (p *Policy) Presets() []module.PresetRef { return p.presets }

// Origins are not tracked: the test module has no rules to merge.
func (p *Policy) Origins() []module.RuleOrigin { return nil }

type guard struct {
	name  string
	paths []string
	allow []string
	// refs is "", "default", "protected" or a list of refs.
	refs string
	list []string
}

// Parse reads the instructions of a file.
func (m Module) Parse(blob []byte) (module.Policy, error) {
	p := &Policy{decision: module.Deny}
	if m.Mode == module.FailOpen {
		p.decision = module.Warn
	}
	for _, line := range strings.Split(string(blob), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "deny", "panic", "slow", "touch", "classify", "bad-new", "sign", "versioned":
			p.instructions = append(p.instructions, f[0])
		case "preset":
			if len(f) != 2 {
				return nil, errors.New("preset needs a name")
			}
			p.presets = append(p.presets, module.PresetRef{Name: f[1], SHA256: "sha-" + f[1]})
		case "guard":
			g, err := parseGuard(f)
			if err != nil {
				return nil, err
			}
			p.guards = append(p.guards, g)
		default:
			return nil, errors.New("unknown instruction " + f[0])
		}
	}
	return p, nil
}

func parseGuard(f []string) (guard, error) {
	switch {
	case len(f) == 5 && f[3] == "allow":
	case len(f) == 7 && f[3] == "allow" && f[5] == "refs":
	default:
		return guard{}, fmt.Errorf("guard: expected guard <rule> <paths> allow <users> [refs <refs>], got %q", strings.Join(f, " "))
	}
	g := guard{name: f[1], paths: strings.Split(f[2], ","), allow: strings.Split(f[4], ",")}
	if len(f) == 7 {
		switch f[6] {
		case "all":
		case "default", "protected":
			g.refs = f[6]
		default:
			g.list = strings.Split(f[6], ",")
		}
	}
	return g, nil
}

// Validate rejects bad-new and a guard that allows "nobody".
func (p *Policy) Validate(context.Context, module.Validator) []module.Problem {
	var problems []module.Problem
	if slices.Contains(p.instructions, "bad-new") {
		problems = append(problems, module.Problem{Msg: "bad-new is rejected at acceptance"})
	}
	for _, g := range p.guards {
		if slices.Contains(g.allow, "nobody") {
			problems = append(problems, module.Problem{Msg: "rule " + g.name + ": user nobody does not exist"})
		}
	}
	return problems
}

// ValidateVersion of "versioned" reports the anchor version it was given.
func (p *Policy) ValidateVersion(anchor module.Policy, now time.Time) []module.Problem {
	if !slices.Contains(p.instructions, "versioned") {
		return nil
	}
	if now.IsZero() {
		return []module.Problem{{Msg: "no clock"}}
	}
	var prev []string
	if a, ok := anchor.(*Policy); ok {
		prev = a.instructions
	}
	return []module.Problem{{Warning: true, Msg: "anchor version: " + strings.Join(prev, ",")}}
}

// Evaluate runs the instructions, then the guards.
func (p *Policy) Evaluate(ctx context.Context, in *module.Input) (*module.Result, error) {
	res := &module.Result{}
	for _, s := range p.instructions {
		switch s {
		case "panic":
			panic("boom")
		case "slow":
			<-ctx.Done()
			return nil, ctx.Err()
		case "touch":
			res.Touched = true
		case "sign":
			res.Touched = true
			for _, u := range in.Updates {
				res.Signatures = append(res.Signatures, module.Signature{Ref: u.Ref, Rules: []string{"r"}, Signer: "lead", Fingerprint: "FPR"})
			}
		case "classify":
			_, st := in.Refs.Class(ctx, in.Updates[0].Ref)
			if !st.IsConfirmed() {
				return &module.Result{Decision: module.Deny, Touched: true, Violations: []module.Violation{{Code: module.CodeAuthorizationBackend, Ref: in.Updates[0].Ref}}}, nil
			}
			res.Touched = true
		case "deny":
			res.Decision, res.Touched = module.Deny, true
			res.Violations = append(res.Violations, module.Violation{Code: module.CodeProtectedPath, Rule: "r", Ref: in.Updates[0].Ref,
				Items: []module.Item{{Name: "a"}}, Reason: "audit only"})
		}
	}
	if len(p.guards) == 0 {
		return res, nil
	}
	return res, p.evaluateGuards(ctx, in, res)
}
