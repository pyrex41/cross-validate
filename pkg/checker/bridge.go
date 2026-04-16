// Package checker runs the obligation framework against a World.
package checker

import (
	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Config holds checker configuration.
type Config struct {
	// StrictConversions refuses webhook conversions entirely
	// instead of allowing them with an opt-in annotation.
	StrictConversions bool
}

// Check runs all obligation generators against the World and returns diagnostics.
func Check(w *types.World, cfg Config) ([]types.Diagnostic, error) {
	result := CheckWithObligations(w, cfg)
	return result.Diagnostics, nil
}

// CheckWithObligations runs the obligation framework and returns the full result
// including obligation counts and IDs (for audit purposes).
func CheckWithObligations(w *types.World, cfg Config) obligation.RunResult {
	reg := obligation.DefaultRegistry()
	ctx := &obligation.Context{
		World:             w,
		StrictConversions: cfg.StrictConversions,
	}
	return obligation.Run(reg, ctx)
}
