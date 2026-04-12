package refs

import "github.com/pyrex41/cross-validate-/pkg/obligation"

func init() {
	obligation.RegisterDefault(CompXRDRef{})
	obligation.RegisterDefault(PipelineFnRef{})
	obligation.RegisterDefault(PatchCompat{})
}
