// Package tag is the TAGPOLICY module: a tag whose name matches a rule is
// accepted only when it is signed by a key that the rule allows. It is
// FailClosed and does not call the API: the check relies only on the tag object
// and the public keys in the file itself.
package tag

import (
	"context"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// File is the policy file of the module.
const File = "TAGPOLICY"

// maxPolicySize is the size limit of TAGPOLICY: armored OpenPGP keys are large.
const maxPolicySize = 256 << 10

// Module is TAGPOLICY.
type Module struct{}

// New returns the module.
func New() *Module { return &Module{} }

func (*Module) Name() string               { return "tagpolicy" }
func (*Module) File() string               { return File }
func (*Module) OnInvalid() module.FailMode { return module.FailClosed }

// MaxPolicySize raises the file limit for armored keys.
func (*Module) MaxPolicySize() int { return maxPolicySize }

// Needs is empty: TAGPOLICY does not use the API.
func (*Module) Needs() module.Needs { return module.Needs{} }

// Parse decodes a TAGPOLICY file offline.
func (*Module) Parse(blob []byte) (module.Policy, error) {
	p, err := parsePolicy(blob)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Validate has nothing to ask GitLab: the keys are checked by Parse and ValidateVersion.
func (*policy) Validate(context.Context, module.Validator) []module.Problem { return nil }

// EvaluateInvalid denies creating and moving every tag of the project while
// TAGPOLICY on the anchor is invalid; branches are not affected.
func (*Module) EvaluateInvalid(_ context.Context, in *module.Input, parseErr error) (*module.Result, error) {
	ev := audit.Event{Event: audit.EventInvalidPolicy, Decision: "allow", Code: module.CodeInvalidPolicy, Reason: parseErr.Error()}
	res := &module.Result{}
	msg := "TAGPOLICY on the default branch is invalid: " + parseErr.Error() +
		"\nNo tag can be created or moved until it is fixed on the default branch."
	for _, u := range in.Updates {
		if !u.IsTag() || u.Kind() == module.Delete {
			continue
		}
		res.Decision, res.Touched = module.Deny, true
		res.Violations = append(res.Violations, module.Violation{Code: module.CodeInvalidPolicy, Ref: u.Ref, Message: msg})
	}
	if res.Touched {
		ev.Decision = "deny"
	}
	res.Events = []audit.Event{ev}
	return res, nil
}

// tagName is the tag name without refs/tags/.
func tagName(ref string) string { return strings.TrimPrefix(ref, "refs/tags/") }
