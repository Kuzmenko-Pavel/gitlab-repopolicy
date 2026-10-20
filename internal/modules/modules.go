// Package modules builds the module list of the hook. The
// core works with the list it returns.
package modules

import (
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/branch"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/commit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/path"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/tag"
)

// Settings are the parts of the server config that modules need; the CLI fills them in.
type Settings struct {
	// Presets are the presets of the server config by module name and preset
	// name, undecoded; each module parses its own.
	Presets map[string]map[string]any
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
		Root: path.New(path.NewSettings(s.Presets["pathpolicy"])),
		Modules: []module.Module{
			commit.New(commit.NewSettings(s.Presets["commitpolicy"])),
			tag.New(tag.NewSettings(s.Presets["tagpolicy"])),
			branch.New(branch.NewSettings(s.Presets["branchpolicy"])),
		},
	}
}
