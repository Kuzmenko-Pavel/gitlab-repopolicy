package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// loaded is a module with the state of its file on the anchor.
type loaded struct {
	mod module.Module
	// present: the anchor has the file; then either policy or invalid is set.
	present bool
	blob    string
	policy  module.Policy
	invalid error
}

// loadAnchor finds the default branch and its commit before the push.
// An unborn default branch or an empty repository has no anchor.
func (r *receive) loadAnchor(ctx context.Context) error {
	ref, err := r.deps.Git.HeadRef(ctx)
	if err != nil {
		return fmt.Errorf("anchor: %w", err)
	}
	r.defaultRef = ref
	info, err := r.deps.Git.Info(ctx, ref)
	if errors.Is(err, gitx.ErrMissing) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("anchor: %w", err)
	}
	if info.Type != "commit" {
		return fmt.Errorf("anchor: %s points to a %s", ref, info.Type)
	}
	r.anchor = info.OID
	return nil
}

// loadPolicies reads the file of every module from the anchor (architecture §12.1).
func (r *receive) loadPolicies(ctx context.Context) error {
	for _, m := range r.deps.Modules {
		l := &loaded{mod: m}
		r.policies = append(r.policies, l)
		if r.anchor == "" {
			continue
		}
		file, err := r.readPolicy(ctx, r.anchor, m)
		if err != nil {
			return err
		}
		if !file.present {
			continue
		}
		l.present, l.blob = true, file.blob
		if file.invalid != nil {
			l.invalid = file.invalid
			continue
		}
		l.policy, l.invalid = parse(m, file.data)
	}
	return nil
}

// policyFile is a policy file read from a commit.
type policyFile struct {
	present bool
	blob    string
	data    []byte
	// invalid is set for a file that cannot be a policy: wrong mode or too large.
	invalid error
}

// readPolicy reads <commit>:<file>: mode and size are checked before the
// content is read; a symlink is never followed.
func (r *receive) readPolicy(ctx context.Context, commit string, m module.Module) (policyFile, error) {
	entry, ok, err := r.deps.Git.Entry(ctx, commit, m.File())
	if err != nil {
		return policyFile{}, fmt.Errorf("read %s: %w", m.File(), err)
	}
	if !ok {
		return policyFile{}, nil
	}
	f := policyFile{present: true, blob: entry.OID}
	switch entry.Mode {
	case gitx.ModeFile, gitx.ModeExecutable:
	case gitx.ModeTree:
		f.invalid = fmt.Errorf("%s is a directory, not a file", m.File())
		return f, nil
	case gitx.ModeSymlink:
		f.invalid = fmt.Errorf("%s is a symbolic link, not a file", m.File())
		return f, nil
	case gitx.ModeGitlink:
		f.invalid = fmt.Errorf("%s is a submodule, not a file", m.File())
		return f, nil
	default:
		f.invalid = fmt.Errorf("%s has mode %s, not a regular file", m.File(), entry.Mode)
		return f, nil
	}
	limit := int64(module.MaxPolicySize(m))
	if entry.Size > limit {
		f.invalid = fmt.Errorf("%s is %d bytes, the limit is %d", m.File(), entry.Size, limit)
		return f, nil
	}
	_, data, err := r.deps.Git.Read(ctx, entry.OID, limit)
	if err != nil {
		return policyFile{}, fmt.Errorf("read %s: %w", m.File(), err)
	}
	f.data = data
	return f, nil
}

// parse calls Module.Parse; a panic in a parser is an invalid policy, not a crash.
func parse(m module.Module, data []byte) (p module.Policy, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			p, err = nil, fmt.Errorf("parser failed: %v", rec)
		}
	}()
	p, err = m.Parse(data)
	if err == nil && p == nil {
		err = errors.New("parser returned no policy")
	}
	return p, err
}

func (r *receive) policyFiles() []string {
	files := make([]string, 0, len(r.deps.Modules))
	for _, m := range r.deps.Modules {
		files = append(files, m.File())
	}
	return files
}
