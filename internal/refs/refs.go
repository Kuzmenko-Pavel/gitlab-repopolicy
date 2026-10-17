// Package refs classifies refs as default, protected or other.
// The default branch is known locally; protected branches and tags come from
// the API, at most once each per receive and only when a module asks.
package refs

import (
	"context"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// API is the part of the GitLab client the classifier uses.
type API interface {
	ProtectedBranches(ctx context.Context, projectID int64) ([]string, error)
	ProtectedTags(ctx context.Context, projectID int64) ([]string, error)
}

// Classifier implements module.RefClassifier for one receive. It is not safe
// for concurrent use: a receive is sequential.
type Classifier struct {
	defaultRef string
	projectID  int64
	api        API
	guard      module.Guard

	branches lazyList
	tags     lazyList
}

type lazyList struct {
	loaded   bool
	patterns []string
	status   module.Status
}

// New returns a classifier; defaultRef is "" when the repository has no default branch.
func New(defaultRef string, projectID int64, api API, guard module.Guard) *Classifier {
	return &Classifier{defaultRef: defaultRef, projectID: projectID, api: api, guard: guard}
}

// Default returns the default branch ref.
func (c *Classifier) Default() string { return c.defaultRef }

// Class returns the class of ref.
func (c *Classifier) Class(ctx context.Context, ref string) (module.RefClass, module.Status) {
	if ref != "" && ref == c.defaultRef {
		return module.ClassDefault, module.Confirmed()
	}
	list, name := c.listFor(ref)
	if list == nil {
		return module.ClassOther, module.Confirmed()
	}
	patterns, status := c.load(ctx, list)
	if !status.IsConfirmed() {
		return module.ClassOther, status
	}
	if matchAny(patterns, name) {
		return module.ClassProtected, module.Confirmed()
	}
	return module.ClassOther, module.Confirmed()
}

// ProtectedScope returns the protected class with the protected branches and tags as its set.
func (c *Classifier) ProtectedScope(ctx context.Context) (module.Scope, module.Status) {
	branches, status := c.load(ctx, &c.branches)
	if !status.IsConfirmed() {
		return module.Scope{}, status
	}
	tags, status := c.load(ctx, &c.tags)
	if !status.IsConfirmed() {
		return module.Scope{}, status
	}
	return module.Scope{Class: module.ClassProtected, Key: "gitlab", Protected: func(ref string) bool {
		if name, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			return matchAny(branches, name)
		}
		if name, ok := strings.CutPrefix(ref, "refs/tags/"); ok {
			return matchAny(tags, name)
		}
		return false
	}}, module.Confirmed()
}

func (c *Classifier) listFor(ref string) (*lazyList, string) {
	if name, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		return &c.branches, name
	}
	if name, ok := strings.CutPrefix(ref, "refs/tags/"); ok {
		return &c.tags, name
	}
	return nil, ""
}

func (c *Classifier) load(ctx context.Context, list *lazyList) ([]string, module.Status) {
	if list.loaded {
		return list.patterns, list.status
	}
	list.loaded = true
	if c.guard != nil {
		if status, stop := c.guard(ctx); stop {
			list.status = status
			return nil, status
		}
	}
	var err error
	if list == &c.tags {
		list.patterns, err = c.api.ProtectedTags(ctx, c.projectID)
	} else {
		list.patterns, err = c.api.ProtectedBranches(ctx, c.projectID)
	}
	list.status = StatusOf(err)
	return list.patterns, list.status
}

// StatusOf converts a GitLab call error into a status: budget and deadline are limits.
func StatusOf(err error) module.Status {
	if err == nil {
		return module.Confirmed()
	}
	switch gitlab.KindOf(err) {
	case gitlab.BudgetExceeded, gitlab.Deadline:
		return module.UnknownLimit(err.Error())
	case gitlab.Unauthorized:
		return module.UnknownSetup(err.Error())
	default:
		return module.Unknown(err.Error())
	}
}

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if module.MatchPattern(p, name) {
			return true
		}
	}
	return false
}
