package engine

import (
	"fmt"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/lockfile"
)

// Preview is what `omnishell apply` would show for a config, and whether
// applying would do anything at all.
type Preview struct {
	// Text is the rendered plan, followed by a warning for every module the
	// config enables that no registered module provides (apply prints those
	// to stderr).
	Text string
	// NeedsApply is false exactly when apply would take its "nothing to do"
	// path: no planned changes and init and rc files that still match the
	// lockfile.
	NeedsApply bool
}

// Preview computes the plan for cfg without touching anything, the way the
// first step of Apply does, and decides with Apply's own rule whether applying
// would change anything. It exists for front ends that show the plan before
// handing over to apply.
func (e Engine) Preview(cfg config.Config, lockPath string) (Preview, error) {
	lock, _, err := lockfile.Load(lockPath)
	if err != nil {
		return Preview{}, err
	}
	plan, err := ComputePlan(e, cfg, lock, false)
	if err != nil {
		return Preview{}, err // a ConfigError propagates unchanged
	}

	var text strings.Builder
	text.WriteString(strings.TrimRight(RenderPlan(plan), "\n"))
	for _, id := range plan.UnknownModules {
		_, _ = fmt.Fprintf(&text, "\nwarning: unknown module %q in config (ignored)", id)
	}
	return Preview{
		Text:       text.String(),
		NeedsApply: plan.HasChanges || e.initOrRCDrift(plan, lock),
	}, nil
}
