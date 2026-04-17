package trajectory

import (
	"fmt"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Bootstrap is a Category F generator that checks whether resources annotated
// as having a missing required resource are bootstrappable or explicitly accepted.
//
// Absorbs legacy rule R9 (XPC009).
type Bootstrap struct{}

var _ obligation.Generator = Bootstrap{}

func (Bootstrap) Name() string                  { return "trajectory-bootstrap" }
func (Bootstrap) Category() obligation.Category { return obligation.CatTrajectory }

func (Bootstrap) Description() string {
	return `Required resources must be bootstrappable.

Resources annotated with xpc.dev/required-resource-missing: "true" reference
a required resource that may not exist on first reconcile. Unless the bootstrap
gap is explicitly accepted via xpc.dev/accept-bootstrap-gap: "true", this is
an error.

Legacy code: XPC009`
}

// Generate emits one obligation per resource with the required-resource-missing annotation.
func (Bootstrap) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	var obs []obligation.Obligation
	for _, res := range w.Resources {
		res := res // capture for closure
		if res.Annotations["xpc.dev/required-resource-missing"] != "true" {
			continue
		}

		instance := sanitizeInstance(res.Kind + "-" + res.Name)
		obs = append(obs, obligation.Obligation{
			ID:       obligation.MakeID(obligation.CatTrajectory, "trajectory-bootstrap", instance),
			Category: obligation.CatTrajectory,
			Subject:  res.Source,
			Claim: fmt.Sprintf("Resource %s is bootstrappable or has accepted bootstrap gap",
				res.Name),
			Provenance: obligation.Provenance{
				Generator: "trajectory-bootstrap",
				Category:  obligation.CatTrajectory,
				InputHash: obligation.ContentHash(res.Kind + res.Name),
			},
			LegacyCode: "XPC009",
			Discharge: func(ctx *obligation.Context) obligation.Result {
				return dischargeBootstrap(res)
			},
		})
	}

	return obs
}

func dischargeBootstrap(res types.ResourceInfo) obligation.Result {
	if res.Annotations["xpc.dev/accept-bootstrap-gap"] == "true" {
		return obligation.Result{Status: obligation.Satisfied}
	}

	return obligation.Result{
		Status: obligation.Violated,
		Diag: &types.Diagnostic{
			Code:     "XPC009",
			Severity: types.SeverityError,
			Source:   res.Source,
			Message:  fmt.Sprintf("required resource not bootstrappable for %s", res.Name),
			Detail: fmt.Sprintf("%s \"%s\" references a required resource that may not exist on "+
				"first reconcile. The Composition pipeline depends on a resource that isn't produced "+
				"by an earlier step or known to exist at bootstrap time.", res.Kind, res.Name),
			Fix: "Ensure the required resource is produced by an earlier pipeline step, " +
				"or add annotation xpc.dev/accept-bootstrap-gap: \"true\" to acknowledge.",
		},
	}
}
