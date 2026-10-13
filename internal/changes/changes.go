// Package changes computes the views of a push that modules use: changed paths,
// new commits and entering commits, depending on the class of the ref
// (architecture §12). Everything is lazy and memoised for the receive.
package changes

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// MaxPathCommits bounds the commits whose paths are listed (architecture §17:
// 50 000 new commits); above it path views fail with module.ErrLimitExceeded.
const MaxPathCommits = 50_000

// Git is the part of gitx the change set uses.
type Git interface {
	Peel(ctx context.Context, rev string) (string, error)
	DiffNames(ctx context.Context, a, b string) ([]string, error)
	CommitPaths(ctx context.Context, commits []string) ([]string, error)
	RevList(ctx context.Context, include, exclude []string, notBranchesTags bool, max int) ([]string, error)
	Refs(ctx context.Context) ([]string, error)
}

// Set implements module.ChangeSet for one receive.
type Set struct {
	git        Git
	defaultRef string

	mu    sync.Mutex
	memo  map[string]memoEntry
	peels map[string]string
	refs  []string
}

type memoEntry struct {
	list []string
	err  error
}

// New returns a change set; defaultRef is "" when the repository has no default branch.
func New(git Git, defaultRef string) *Set {
	return &Set{git: git, defaultRef: defaultRef, memo: map[string]memoEntry{}, peels: map[string]string{}}
}

func (s *Set) cached(key string, compute func() ([]string, error)) ([]string, error) {
	s.mu.Lock()
	e, ok := s.memo[key]
	s.mu.Unlock()
	if ok {
		return e.list, e.err
	}
	list, err := compute()
	s.mu.Lock()
	s.memo[key] = memoEntry{list: list, err: err}
	s.mu.Unlock()
	return list, err
}

// peel returns the commit a revision of the push points to.
func (s *Set) peel(ctx context.Context, rev string) (string, error) {
	s.mu.Lock()
	c, ok := s.peels[rev]
	s.mu.Unlock()
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
	s.mu.Lock()
	s.peels[rev] = c
	s.mu.Unlock()
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

func (s *Set) revList(ctx context.Context, include, exclude []string, notBranchesTags bool, max int) ([]string, error) {
	commits, err := s.git.RevList(ctx, include, exclude, notBranchesTags, max)
	if errors.Is(err, gitx.ErrLimit) {
		return nil, fmt.Errorf("more than %d commits: %w", max, module.ErrLimitExceeded)
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
	if u.Kind() == module.Delete {
		return nil, nil
	}
	return s.cached(fmt.Sprintf("new %s %d", u.Ref, max), func() ([]string, error) {
		_, newC, err := s.ends(ctx, u)
		if err != nil {
			return nil, err
		}
		return s.revList(ctx, []string{newC}, nil, true, max)
	})
}

// CandidatePaths is a local superset of Paths for every class of the ref:
// the net diff and the paths of new commits on update, the paths of commits
// not in the default branch on create.
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
			commits, err := s.revList(ctx, []string{newC}, s.withDefault(), false, MaxPathCommits)
			if err != nil {
				return nil, err
			}
			return s.git.CommitPaths(ctx, commits)
		}
		diff, err := s.git.DiffNames(ctx, oldC, newC)
		if err != nil {
			return nil, err
		}
		commits, err := s.NewCommits(ctx, u, MaxPathCommits)
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

// Paths are the paths changed by the update under scope: the net diff of the
// trees for default and protected refs, the paths of new commits for other refs.
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
			return s.git.DiffNames(ctx, oldC, newC)
		default:
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
			return s.revList(ctx, []string{newC}, []string{oldC}, false, max)
		}
		return s.revList(ctx, []string{newC}, s.withDefault(), false, max)
	})
}

// EnteringCommits are the commits that enter the ref: old..new on update; on
// create all commits for the default branch, commits not in the default branch
// and the protected set for a protected ref, new commits for other refs.
func (s *Set) EnteringCommits(ctx context.Context, u module.RefUpdate, scope module.Scope, max int) ([]string, error) {
	if u.Kind() == module.Delete {
		return nil, nil
	}
	if scope.Class == module.ClassOther && u.Kind() == module.Create {
		return s.NewCommits(ctx, u, max)
	}
	return s.cached(fmt.Sprintf("entering %s %d %q %d", u.Ref, scope.Class, scope.Key, max), func() ([]string, error) {
		oldC, newC, err := s.ends(ctx, u)
		if err != nil {
			return nil, err
		}
		switch {
		case u.Kind() == module.Update:
			return s.revList(ctx, []string{newC}, []string{oldC}, false, max)
		case scope.Class == module.ClassDefault:
			return s.revList(ctx, []string{newC}, nil, false, max)
		default:
			exclude, err := s.protectedSet(ctx, scope)
			if err != nil {
				return nil, err
			}
			return s.revList(ctx, []string{newC}, exclude, false, max)
		}
	})
}

// protectedSet lists the default branch and the existing refs of the protected set.
func (s *Set) protectedSet(ctx context.Context, scope module.Scope) ([]string, error) {
	s.mu.Lock()
	refs := s.refs
	s.mu.Unlock()
	if refs == nil {
		var err error
		if refs, err = s.git.Refs(ctx); err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.refs = refs
		s.mu.Unlock()
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
