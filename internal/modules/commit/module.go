// Package commit is the COMMITPOLICY module: the format of every commit
// message that enters a ref, by independent rules.
package commit

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// File is the policy file of the module.
const File = "COMMITPOLICY"

// MaxEnteringCommits is the limit of commits checked per ref.
const MaxEnteringCommits = 10_000

// PresetErrors lists presets of the server config that cannot be used.
func (m *Module) PresetErrors() []string {
	if m.settings == nil {
		return nil
	}
	errs := m.settings.presets.Errors()
	for i, e := range errs {
		errs[i] = "presets.commitpolicy." + e
	}
	return errs
}

// Module is COMMITPOLICY. It is FailOpen: a broken policy or a failure of the
// API skips the check with a warning instead of blocking merges.
type Module struct {
	settings *Settings
}

// New returns the module; s may be nil when there is no server config.
func New(s *Settings) *Module { return &Module{settings: s} }

func (*Module) Name() string               { return "commitpolicy" }
func (*Module) File() string               { return File }
func (*Module) OnInvalid() module.FailMode { return module.FailOpen }
func (*Module) MaxPolicySize() int         { return module.DefaultMaxPolicySize }

// Needs is empty: COMMITPOLICY gets no authorization backend.
func (*Module) Needs() module.Needs { return module.Needs{} }

// Parse decodes a COMMITPOLICY file offline.
func (m *Module) Parse(blob []byte) (module.Policy, error) {
	p, err := Parse(blob, m.settings)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// EvaluateInvalid skips the check when the policy on the anchor is invalid.
func (*Module) EvaluateInvalid(_ context.Context, in *module.Input, parseErr error) (*module.Result, error) {
	branch := "the default branch"
	if in.Refs != nil {
		branch = strings.TrimPrefix(in.Refs.Default(), "refs/heads/")
	}
	return &module.Result{
		Decision: module.Warn,
		Touched:  true,
		Violations: []module.Violation{{Code: module.CodeInvalidPolicy,
			Title: "COMMITPOLICY in " + branch + " is broken, so commit messages are not checked until it is fixed",
			Facts: []string{"problem: COMMITPOLICY " + parseErr.Error()}}},
		Events: []audit.Event{{Event: audit.EventInvalidCommitPolicy, Decision: "warn", Code: module.CodeInvalidPolicy, Reason: parseErr.Error()}},
	}, nil
}

// Validate checks a new version: what Parse leaves to the anchor (unknown
// checks and presets) and the users of except.users.
func (p *Policy) Validate(ctx context.Context, v module.Validator) []module.Problem {
	if p.unusable != nil {
		return []module.Problem{{Msg: p.unusable.Error()}}
	}
	var problems []module.Problem
	var users []string
	for _, r := range p.rules {
		for _, name := range r.unknown {
			problems = append(problems, module.Problem{Msg: fmt.Sprintf("rule %s: unknown check %q", r.name, name)})
		}
		users = append(users, r.except.Users...)
	}
	if v != nil {
		var added []string
		for _, u := range users {
			if !v.Known(u) {
				added = append(added, u)
			}
		}
		if len(added) > 0 {
			found, status := v.Users(ctx, added)
			if !status.IsConfirmed() {
				return append(problems, module.Problem{Unknown: true, Msg: "except.users cannot be checked: " + status.Reason()})
			}
			for _, u := range added {
				if !found[strings.ToLower(u)].Active {
					problems = append(problems, module.Problem{Msg: fmt.Sprintf("except.users: %q is not an active GitLab user", u)})
				}
			}
		}
	}
	return problems
}

// Usernames are the principals of the policy: at acceptance only the ones
// that the anchor version does not have are looked up.
func (p *Policy) Usernames() []string {
	var out []string
	for _, r := range p.rules {
		out = append(out, r.except.Users...)
	}
	return out
}
