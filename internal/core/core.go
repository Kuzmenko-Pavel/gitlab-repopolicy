// Package core runs one receive: anchor, policy loading, acceptance of new
// policy versions, module evaluation, budgets, mode and aggregation
// (architecture §6-§8, §12, §14). It knows modules only through the contract.
package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/changes"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/hookenv"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/refs"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/report"
)

// Git is the part of gitx the core uses.
type Git interface {
	changes.Git
	HeadRef(ctx context.Context) (string, error)
	Info(ctx context.Context, spec string) (gitx.Info, error)
	Entry(ctx context.Context, commit, path string) (gitx.Info, bool, error)
	Read(ctx context.Context, oid string, max int64) (gitx.Info, []byte, error)
}

// API is the part of the GitLab client the core uses.
type API interface {
	refs.API
	identity.Users
	Calls() int
}

// Breakglass reports a valid break-glass grant for a project (architecture §18.6).
type Breakglass interface {
	Active(projectID int64, now time.Time) (reason string, ok bool)
}

// Auditor receives audit events.
type Auditor interface {
	Write(e audit.Event) error
}

// Deps are the collaborators of a receive.
type Deps struct {
	// Modules are the enabled modules in evaluation order, PATHPOLICY first.
	Modules []module.Module
	Git     Git
	API     API
	// Validator answers online questions of policy acceptance; nil when unavailable.
	Validator  module.Validator
	Audit      Auditor
	Breakglass Breakglass
	Clock      func() time.Time
}

// Settings of a receive from the server config.
type Settings struct {
	Mode  config.Mode
	Total time.Duration
}

// Request is the receive as Gitaly passed it.
type Request struct {
	Env     hookenv.Env
	Updates []module.RefUpdate
}

// Outcome is the decision and what to print.
type Outcome struct {
	Allow  bool
	Stdout string
	Stderr string
	// AuditErr is a failure to write audit events; it does not change the decision.
	AuditErr error
}

// Run decides on one receive. It never returns an error: a failure is a decision.
func Run(ctx context.Context, s Settings, d Deps, req Request) Outcome {
	if s.Mode == config.ModeOff || !req.Env.IsProject() {
		return Outcome{Allow: true}
	}
	ctx, cancel := context.WithTimeout(ctx, s.Total)
	defer cancel()
	r := &receive{settings: s, deps: d, req: req, start: d.Clock()}
	r.run(ctx)
	return r.outcome()
}

// receive is the state of one hook invocation.
type receive struct {
	settings Settings
	deps     Deps
	req      Request
	start    time.Time

	updates    []module.RefUpdate
	defaultRef string
	anchor     string
	policies   []*loaded
	input      *module.Input

	personal *bool

	sections []report.Section
	warnings []report.Section
	notices  []string
	events   []audit.Event
	deny     bool
}

func (r *receive) run(ctx context.Context) {
	defer func() {
		if p := recover(); p != nil {
			r.fail(module.CodeInternalError, fmt.Sprintf("panic: %v", p))
		}
	}()
	for _, u := range r.req.Updates {
		if u.IsBranch() || u.IsTag() {
			r.updates = append(r.updates, u)
		}
	}
	if len(r.updates) == 0 {
		return
	}
	if err := r.loadAnchor(ctx); err != nil {
		r.failErr(ctx, err)
		return
	}
	if r.deps.Breakglass != nil {
		if reason, ok := r.deps.Breakglass.Active(r.req.Env.ProjectID, r.deps.Clock()); ok {
			r.notices = append(r.notices, "break-glass is active for this project: policies are not enforced")
			r.events = append(r.events, r.event(audit.Event{Event: audit.EventBreakglass, Decision: "allow", Reason: reason}))
			return
		}
	}
	if err := r.loadPolicies(ctx); err != nil {
		r.failErr(ctx, err)
		return
	}
	r.input = r.newInput()
	if err := r.accept(ctx); err != nil {
		r.failErr(ctx, err)
		return
	}
	r.evaluate(ctx)
}

// isPersonal resolves the namespace type once; the hook does not check
// personal projects (architecture D25).
func (r *receive) isPersonal(ctx context.Context) bool {
	if r.personal == nil {
		p := identity.IsPersonalProject(ctx, r.deps.API, r.req.Env.ProjectPath)
		r.personal = &p
	}
	return *r.personal
}

// fail records a core violation that rejects the push.
func (r *receive) fail(code, reason string) {
	v := module.Violation{Code: code, Reason: reason, Message: coreMessage(code, r.settings.Total)}
	r.addSection(report.CoreSection, v)
	r.events = append(r.events, r.event(audit.Event{Event: eventFor(code), Decision: "deny", Code: code, Reason: reason}))
}

// failErr maps an error of git, the API or a limit to a core violation.
func (r *receive) failErr(ctx context.Context, err error) {
	switch {
	case errors.Is(err, module.ErrLimitExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.fail(module.CodeLimitExceeded, err.Error())
	default:
		r.fail(module.CodeInternalError, err.Error())
	}
}

func (r *receive) addSection(name string, vs ...module.Violation) {
	r.deny = true
	for i := range r.sections {
		if r.sections[i].Name == name {
			r.sections[i].Violations = append(r.sections[i].Violations, vs...)
			return
		}
	}
	r.sections = append(r.sections, report.Section{Name: name, Violations: vs})
}

func (r *receive) addWarning(name string, vs ...module.Violation) {
	for i := range r.warnings {
		if r.warnings[i].Name == name {
			r.warnings[i].Violations = append(r.warnings[i].Violations, vs...)
			return
		}
	}
	r.warnings = append(r.warnings, report.Section{Name: name, Violations: vs})
}

func coreMessage(code string, total time.Duration) string {
	switch code {
	case module.CodeLimitExceeded:
		return fmt.Sprintf("The push is too large to verify within the limits (time %s, API calls, commits). Split it or ask an administrator.", total)
	case module.CodeInternalError:
		return "An internal error stopped the verification. Administrators are alerted; retry later."
	case module.CodeAuthorizationBackend:
		return "GitLab could not confirm the authorization. Retry later; administrators are alerted."
	}
	return ""
}

func eventFor(code string) string {
	switch code {
	case module.CodeInternalError:
		return audit.EventInternalError
	case module.CodeAuthorizationBackend:
		return audit.EventAuthorizationBackendFailure
	default:
		return audit.EventDecision
	}
}

// event fills the receive fields of an audit event.
func (r *receive) event(e audit.Event) audit.Event {
	env := r.req.Env
	e.ProjectID = env.ProjectID
	e.ProjectPath = env.ProjectPath
	e.Username = env.Principal.Username
	e.GLID = env.Principal.GLID
	if kind, id := env.Principal.Kind(); kind == module.PrincipalUser {
		e.UserID = id
	}
	e.Protocol = env.Protocol
	if e.AnchorRev == "" {
		e.AnchorRev = r.anchor
	}
	if r.deps.API != nil {
		e.APICalls = r.deps.API.Calls()
	}
	e.DurationMS = r.deps.Clock().Sub(r.start).Milliseconds()
	return e
}

// outcome applies the namespace type and the mode and renders the output.
func (r *receive) outcome() Outcome {
	needsNamespace := r.deny || len(r.warnings) > 0 || len(r.notices) > 0
	if needsNamespace && r.personal == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		r.isPersonal(ctx)
		cancel()
	}
	if r.personal != nil && *r.personal {
		return Outcome{Allow: true}
	}
	// An audit failure does not change the decision; selftest checks that the log is writable.
	var auditErr error
	for _, e := range r.events {
		if r.deps.Audit != nil {
			auditErr = errors.Join(auditErr, r.deps.Audit.Write(e))
		}
	}
	if r.settings.Mode == config.ModeAudit {
		return Outcome{Allow: true, AuditErr: auditErr}
	}
	if r.deny {
		who := report.Pusher{Username: r.req.Env.Principal.Username, GLID: r.req.Env.Principal.GLID}
		return Outcome{Allow: false, Stderr: report.Rejection(r.sections, who), AuditErr: auditErr}
	}
	return Outcome{Allow: true, Stdout: report.Warnings(r.warnings, r.notices), AuditErr: auditErr}
}
