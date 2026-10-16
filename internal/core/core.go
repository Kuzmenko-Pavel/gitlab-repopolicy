// Package core runs one receive: anchor, policy loading, acceptance of new
// policy versions, module evaluation, budgets, mode and aggregation. It knows
// modules only through the contract; PATHPOLICY comes as the trust root.
package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/hookenv"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/refs"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/report"
)

// API is the part of the GitLab client the core uses.
type API interface {
	refs.API
	identity.API
	identity.AuthzAPI
	ProjectAccess(ctx context.Context, projectID int64) (int, error)
	Calls() int
}

// Breakglass reports a valid break-glass grant for a project.
type Breakglass interface {
	Active(projectID int64, now time.Time) (reason string, ok bool)
}

// Auditor receives audit events.
type Auditor interface {
	Write(e audit.Event) error
}

// Deps are the collaborators of a receive.
type Deps struct {
	// Root is PATHPOLICY, nil when it is disabled. It is evaluated first.
	Root module.Root
	// Modules are the other enabled modules in evaluation order.
	Modules    []module.Module
	Git        *gitx.Repo
	API        API
	Audit      Auditor
	Breakglass Breakglass
	Clock      func() time.Time
}

// Settings of a receive from the server config.
type Settings struct {
	Mode  config.Mode
	Total time.Duration
	// Help is printed in denials: who to ask and where the rules are explained.
	Help config.Help
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
	// ReceiveID is the receive_id of the events, for an event written after Run.
	ReceiveID string
	// Touched is false when no rule of any module applied to the push.
	Touched bool
}

// Run decides on one receive. It never returns an error: a failure is a decision.
func Run(ctx context.Context, s Settings, d Deps, req Request) Outcome {
	if s.Mode == config.ModeOff || !req.Env.IsProject() {
		return Outcome{Allow: true}
	}
	ctx, cancel := context.WithTimeout(ctx, s.Total)
	defer cancel()
	r := &receive{settings: s, deps: d, req: req, start: d.Clock(), id: rand.Text()}
	r.run(ctx)
	return r.outcome()
}

// receive is the state of one hook invocation.
type receive struct {
	settings Settings
	deps     Deps
	req      Request
	start    time.Time
	// id is the receive_id of the events.
	id string

	updates    []module.RefUpdate
	defaultRef string
	anchor     string
	policies   []*loaded
	// root is the entry of Deps.Root in policies, nil without it.
	root *loaded
	// entryCache holds the policy file entries by commit.
	entryCache map[string]map[string]gitx.Info
	input      *module.Input

	personal *bool
	// serviceAccount is the outcome of the check of the service account in the project.
	serviceAccount *module.Status
	authz          *identity.Authorizer

	sections []report.Section
	warnings []report.Section
	notices  []string
	events   []audit.Event
	deny     bool
}

func (r *receive) run(ctx context.Context) {
	defer func() {
		if p := recover(); p != nil {
			r.fail(module.CodeInternalError, nil, fmt.Sprintf("panic: %v", p))
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
// personal projects.
func (r *receive) isPersonal(ctx context.Context) bool {
	if r.personal == nil {
		p := identity.IsPersonalProject(ctx, r.deps.API, r.req.Env.ProjectPath)
		r.personal = &p
	}
	return *r.personal
}

// fail records a core violation that rejects the push.
func (r *receive) fail(code string, err error, reason string) {
	r.addSection(report.CoreSection, r.coreViolation(code, err, reason))
	r.events = append(r.events, r.event(audit.Event{Event: eventFor(code), Decision: "deny", Code: code, Reason: reason}))
}

// failErr maps an error of git, the API or a limit to a core violation.
func (r *receive) failErr(ctx context.Context, err error) {
	switch {
	case errors.Is(err, module.ErrLimitExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.fail(module.CodeLimitExceeded, err, err.Error())
	default:
		r.fail(module.CodeInternalError, err, err.Error())
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

// coreViolation is a denial of the core: a limit of the push or an internal
// error. Neither is the fault of the pusher; reason goes only to audit.
func (r *receive) coreViolation(code string, err error, reason string) module.Violation {
	v := module.Violation{Code: code, Reason: reason}
	switch code {
	case module.CodeLimitExceeded:
		limit := fmt.Sprintf("the time of a push (timeouts.total %s)", r.settings.Total)
		hint := "if the push is large, push it in parts; if it is small, the limit is too low on the server"
		if errors.Is(err, module.ErrLimitExceeded) {
			limit = strings.TrimSuffix(err.Error(), ": "+module.ErrLimitExceeded.Error()) + " in one ref"
			hint = "push the branch in parts, for example git push origin <commit>:refs/heads/<branch>, then the rest"
		}
		v.Facts = []string{"limit reached: " + limit}
		v.Hint = hint
		v.Summary = "the check did not finish within the server limits (" + limit + "); the push is rejected"
	default:
		v.Facts = []string{"this is a problem of the server, not of your change"}
		v.Hint = "retry once; if it repeats, report this id: " + r.id
		v.Summary = "internal error: the push was not checked and is rejected; retry once, if it repeats report id " + r.id
	}
	return v
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
	e.ReceiveID = r.id
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
		if r.deny && r.settings.Mode == config.ModeEnforce && acceptedChange(e.Event) {
			// The push is rejected: the policy change did not happen.
			continue
		}
		if r.deps.Audit != nil {
			auditErr = errors.Join(auditErr, r.deps.Audit.Write(e))
		}
	}
	out := Outcome{Allow: true, AuditErr: auditErr, ReceiveID: r.id, Touched: len(r.events) > 0}
	switch {
	case r.settings.Mode == config.ModeAudit:
	case r.deny:
		out.Allow, out.Stderr = false, report.Rejection(r.sections, r.reportContext())
	default:
		out.Stdout = report.Warnings(r.warnings, r.notices, r.reportContext())
	}
	return out
}

// reportContext is the frame of the output: the rules on the anchor, the
// pusher and where to get help.
func (r *receive) reportContext() report.Context {
	env := r.req.Env
	c := report.Context{
		Pusher:  report.Pusher{Username: env.Principal.Username, GLID: env.Principal.GLID, Protocol: env.Protocol},
		Branch:  strings.TrimPrefix(r.defaultRef, "refs/heads/"),
		Anchor:  r.anchor,
		Contact: r.settings.Help.Contact,
		DocsURL: r.settings.Help.DocsURL,
	}
	for _, l := range r.policies {
		if l.present {
			c.Files = append(c.Files, l.mod.File())
		}
	}
	return c
}

// acceptedChange reports the events of a policy change that the push makes.
func acceptedChange(event string) bool {
	switch event {
	case audit.EventPolicyChange, audit.EventPolicyBootstrap, audit.EventPolicyRecovery:
		return true
	}
	return false
}
