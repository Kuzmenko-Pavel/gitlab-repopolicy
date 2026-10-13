// Package module is the contract between the core and policy modules (architecture §8).
// The core does not know what a module checks; a module does not know how
// changes and identity were obtained.
package module

import (
	"context"
	"errors"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
)

// FailMode is what happens when the policy on the anchor is invalid.
type FailMode int

// Fail modes.
const (
	FailClosed FailMode = iota
	FailOpen
)

// DefaultMaxPolicySize is the size limit of a policy file (architecture §17).
const DefaultMaxPolicySize = 64 << 10

// Module is one policy kind with its own file.
type Module interface {
	// Name is the module name in config and audit, for example "pathpolicy".
	Name() string
	// File is the policy file path at the repository root, for example "PATHPOLICY".
	File() string
	OnInvalid() FailMode
	// Parse decodes and checks a policy file offline: strict decoding and limits, no network.
	Parse(blob []byte) (Policy, error)
}

// SizeLimiter is implemented by modules whose files may exceed DefaultMaxPolicySize.
type SizeLimiter interface {
	MaxPolicySize() int
}

// MaxPolicySize returns the size limit of the module's file.
func MaxPolicySize(m Module) int {
	if l, ok := m.(SizeLimiter); ok {
		return l.MaxPolicySize()
	}
	return DefaultMaxPolicySize
}

// Policy is a parsed policy file.
type Policy interface {
	// Validate runs the checks that need GitLab, such as existence of principals.
	// The core calls it when it accepts a new version of the file.
	Validate(ctx context.Context, v Validator) []Problem
	// Needs declares which inputs the policy uses.
	Needs() Needs
	Evaluate(ctx context.Context, in *Input) (*Result, error)
}

// Needs declares inputs that are not available to every module.
type Needs struct {
	// Identity gives the module access to the authorization backend.
	Identity bool
}

// Problem is a finding of policy validation.
type Problem struct {
	Msg string
	// Warning problems do not reject a new version.
	Warning bool
	// Unknown means the check could not be done because GitLab did not answer.
	Unknown bool
}

// Validator answers the online questions of policy validation.
type Validator interface {
	// ActiveUsers reports which of the usernames exist and are active.
	ActiveUsers(ctx context.Context, usernames []string) (map[string]bool, Status)
}

// Input is everything a module may use to evaluate a push.
type Input struct {
	// Anchor is the commit of the default branch before the push; empty without one.
	Anchor  string
	Updates []RefUpdate
	Refs    RefClassifier
	Changes ChangeSet
	// Principal is the identity of the pusher as GL_ID and GL_USERNAME.
	Principal Principal
	// Principals resolves the pusher to a person's username; an SSH key is
	// checked to be personal (architecture §9.2). It calls the API lazily.
	Principals PrincipalResolver
	Objects    ObjectReader
	Clock      func() time.Time
	// PolicyFiles are the files of all enabled modules; a change to any of them always touches the rules.
	PolicyFiles []string
}

// Decision of a module.
type Decision int

// Decisions; their order is the severity.
const (
	Allow Decision = iota
	Warn
	Deny
)

func (d Decision) String() string {
	switch d {
	case Warn:
		return "warn"
	case Deny:
		return "deny"
	default:
		return "allow"
	}
}

// Result of a module for the whole push.
type Result struct {
	Decision Decision
	// Touched means that some rule applied to the push; a push that touches no rule is not audited.
	Touched    bool
	Violations []Violation
	// Notices are printed on a successful push, for example a recovery notice.
	Notices []string
	// Events are extra audit events of the module.
	Events []audit.Event
}

// Violation is one broken rule.
type Violation struct {
	Code string
	Rule string
	Ref  string
	// Items are the files or commits the violation is about, in output order.
	Items []Item
	// Message is the user text from the policy; empty means the default text.
	Message string
	// Hint is a fixed remedy line, for example how to reword commits.
	Hint string
	// Reason may hold API results; it goes only to audit.
	Reason string
}

// Item is a file or a commit with optional details.
type Item struct {
	Name    string
	Details []string
	// Commit marks commit items; others are paths.
	Commit bool
}

// Violation codes of architecture §15.
const (
	CodeProtectedPath        = "protected_path_violation"
	CodePolicyAdmin          = "policy_admin_violation"
	CodeLockoutPrevented     = "lockout_prevented"
	CodeInvalidPolicy        = "invalid_policy"
	CodeInvalidPolicyUpdate  = "invalid_policy_update"
	CodeAuthorizationBackend = "authorization_backend_failure"
	CodeUnknownPrincipal     = "unknown_principal"
	CodeCommitFormat         = "commit_format_violation"
	CodeCommitCheckTimeout   = "commit_check_timeout"
	CodeTagNotSigned         = "tag_not_signed"
	CodeTagObjectInvalid     = "tag_object_invalid"
	CodeTagSignerNotAllowed  = "tag_signer_not_allowed"
	CodeTagSignatureInvalid  = "tag_signature_invalid"
	CodeLimitExceeded        = "limit_exceeded"
	CodeInternalError        = "internal_error"
)

// Errors that ChangeSet, ObjectReader and RefClassifier return.
var (
	// ErrLimitExceeded is returned when a commit list exceeds the requested maximum.
	ErrLimitExceeded = errors.New("limit exceeded")
	// ErrNotCommit is returned when a ref does not peel to a commit.
	ErrNotCommit = errors.New("object is not a commit")
	// ErrTooLarge is returned when an object exceeds the requested size.
	ErrTooLarge = errors.New("object too large")
	// ErrMissing is returned for an object that does not exist.
	ErrMissing = errors.New("object missing")
)

// InvalidEvaluator is implemented by modules whose reaction to an invalid
// policy on the anchor is narrower than the default, for example TAGPOLICY
// blocks only tags and PATHPOLICY allows Owner recovery (architecture §14).
// Without it the core denies every change of branch and tag content for
// FailClosed modules and only warns for FailOpen modules.
type InvalidEvaluator interface {
	EvaluateInvalid(ctx context.Context, in *Input, parseErr error) (*Result, error)
}

// AbsentEvaluator is implemented by modules that act when the anchor has no
// policy file, for example the PATHPOLICY bootstrap (architecture §9.4).
type AbsentEvaluator interface {
	EvaluateAbsent(ctx context.Context, in *Input) (*Result, error)
}

// SchemaProvider is implemented by modules that publish a JSON Schema of their
// file in schemas/ for editors (architecture §22).
type SchemaProvider interface {
	// SchemaType returns a zero value of the Go type the file is decoded into.
	SchemaType() any
}

// JSONSchemaer is implemented by types of a policy schema whose JSON Schema
// the reflection of their Go type cannot express, such as enums or durations.
type JSONSchemaer interface {
	JSONSchema() map[string]any
}

// PrincipalResolver resolves the pusher to the username of a person.
type PrincipalResolver interface {
	// Username is "" for a deploy key or an unparsable GL_ID; the status is
	// unknown when GitLab did not answer.
	Username(ctx context.Context) (string, Status)
}

// LintFinding is a broken rule found by a local lint of a commit message.
type LintFinding struct {
	Ruleset string
	// Error is false for warnings.
	Error  bool
	Reason string
}

// CommitLinter is implemented by policies that check commit messages
// outside of a push, for gitlab-repopolicy lint-commit.
type CommitLinter interface {
	LintMessage(ctx context.Context, message string) []LintFinding
	LintCommit(ctx context.Context, object []byte) []LintFinding
}

// UsernameLister is implemented by policies with principals. At acceptance the
// core checks only usernames that the anchor version does not have (D28).
type UsernameLister interface {
	Usernames() []string
}
