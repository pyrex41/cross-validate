package versions

import "github.com/pyrex41/cross-validate-/pkg/obligation"

func init() {
	obligation.RegisterDefault(VersionCoherence{})
	obligation.RegisterDefault(MachineryPlacement{})
}
