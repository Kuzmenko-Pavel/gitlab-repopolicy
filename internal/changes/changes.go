// Package changes computes the views of a push that modules use: changed paths,
// new commits and entering commits, depending on the class of the ref.
// Everything is lazy and memoised for the receive.
package changes

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// MaxPathCommits bounds the commits whose paths are listed; above it path views
// fail with module.ErrLimitExceeded.
const MaxPathCommits = 50_000

// Git is the part of gitx the change set uses.
type Git interface {
	Peel(ctx context.Context, rev string) (string, error)
	DiffNames(ctx context.Context, a, b string) ([]string, error)
	CommitPaths(ctx context.Context, commits []string) ([]string, error)
	RevList(ctx context.Context, o gitx.RevListOptions) ([]string, error)
	Refs(ctx context.Context) ([]string, error)
	CommitChanges(ctx context.Context, commits []string) ([]gitx.Commit, error)
	Entries(ctx context.Context, commit string, paths []string) (map[string]gitx.Info, error)
}

// Set implements module.ChangeSet for one receive. It is not safe for
// concurrent use: a receive is sequential.
type Set struct {
	git        Git
	defaultRef string

	memo    map[string]memoEntry
	changes map[string]changesEntry
	peels   map[string]string
	refs    []string
}

type changesEntry struct {
	list []gitx.Commit
	err  error
}

type memoEntry struct {
	list []string
	err  error
}

// New returns a change set; defaultRef is "" when the repository has no default branch.
func New(git Git, defaultRef string) *Set {
	return &Set{git: git, defaultRef: defaultRef, memo: map[string]memoEntry{}, changes: map[string]changesEntry{}, peels: map[string]string{}}
}

// commitChanges lists the changes of the commits once per key: every rule
// of a ref asks for the same commits.
func (s *Set) commitChanges(ctx context.Context, key string, commits func() ([]string, error)) ([]gitx.Commit, error) {
	if e, ok := s.changes[key]; ok {
		return e.list, e.err
	}
	list, err := commits()
	var changes []gitx.Commit
	if err == nil {
		changes, err = s.git.CommitChanges(ctx, list)
	}
	s.changes[key] = changesEntry{list: changes, err: err}
	return changes, err
}

func (s *Set) cached(key string, compute func() ([]string, error)) ([]string, error) {
	if e, ok := s.memo[key]; ok {
		return e.list, e.err
	}
	list, err := compute()
	s.memo[key] = memoEntry{list: list, err: err}
	return list, err
}

// peel returns the commit a revision of the push points to.
func (s *Set) peel(ctx context.Context, rev string) (string, error) {
	c, ok := s.peels[rev]
	if ok {
		return c, nil
	}
	c, err := s.git.Peel(ctx, rev)
	if errors.Is(err, gitx.ErrNotCommit) {
		return "", fmt.Errorf("%s: %w", rev, module.ErrNotCommit)
	}
	if err != nil {
		return "", err
	}
	s.peels[rev] = c
	return c, nil
}

func (s *Set) ends(ctx context.Context, u module.RefUpdate) (oldC, newC string, err error) {
	if newC, err = s.peel(ctx, u.New); err != nil {
		return "", "", err
	}
	if u.Kind() == module.Update {
		if oldC, err = s.peel(ctx, u.Old); err != nil {
			return "", "", err
		}
	}
	return oldC, newC, nil
}

func (s *Set) revList(ctx context.Context, o gitx.RevListOptions) ([]string, error) {
	commits, err := s.git.RevList(ctx, o)
	if errors.Is(err, gitx.ErrLimit) {
		return nil, fmt.Errorf("more than %d commits: %w", o.Max, module.ErrLimitExceeded)
	}
	return commits, err
}

func (s *Set) withDefault() []string {
	if s.defaultRef == "" {
		return nil
	}
	return []string{s.defaultRef}
}

// NewCommits are the commits not reachable from any branch or tag before the push.
func (s *Set) NewCommits(ctx context.Context, u module.RefUpdate, max int) ([]string, error) {
	return s.newCommits(ctx, u, false, max)
}

func (s *Set) newCommits(ctx context.Context, u module.RefUpdate, firstParent bool, max int) ([]string, error) {
	if u.Kind() == module.Delete {
		return nil, nil
	}
	return s.cached(fmt.Sprintf("new %s %t %d", u.Ref, firstParent, max), func() ([]string, error) {
		_, newC, err := s.ends(ctx, u)
		if err != nil {
			return nil, err
		}
		return s.revList(ctx, gitx.RevListOptions{Include: []string{newC}, NotBranchesTags: true, FirstParent: firstParent, Max: max})
	})
}

// CandidatePaths is a local superset of Paths for every class of the ref:
// the net diff and the paths of every commit of old..new on update, the
// paths of commits not in the default branch on create.
func (s *Set) CandidatePaths(ctx context.Context, u module.RefUpdate) ([]string, error) {
	if u.Kind() == module.Delete {
		return nil, nil
	}
	if u.Ref == s.defaultRef {
		return s.Paths(ctx, u, module.Scope{Class: module.ClassDefault})
	}
	return s.cached("candidate-paths "+u.Ref, func() ([]string, error) {
		oldC, newC, err := s.ends(ctx, u)
		if err != nil {
			return nil, err
		}
		if u.Kind() == module.Create {
			commits, err := s.revList(ctx, gitx.RevListOptions{Include: []string{newC}, Exclude: s.withDefault(), Max: MaxPathCommits})
			if err != nil {
				return nil, err
			}
			return s.git.CommitPaths(ctx, commits)
		}
		diff, err := s.git.DiffNames(ctx, oldC, newC)
		if err != nil {
			return nil, err
		}
		commits, err := s.CandidateCommits(ctx, u, MaxPathCommits)
		if err != nil {
			return nil, err
		}
		paths, err := s.git.CommitPaths(ctx, commits)
		if err != nil {
			return nil, err
		}
		return union(diff, paths), nil
	})
}

// Paths are the paths changed by the update under scope. For default and
// protected refs they are the net diff of the trees and the paths of every
// commit that enters the ref: a change reverted later in the same push still
// counts, and the net diff keeps a rollback by a force push. For other refs
// they are the paths of new commits.
func (s *Set) Paths(ctx context.Context, u module.RefUpdate, scope module.Scope) ([]string, error) {
	if u.Kind() == module.Delete {
		return nil, nil
	}
	return s.cached(fmt.Sprintf("paths %s %d %q", u.Ref, scope.Class, scope.Key), func() ([]string, error) {
		oldC, newC, err := s.ends(ctx, u)
		if err != nil {
			return nil, err
		}
		var commits []string
		switch {
		case scope.Class == module.ClassOther:
			// Paths of new commits only: a rebase on a fresh default branch does not
			// attribute other people's changes to the pusher.
			commits, err = s.NewCommits(ctx, u, MaxPathCommits)
		case u.Kind() == module.Update:
			diff, err := s.git.DiffNames(ctx, oldC, newC)
			if err != nil {
				return nil, err
			}
			scope.FirstParent = false
			if commits, err = s.EnteringCommits(ctx, u, scope, MaxPathCommits); err != nil {
				return nil, err
			}
			paths, err := s.git.CommitPaths(ctx, commits)
			if err != nil {
				return nil, err
			}
			return union(diff, paths), nil
		default:
			scope.FirstParent = false
			commits, err = s.EnteringCommits(ctx, u, scope, MaxPathCommits)
		}
		if err != nil {
			return nil, err
		}
		return s.git.CommitPaths(ctx, commits)
	})
}

// CandidateCommits is a local superset of EnteringCommits for every class.
func (s *Set) CandidateCommits(ctx context.Context, u module.RefUpdate, max int) ([]string, error) {
	if u.Kind() == module.Delete {
		return nil, nil
	}
	return s.cached(fmt.Sprintf("candidate-commits %s %d", u.Ref, max), func() ([]string, error) {
		oldC, newC, err := s.ends(ctx, u)
		if err != nil {
			return nil, err
		}
		if u.Kind() == module.Update {
			return s.revList(ctx, gitx.RevListOptions{Include: []string{newC}, Exclude: []string{oldC}, Max: max})
		}
		return s.revList(ctx, gitx.RevListOptions{Include: []string{newC}, Exclude: s.withDefault(), Max: max})
	})
}

// EnteringCommits are the commits that enter the ref. For the default branch
// and a protected ref: old..new on update; on create all commits for the
// default branch, commits not in the default branch and the protected set for
// a protected ref. For other refs only commits that no branch or tag has yet:
// merging or rebasing onto the default branch brings commits that are already
// in the repository, and their history is not the pusher's.
func (s *Set) EnteringCommits(ctx context.Context, u module.RefUpdate, scope module.Scope, max int) ([]string, error) {
	if u.Kind() == module.Delete {
		return nil, nil
	}
	if scope.Class == module.ClassOther && u.Kind() == module.Create {
		return s.newCommits(ctx, u, scope.FirstParent, max)
	}
	key := fmt.Sprintf("entering %s %d %q %t %d", u.Ref, scope.Class, scope.Key, scope.FirstParent, max)
	return s.cached(key, func() ([]string, error) {
		oldC, newC, err := s.ends(ctx, u)
		if err != nil {
			return nil, err
		}
		o := gitx.RevListOptions{Include: []string{newC}, FirstParent: scope.FirstParent, Max: max}
		switch {
		case u.Kind() == module.Update:
			o.Exclude = []string{oldC}
			o.NotBranchesTags = scope.Class == module.ClassOther
		case scope.Class == module.ClassDefault:
		default:
			if o.Exclude, err = s.protectedSet(ctx, scope); err != nil {
				return nil, err
			}
		}
		return s.revList(ctx, o)
	})
}

// NewPathChanges are the paths that the new commits change, with their states.
func (s *Set) NewPathChanges(ctx context.Context, u module.RefUpdate) ([]module.PathChange, error) {
	commits, err := s.commitChanges(ctx, "new "+u.Ref, func() ([]string, error) { return s.NewCommits(ctx, u, MaxPathCommits) })
	if err != nil {
		return nil, err
	}
	var out []module.PathChange
	for _, c := range commits {
		for _, ch := range c.Changes {
			out = append(out, module.PathChange{Commit: c.OID, Path: ch.Path, State: module.PathState(ch.To)})
		}
	}
	return out, nil
}

// KnownStates are the states of paths in the default branch before the push
// and, for an update, in the old tip and in every commit of anchor..old. A
// commit that does not change a path has the state of one of its parents: a
// parent in anchor..old is counted on its own, and a commit with a parent
// outside the range, or with none, is read as it is. No state comes from a
// commit outside the branch.
func (s *Set) KnownStates(ctx context.Context, u module.RefUpdate, paths []string) (map[string][]module.PathState, error) {
	out := map[string][]module.PathState{}
	add := func(p string, st module.PathState) {
		if !slices.Contains(out[p], st) {
			out[p] = append(out[p], st)
		}
	}
	tip := func(commit string) error {
		entries, err := s.git.Entries(ctx, commit, paths)
		if err != nil {
			return err
		}
		for _, p := range paths {
			e := entries[p]
			add(p, module.PathState{Mode: e.Mode, OID: e.OID})
		}
		return nil
	}
	anchor := ""
	if s.defaultRef != "" {
		// A repository without a default branch yet has no anchor; any other
		// error stops the check.
		c, err := s.git.Peel(ctx, s.defaultRef)
		switch {
		case errors.Is(err, gitx.ErrMissing):
		case err != nil:
			return nil, err
		default:
			anchor = c
			if err := tip(anchor); err != nil {
				return nil, err
			}
		}
	}
	if u.Kind() != module.Update {
		return out, nil
	}
	oldC, err := s.peel(ctx, u.Old)
	if err != nil {
		return nil, err
	}
	if err := tip(oldC); err != nil {
		return nil, err
	}
	branch, err := s.cached("branch "+u.Ref, func() ([]string, error) {
		o := gitx.RevListOptions{Include: []string{oldC}, Max: MaxPathCommits}
		if anchor != "" {
			o.Exclude = []string{anchor}
		}
		return s.revList(ctx, o)
	})
	if err != nil {
		return nil, err
	}
	commits, err := s.commitChanges(ctx, "branch "+u.Ref, func() ([]string, error) { return branch, nil })
	if err != nil {
		return nil, err
	}
	inBranch := make(map[string]bool, len(branch))
	for _, c := range branch {
		inBranch[c] = true
	}
	for _, c := range commits {
		for _, ch := range c.Changes {
			if slices.Contains(paths, ch.Path) {
				add(ch.Path, module.PathState(ch.To))
			}
		}
		if len(c.Parents) > 0 && !slices.ContainsFunc(c.Parents, func(p string) bool { return !inBranch[p] }) {
			continue
		}
		if err := tip(c.OID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// protectedSet lists the default branch and the existing refs of the protected set.
func (s *Set) protectedSet(ctx context.Context, scope module.Scope) ([]string, error) {
	refs := s.refs
	if refs == nil {
		var err error
		if refs, err = s.git.Refs(ctx); err != nil {
			return nil, err
		}
		s.refs = refs
	}
	exclude := s.withDefault()
	for _, r := range refs {
		if r != s.defaultRef && scope.Protected != nil && scope.Protected(r) {
			exclude = append(exclude, r)
		}
	}
	return exclude, nil
}

func union(a, b []string) []string {
	out := slices.Clone(a)
	seen := make(map[string]bool, len(a)+len(b))
	for _, p := range a {
		seen[p] = true
	}
	for _, p := range b {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
