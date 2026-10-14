// Package modules is the only place that names concrete policy modules
// (architecture §20). The core and the CLI work with the list it returns.
package modules

import (
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/commit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/path"
)

// Settings are the parts of the server config that modules need; the CLI fills them in.
type Settings struct {
	// CommitPresets is commitpolicy_presets of the server config, undecoded.
	CommitPresets  map[string]any
	PatternTimeout time.Duration
	PatternBudget  time.Duration
	// Clock measures time budgets.
	Clock func() time.Time
}

// Default returns the modules in evaluation order: PATHPOLICY first, because
// it guards the files of the other modules (architecture §8).
func Default(s Settings) []module.Module {
	return []module.Module{
		path.New(),
		commit.New(commit.NewSettings(s.CommitPresets, s.PatternTimeout, s.PatternBudget, s.Clock)),
	}
}
