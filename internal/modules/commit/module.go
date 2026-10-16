// Package commit is the COMMITPOLICY module: the format of every commit
// message that enters a ref, by independent rules.
package commit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// File is the policy file of the module.
const File = "COMMITPOLICY"

// MaxEnteringCommits is the limit of commits checked per ref.
const MaxEnteringCommits = 10_000

// Settings come from the server config.
type Settings struct {
	// Presets are the decoded commitpolicy_presets; a broken preset is
	// treated as unknown, so that extends of it is reported.
	presets      map[string]map[string]map[string]any
	presetErrors map[string]error
	// PatternTimeout and PatternBudget bound the pattern rule per message and per receive.
	PatternTimeout time.Duration
	PatternBudget  time.Duration
	// Clock measures the pattern budget.
	Clock func() time.Time
}

// NewSettings decodes the raw presets of the server config.
func NewSettings(rawPresets map[string]any, timeout, budget time.Duration, clock func() time.Time) *Settings {
	s := &Settings{presets: map[string]map[string]map[string]any{}, presetErrors: map[string]error{},
		PatternTimeout: timeout, PatternBudget: budget, Clock: clock}
	for name, raw := range rawPresets {
		var p presetYAML
		tree, err := strictyaml.DecodeValueTree(raw, module.DefaultMaxPolicySize)
		if err == nil {
			err = spec.Validate(presetSpec, tree, checks)
		}
		if err == nil {
			err = strictyaml.DecodeValue(raw, &p, module.DefaultMaxPolicySize)
		}
		if err != nil {
			s.presetErrors[name] = err
			continue
		}
		s.presets[name] = p.Rules
	}
	return s
}

// PresetErrors lists presets of the server config that cannot be used.
func (s *Settings) PresetErrors() []string {
	out := make([]string, 0, len(s.presetErrors))
	for name, err := range s.presetErrors {
		out = append(out, fmt.Sprintf("commitpolicy_presets.%s: %v", name, err))
	}
	sort.Strings(out)
	return out
}

// Module is COMMITPOLICY. It is FailOpen: a broken policy or a failure of the
// API skips the check with a warning instead of blocking merges.
type Module struct {
	settings *Settings
}

// New returns the module.
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
func (*Module) EvaluateInvalid(_ context.Context, _ *module.Input, parseErr error) (*module.Result, error) {
	return &module.Result{
		Decision: module.Warn,
		Touched:  true,
		Violations: []module.Violation{{Code: module.CodeInvalidPolicy,
			Message: "COMMITPOLICY on the default branch is invalid: " + parseErr.Error() + "\nCommit messages are not checked until it is fixed."}},
		Events: []audit.Event{{Event: audit.EventInvalidCommitPolicy, Decision: "warn", Code: module.CodeInvalidPolicy, Reason: parseErr.Error()}},
	}, nil
}

// Validate checks a new version: what Parse leaves to the anchor (unknown
// rules and presets), pattern examples and the users of except.users.
func (p *Policy) Validate(ctx context.Context, v module.Validator) []module.Problem {
	var problems []module.Problem
	if p.unknownPreset != "" {
		msg := fmt.Sprintf("extends: preset %q is not defined in the server config", p.unknownPreset)
		if err, ok := p.settings.presetErrors[p.unknownPreset]; ok {
			msg = fmt.Sprintf("extends: preset %q of the server config is invalid: %v", p.unknownPreset, err)
		}
		problems = append(problems, module.Problem{Msg: msg})
	}
	var users []string
	for _, rs := range p.rulesets {
		for _, name := range rs.unknown {
			problems = append(problems, module.Problem{Msg: fmt.Sprintf("ruleset %s: unknown rule %q", rs.name, name)})
		}
		for _, r := range rs.rules {
			problems = append(problems, r.checkExamples(rs)...)
		}
		users = append(users, rs.except.Users...)
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
	for _, rs := range p.rulesets {
		out = append(out, rs.except.Users...)
	}
	return out
}

// checkExamples runs the examples of a pattern rule.
func (r *rule) checkExamples(rs *ruleset) []module.Problem {
	if r.pattern == nil {
		return nil
	}
	var problems []module.Problem
	run := func(text string, want bool) {
		ok, err := r.matchPattern(Normalise(text), 0)
		switch {
		case err != nil:
			problems = append(problems, module.Problem{Msg: fmt.Sprintf("ruleset %s: pattern example %q: %v", rs.name, firstLine(text), err)})
		case ok != want:
			verb := "does not match"
			if ok {
				verb = "matches"
			}
			problems = append(problems, module.Problem{Msg: fmt.Sprintf("ruleset %s: pattern %s the example %q", rs.name, verb, firstLine(text))})
		}
	}
	for _, e := range r.examples.Valid {
		run(e, true)
	}
	for _, e := range r.examples.Invalid {
		run(e, false)
	}
	return problems
}
