// Package modules builds the module list of the hook. The
// core works with the list it returns.
package modules

import (
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/branch"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/commit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/path"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/tag"
)

// Settings are the parts of the server config that modules need; the CLI fills them in.
type Settings struct {
	// CommitPresets is commitpolicy_presets of the server config, undecoded.
	CommitPresets map[string]any
	// BranchPresets is branchpolicy_presets of the server config, undecoded.
	BranchPresets  map[string]any
	PatternTimeout time.Duration
	PatternBudget  time.Duration
	// Clock measures time budgets.
	Clock func() time.Time
}

// Set is the module list: the trust root and the other modules in evaluation order.
type Set struct {
	// Root is PATHPOLICY; it guards the files of the other modules.
	Root    module.Root
	Modules []module.Module
}

// All returns every module of the set, Root first.
func (s Set) All() []module.Module {
	if s.Root == nil {
		return s.Modules
	}
	return append([]module.Module{s.Root}, s.Modules...)
}

// Enabled returns the set without the modules that enabled rejects.
func (s Set) Enabled(enabled func(name string) bool) Set {
	var out Set
	if s.Root != nil && enabled(s.Root.Name()) {
		out.Root = s.Root
	}
	for _, m := range s.Modules {
		if enabled(m.Name()) {
			out.Modules = append(out.Modules, m)
		}
	}
	return out
}

// Default returns the modules of gitlab-repopolicy.
func Default(s Settings) Set {
	return Set{
		Root: path.New(),
		Modules: []module.Module{
			commit.New(commit.NewSettings(s.CommitPresets, s.PatternTimeout, s.PatternBudget, s.Clock)),
			tag.New(),
			branch.New(branch.NewSettings(s.BranchPresets)),
		},
	}
}
