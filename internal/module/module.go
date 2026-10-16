// Package module is the contract between the core and policy modules (architecture §8).
// The core does not know what a module checks; a module does not know how
// changes and identity were obtained.
package module

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
)

// FailMode is how a module fails. The core applies it to an error or a panic
// of the module; EvaluateInvalid follows it for an invalid policy on the anchor.
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
	// MaxPolicySize is the size limit of the file, DefaultMaxPolicySize unless
	// the format needs more.
	MaxPolicySize() int
	// Needs declares which inputs the module uses. It does not depend on the
	// file, so the core knows it also when the anchor has no valid policy.
	Needs() Needs
	// Parse decodes and checks a policy file offline: strict decoding and limits, no network.
	Parse(blob []byte) (Policy, error)
	// EvaluateInvalid decides a push while the file on the anchor does not
	// parse (architecture §14), for example TAGPOLICY blocks only tags.
	EvaluateInvalid(ctx context.Context, in *Input, parseErr error) (*Result, error)
}

// Policy is a parsed policy file.
type Policy interface {
	// Validate runs the checks that need GitLab, such as existence of principals.
	// The core calls it when it accepts a new version of the file.
	Validate(ctx context.Context, v Validator) []Problem
	Evaluate(ctx context.Context, in *Input) (*Result, error)
}

// Needs declares inputs that are not available to every module.
type Needs struct {
	// Identity gives the module access to the authorization backend.
	Identity bool
}

// Problem is a finding of policy validation.
type Problem struct {
	// Code is the violation code of a rejected new version; empty means invalid_policy_update.
	Code string
	Msg  string
	// Warning problems do not reject a new version.
	Warning bool
	// Unknown means the check could not be done because GitLab did not answer.
	Unknown bool
}

// Access levels of GitLab members.
const (
	AccessDeveloper  = 30
	AccessMaintainer = 40
	AccessOwner      = 50
)

// User is a GitLab user as policies see it.
type User struct {
	ID     int64
	Active bool
}

// Validator answers the online questions of policy validation.
type Validator interface {
	// Users looks usernames up in one request (D28). The map is keyed by the
	// lowercased username; a missing key means the user does not exist or is
	// not visible (blocked, banned).
	Users(ctx context.Context, usernames []string) (map[string]User, Status)
	// AccessLevels returns the effective access level in the project by user ID; 0 is not a member.
	AccessLevels(ctx context.Context, userIDs []int64) (map[int64]int, Status)
	// Known reports whether the username is in the version on the anchor:
	// such usernames are not checked for existence again (D28).
	Known(username string) bool
}

// Authorizer answers questions about the pusher and the project for modules
// that declare Needs().Identity (architecture §13). Answers are three-valued:
// a status that is not confirmed is never "no".
type Authorizer interface {
	// PusherAccess is the effective access level of the pushing person; 0 is
	// not a member, a deploy key or an unknown principal.
	PusherAccess(ctx context.Context) (int, Status)
	Users(ctx context.Context, usernames []string) (map[string]User, Status)
	AccessLevels(ctx context.Context, userIDs []int64) (map[int64]int, Status)
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
	// Principals resolves the pusher to a person; an SSH key is checked to be
	// personal (architecture §9.2). It calls the API lazily.
	Principals PrincipalResolver
	// Authz is set only for modules with Needs().Identity.
	Authz Authorizer
	// AnchorPolicies are the policy files present on the anchor.
	AnchorPolicies []string
	// PolicyChanges are the policy files the push changes, from acceptance.
	PolicyChanges []PolicyChange
	Objects       ObjectReader
	Clock         func() time.Time
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
	// Signatures are the verified signers of tags, for the decision events.
	Signatures []Signature
}

// Signature is the signer of a tag that a module verified (architecture §11.2).
type Signature struct {
	Ref string
	// Rules are the rules whose names matched the tag.
	Rules []string
	// Signer is the key label in the policy file.
	Signer      string
	Fingerprint string
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

// Root is the module of the trust root, PATHPOLICY (architecture §1, D4). It
// guards the files of the delegated modules and decides a push also while the
// anchor has no file.
type Root interface {
	Module
	// EvaluateAbsent decides a push while the anchor has no file, the bootstrap (architecture §9.4).
	EvaluateAbsent(ctx context.Context, in *Input) (*Result, error)
	// Covers reports whether a rule of p, a policy parsed by this module,
	// guards the path (architecture §12.2).
	Covers(p Policy, path string) bool
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

// Person is the pusher as a GitLab user.
type Person struct {
	Username string
	ID       int64
}

// PrincipalResolver resolves the pusher to a person.
type PrincipalResolver interface {
	// Person is zero for a deploy key or an unparsable GL_ID; the status is
	// unknown when GitLab did not answer.
	Person(ctx context.Context) (Person, Status)
}

// VersionValidator is implemented by policies whose acceptance depends on the
// time or on the version on the anchor, for example TAGPOLICY rejects an
// expired key only in a new or changed entry (architecture §11.3). The anchor
// policy is nil when the anchor has no valid version of the file.
type VersionValidator interface {
	ValidateVersion(anchor Policy, now time.Time) []Problem
}

// UsernameLister is implemented by policies with principals. At acceptance the
// core checks only usernames that the anchor version does not have (D28).
type UsernameLister interface {
	Usernames() []string
}

// AccessChecker is what counting admins needs; Validator and Authorizer implement it.
type AccessChecker interface {
	Users(ctx context.Context, usernames []string) (map[string]User, Status)
	AccessLevels(ctx context.Context, userIDs []int64) (map[int64]int, Status)
}

// PolicyChange is a policy file that an update changes, as the core accepted it.
type PolicyChange struct {
	Ref  string
	File string
	// Deleted means the file is absent after the update.
	Deleted bool
}

// UsernamePattern is the format of a GitLab username: NAMESPACE_FORMAT_REGEX
// of GitLab (lib/gitlab/path_regex.rb) without its look-behind, which
// ValidUsername checks separately.
const UsernamePattern = `^(?:[a-zA-Z0-9_.][a-zA-Z0-9_.-]{0,254}[a-zA-Z0-9_-]|[a-zA-Z0-9_])$`

var usernameFormat = regexp.MustCompile(UsernamePattern)

// ValidUsername reports whether s has the format of a GitLab username. Whether
// the user exists is checked online when a policy is accepted.
func ValidUsername(s string) bool {
	return usernameFormat.MatchString(s) && !strings.HasSuffix(s, ".git") && !strings.HasSuffix(s, ".atom")
}
