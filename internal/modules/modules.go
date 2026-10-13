// Package modules is the only place that names concrete policy modules
// (architecture §20). The core and the CLI work with the list it returns.
package modules

import (
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// Settings are the parts of the server config that modules need; the CLI fills them in.
type Settings struct {
	// CommitPresets is commitpolicy_presets of the server config, undecoded.
	CommitPresets  map[string]any
	PatternTimeout time.Duration
	PatternBudget  time.Duration
}

// Default returns the modules in evaluation order: PATHPOLICY first, because
// it guards the files of the other modules (architecture §8).
func Default(Settings) []module.Module {
	return []module.Module{}
}
