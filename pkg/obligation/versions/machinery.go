package versions

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// MachineryPlacement is a Category C generator that checks whether resources
// matching a v2 XRD use the v2 machinery field placement (under spec.crossplane)
// instead of top-level v1-style fields.
//
// Absorbs legacy rule R8 (XPC008).
type MachineryPlacement struct{}

var _ obligation.Generator = MachineryPlacement{}

func (MachineryPlacement) Name() string                  { return "crossplane-machinery-placement" }
func (MachineryPlacement) Category() obligation.Category { return obligation.CatVersionCoherence }

func (MachineryPlacement) Description() string {
	return `Resources matching a v2 XRD must use spec.crossplane for machinery fields.

In Crossplane v2, machinery fields like publishConnectionDetailsTo,
compositionRef, etc. moved from spec top-level to spec.crossplane.
Resources that still use top-level placement will fail validation.

Fix: Move machinery fields under spec.crossplane.

Legacy code: XPC008`
}

// v1MachineryFields lists the spec-level fields that moved under spec.crossplane in v2.
var v1MachineryFields = []string{
	"publishConnectionDetailsTo",
	"writeConnectionSecretToRef",
	"compositionRef",
	"compositionSelector",
	"compositionRevisionRef",
	"compositionRevisionSelector",
	"compositionUpdatePolicy",
}

// Generate emits one obligation per resource that matches a v2 XRD.
func (MachineryPlacement) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	// Build XRD lookup: group/kind -> apiVersion
	xrdAPIVersionMap := make(map[string]string)
	for _, xrd := range w.XRDs {
		key := xrd.Group + "/" + xrd.Kind
		xrdAPIVersionMap[key] = xrd.APIVersion
	}

	var obs []obligation.Obligation
	for _, res := range w.Resources {
		res := res // capture for closure
		parts := strings.SplitN(res.APIVersion, "/", 2)
		if len(parts) != 2 {
			continue
		}
		group := parts[0]
		key := group + "/" + res.Kind
		xrdAPIVer, ok := xrdAPIVersionMap[key]
		if !ok {
			continue
		}
		if xrdAPIVer != "apiextensions.crossplane.io/v2" {
			continue
		}

		instance := sanitizeInstance(res.Kind + "." + res.Name)

		obs = append(obs, obligation.Obligation{
			ID:       obligation.MakeID(obligation.CatVersionCoherence, "crossplane-machinery-placement", instance),
			Category: obligation.CatVersionCoherence,
			Subject:  res.Source,
			Claim: fmt.Sprintf("Resource %s/%s uses v2 machinery placement (spec.crossplane)",
				res.Kind, res.Name),
			Provenance: obligation.Provenance{
				Generator: "crossplane-machinery-placement",
				Category:  obligation.CatVersionCoherence,
				InputHash: obligation.ContentHash(res.Kind + res.Name + res.APIVersion),
			},
			LegacyCode: "XPC008",
			Discharge: func(ctx *obligation.Context) obligation.Result {
				return dischargeMachineryPlacement(res)
			},
		})
	}

	return obs
}

func dischargeMachineryPlacement(res types.ResourceInfo) obligation.Result {
	spec, _ := res.Raw["spec"].(map[string]interface{})
	if spec == nil {
		return obligation.Result{Status: obligation.Satisfied}
	}

	for _, field := range v1MachineryFields {
		if _, exists := spec[field]; exists {
			if _, hasCrossplane := spec["crossplane"]; !hasCrossplane {
				return obligation.Result{
					Status: obligation.Violated,
					Diag: &types.Diagnostic{
						Code:     "XPC008",
						Severity: types.SeverityError,
						Source:   res.Source,
						Message: fmt.Sprintf("Resource %s uses v1-style machinery fields with a v2 XRD",
							res.Name),
						Detail: fmt.Sprintf("%s \"%s\" uses top-level machinery field \"%s\" "+
							"but its XRD uses apiextensions.crossplane.io/v2. In v2, these fields "+
							"must be under spec.crossplane.", res.Kind, res.Name, field),
						Fix: "Move machinery fields under spec.crossplane. See the Crossplane v2 migration guide.",
					},
				}
			}
		}
	}

	return obligation.Result{Status: obligation.Satisfied}
}
