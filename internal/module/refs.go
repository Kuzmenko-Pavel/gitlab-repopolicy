package module

import (
	"context"
	"strconv"
	"strings"
)

// RefUpdate is one line of pre-receive stdin.
type RefUpdate struct {
	Old string
	New string
	Ref string
}

// Kind of a ref update.
type Kind int

// Kinds of ref updates.
const (
	Update Kind = iota
	Create
	Delete
)

// IsZero reports whether rev is the null revision of any hash length.
func IsZero(rev string) bool {
	return rev != "" && strings.Trim(rev, "0") == ""
}

// Kind of the update.
func (u RefUpdate) Kind() Kind {
	switch {
	case IsZero(u.Old):
		return Create
	case IsZero(u.New):
		return Delete
	default:
		return Update
	}
}

// IsBranch reports whether the ref is under refs/heads/.
func (u RefUpdate) IsBranch() bool { return strings.HasPrefix(u.Ref, "refs/heads/") }

// IsTag reports whether the ref is under refs/tags/.
func (u RefUpdate) IsTag() bool { return strings.HasPrefix(u.Ref, "refs/tags/") }

// RefClass is the class of a ref.
type RefClass int

// Ref classes.
const (
	ClassOther RefClass = iota
	ClassProtected
	ClassDefault
)

func (c RefClass) String() string {
	switch c {
	case ClassDefault:
		return "default"
	case ClassProtected:
		return "protected"
	default:
		return "other"
	}
}

// Scope describes the class under which changes of a ref are computed.
type Scope struct {
	Class RefClass
	// Key names the protected set for memoisation, for example "gitlab" or a rule's ref list.
	Key string
	// Protected matches the refs of the protected set; for ClassProtected the
	// commits already in the default branch and in these refs are not new.
	Protected func(ref string) bool
	// FirstParent limits commit views to the first-parent chain: commits that
	// enter through the other parents of a merge commit are left out. Paths ignore it.
	FirstParent bool
}

// RefClassifier classifies refs. Default is local; protected needs the API and
// is resolved lazily, once per receive.
type RefClassifier interface {
	// Default returns the default branch ref, or "" without one.
	Default() string
	// Class returns the class of ref.
	Class(ctx context.Context, ref string) (RefClass, Status)
	// ProtectedScope returns the scope of the GitLab protected refs.
	ProtectedScope(ctx context.Context) (Scope, Status)
}

// ChangeSet gives modules three views of a push, computed lazily.
// Lists of commits stop with ErrLimitExceeded when longer than max.
type ChangeSet interface {
	// CandidatePaths is a local superset of Paths for any class of the ref.
	CandidatePaths(ctx context.Context, u RefUpdate) ([]string, error)
	// Paths are the paths changed by the update under scope.
	Paths(ctx context.Context, u RefUpdate, s Scope) ([]string, error)
	// CandidateCommits is a local superset of EnteringCommits for any class.
	CandidateCommits(ctx context.Context, u RefUpdate, max int) ([]string, error)
	// EnteringCommits are the commits that enter the ref with this push under scope.
	EnteringCommits(ctx context.Context, u RefUpdate, s Scope, max int) ([]string, error)
	// NewCommits are the commits not reachable from any branch or tag before the push.
	NewCommits(ctx context.Context, u RefUpdate, max int) ([]string, error)
}

// Object is a git object read from the quarantine or the repository.
type Object struct {
	OID  string
	Type string
	Data []byte
}

// ObjectReader reads objects through the one cat-file process of the receive.
type ObjectReader interface {
	// Read returns the object; it fails with ErrTooLarge above max bytes and with ErrMissing.
	Read(ctx context.Context, oid string, max int) (Object, error)
}

// PrincipalKind tells how the pusher authenticated.
type PrincipalKind int

// Principal kinds.
const (
	PrincipalUnknown PrincipalKind = iota
	PrincipalUser
	PrincipalKey
)

// Principal is the pusher from GL_ID and GL_USERNAME.
type Principal struct {
	GLID     string
	Username string
}

// Kind parses GL_ID: user-<id> or key-<id>.
func (p Principal) Kind() (PrincipalKind, int64) {
	for prefix, kind := range map[string]PrincipalKind{"user-": PrincipalUser, "key-": PrincipalKey} {
		if rest, ok := strings.CutPrefix(p.GLID, prefix); ok {
			id, err := strconv.ParseInt(rest, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != rest {
				return PrincipalUnknown, 0
			}
			return kind, id
		}
	}
	return PrincipalUnknown, 0
}

// Status is the outcome of a question to GitLab: confirmed or unknown with a reason.
// It replaces (bool, error) so that a failure cannot be read as "no".
// The zero value is unknown.
type Status struct {
	confirmed bool
	limit     bool
	setup     bool
	reason    string
}

// Confirmed is the status of an answered question.
func Confirmed() Status { return Status{confirmed: true} }

// Unknown is the status of an unanswered question.
func Unknown(reason string) Status { return Status{reason: reason} }

// UnknownLimit is the status of a question left unanswered because the call
// budget or the time limit of the receive ran out (limit_exceeded).
func UnknownLimit(reason string) Status { return Status{limit: true, reason: reason} }

// UnknownSetup is the status of a question that GitLab will not answer until
// the server is set up right, for example the token is rejected or the
// service account does not see the project: retrying does not help.
func UnknownSetup(reason string) Status { return Status{setup: true, reason: reason} }

// IsSetup reports whether the answer is unknown because of the setup of the server.
func (s Status) IsSetup() bool { return !s.confirmed && s.setup }

// IsLimit reports whether the answer is unknown because a limit ran out.
func (s Status) IsLimit() bool { return !s.confirmed && s.limit }

// IsConfirmed reports whether GitLab answered.
func (s Status) IsConfirmed() bool { return s.confirmed }

// Reason why the answer is unknown.
func (s Status) Reason() string {
	if !s.confirmed && s.reason == "" {
		return "no answer"
	}
	return s.reason
}

// MatchPattern reports whether name matches a GitLab protected branch or tag pattern,
// where "*" matches any sequence of characters, including "/".
func MatchPattern(pattern, name string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == name
	}
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	name = name[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(name, p)
		if i < 0 {
			return false
		}
		name = name[i+len(p):]
	}
	return strings.HasSuffix(name, last)
}
