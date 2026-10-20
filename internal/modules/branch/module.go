// Package branch is the BRANCHPOLICY module: a new branch gets a class by the
// rule with the lowest priority whose match it fits; the class may forbid it,
// require a GitLab username before the first / or a protected branch, and a
// name that fits no class is not checked. It is
// hygiene, not access, so it is FailOpen: a broken policy or a failure of the
// API skips the check with a warning. Existing branches are not checked.
package branch

import (
	"context"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// File is the policy file of the module.
const File = "BRANCHPOLICY"

// Module is BRANCHPOLICY.
type Module struct {
	settings *Settings
}

// New returns the module.
func New(s *Settings) *Module { return &Module{settings: s} }

func (*Module) Name() string               { return "branchpolicy" }
func (*Module) File() string               { return File }
func (*Module) OnInvalid() module.FailMode { return module.FailOpen }
func (*Module) MaxPolicySize() int         { return module.DefaultMaxPolicySize }

// Needs identity for user_prefix: the authorizer looks a username up.
func (*Module) Needs() module.Needs { return module.Needs{Identity: true} }

// PresetErrors lists presets of the server config that cannot be used.
func (m *Module) PresetErrors() []string {
	if m.settings == nil {
		return nil
	}
	errs := m.settings.presets.Errors()
	for i, e := range errs {
		errs[i] = "presets.branchpolicy." + e
	}
	return errs
}

// Parse decodes a BRANCHPOLICY file offline.
func (m *Module) Parse(blob []byte) (module.Policy, error) {
	p, err := Parse(blob, m.settings)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// EvaluateInvalid skips the check when the policy on the anchor is invalid,
// and says so only to pushes that create a branch: others are not checked anyway.
func (*Module) EvaluateInvalid(_ context.Context, in *module.Input, parseErr error) (*module.Result, error) {
	if !createsBranch(in) {
		return &module.Result{}, nil
	}
	return skipped(in, "problem: BRANCHPOLICY "+parseErr.Error()), nil
}

// skipped is the warning of a module that cannot check new branches.
func skipped(in *module.Input, fact string) *module.Result {
	branch := strings.TrimPrefix(in.Refs.Default(), "refs/heads/")
	return &module.Result{
		Decision: module.Warn,
		Touched:  true,
		Violations: []module.Violation{{Code: module.CodeInvalidPolicy,
			Title: "BRANCHPOLICY in " + branch + " cannot be used, so names of new branches are not checked until it is fixed",
			Facts: []string{fact}}},
		Events: []audit.Event{{Event: audit.EventInvalidBranchPolicy, Decision: "warn", Code: module.CodeInvalidPolicy, Reason: fact}},
	}
}

func createsBranch(in *module.Input) bool {
	for _, u := range in.Updates {
		if checked(in, u) {
			return true
		}
	}
	return false
}

// checked reports an update that the module checks: a new branch other than the default one.
func checked(in *module.Input, u module.RefUpdate) bool {
	return u.IsBranch() && u.Kind() == module.Create && u.Ref != in.Refs.Default()
}
