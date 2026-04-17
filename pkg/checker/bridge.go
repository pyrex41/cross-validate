// Package checker runs the obligation framework against a World.
//
// Phase 1: the obligation framework is now the primary check path.
// All R1-R11 rules are ported to generators in pkg/obligation/ subpackages.
// The Shen kernel bridge and legacy Go rules are retained in legacy.go
// for reference but are no longer called by Check().
package checker

import (
	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Config holds checker configuration.
type Config struct {
	// KernelPath is the path to the Shen kernel directory.
	// Retained for future Shen integration where the kernel holds
	// the canonical obligation taxonomy.
	KernelPath string

	// ShenBinary is the path to the shen-cl binary.
	// Reserved for future use when Shen becomes the authoritative spec.
	ShenBinary string

	// StrictConversions refuses webhook conversions entirely
	// instead of allowing them with an opt-in annotation.
	StrictConversions bool
}

// Check runs all obligation generators against the World and returns diagnostics.
// This is the primary entry point. All checks come from the obligation framework.
func Check(w *types.World, cfg Config) ([]types.Diagnostic, error) {
	result := CheckWithObligations(w, cfg)
	return result.Diagnostics, nil
}

// CheckWithObligations runs the obligation framework and returns the full result
// including obligation counts and IDs (for audit/proof purposes).
func CheckWithObligations(w *types.World, cfg Config) obligation.RunResult {
	reg := obligation.DefaultRegistry()
	ctx := &obligation.Context{
		World:             w,
		StrictConversions: cfg.StrictConversions,
	}
	return obligation.Run(reg, ctx)
}
