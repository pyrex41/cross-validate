package checker

// Import all obligation generator packages so their init() functions
// register generators in the default registry. Required for integration
// tests that call Check() which now routes through the obligation framework.
import (
	_ "github.com/pyrex41/cross-validate-/pkg/obligation/conversion"
	_ "github.com/pyrex41/cross-validate-/pkg/obligation/crossapp"
	_ "github.com/pyrex41/cross-validate-/pkg/obligation/deprecation"
	_ "github.com/pyrex41/cross-validate-/pkg/obligation/refs"
	_ "github.com/pyrex41/cross-validate-/pkg/obligation/secretflow"
	_ "github.com/pyrex41/cross-validate-/pkg/obligation/trajectory"
	_ "github.com/pyrex41/cross-validate-/pkg/obligation/versions"
)
