// Package conversion implements Category J (conversion-cost) obligation generators.
package conversion

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// CostOptIn is a Category J generator that checks whether resources written at
// a non-storage version with webhook conversion have properly opted in.
//
// Absorbs legacy rule R2 (XPC002).
type CostOptIn struct{}

var _ obligation.Generator = CostOptIn{}

func (CostOptIn) Name() string                  { return "conversion-cost-opt-in" }
func (CostOptIn) Category() obligation.Category { return obligation.CatConversionCost }

func (CostOptIn) Description() string {
	return `Resources written at a non-storage version where the CRD uses webhook conversion must opt in.

Writing a resource at a non-storage version causes the API server to invoke
a conversion webhook on every read/write, adding latency and a single point
of failure. Either re-author the resource at the storage version, or
acknowledge the cost with an annotation.

Fix: Re-author at the storage version, or add annotation
xpc.dev/accept-conversion-webhook: "true".

Legacy code: XPC002`
}

// Generate emits one obligation per resource written at a non-storage version
// where the CRD uses webhook conversion.
func (CostOptIn) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	// Build CRD lookup: group/kind -> *CRDInfo
	crdMap := make(map[string]*types.CRDInfo)
	for i := range w.CRDs {
		key := w.CRDs[i].Group + "/" + w.CRDs[i].Kind
		crdMap[key] = &w.CRDs[i]
	}

	var obs []obligation.Obligation
	for _, res := range w.Resources {
		res := res // capture for closure
		parts := strings.SplitN(res.APIVersion, "/", 2)
		if len(parts) != 2 {
			continue
		}
		group, version := parts[0], parts[1]

		key := group + "/" + res.Kind
		crd, ok := crdMap[key]
		if !ok {
			continue
		}

		if crd.Conversion.CostClass != types.CostClassWebhook {
			continue
		}

		storageVersion := crd.StorageVersion()
		if version == storageVersion {
			continue
		}

		crdCopy := crd // capture pointer for closure
		instance := sanitizeInstance(res.Kind + "." + res.Name)

		obs = append(obs, obligation.Obligation{
			ID:       obligation.MakeID(obligation.CatConversionCost, "conversion-cost-opt-in", instance),
			Category: obligation.CatConversionCost,
			Subject:  res.Source,
			Claim: fmt.Sprintf("Resource %s/%s at version %s acknowledges webhook conversion cost",
				res.Kind, res.Name, version),
			Provenance: obligation.Provenance{
				Generator: "conversion-cost-opt-in",
				Category:  obligation.CatConversionCost,
				InputHash: obligation.ContentHash(res.Kind + res.Name + res.APIVersion),
			},
			LegacyCode: "XPC002",
			Discharge: func(ctx *obligation.Context) obligation.Result {
				return dischargeCostOptIn(res, crdCopy, group, version, ctx.StrictConversions)
			},
		})
	}

	return obs
}

func dischargeCostOptIn(res types.ResourceInfo, crd *types.CRDInfo, group, version string, strictConversions bool) obligation.Result {
	storageVersion := crd.StorageVersion()

	if strictConversions {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC002",
				Severity: types.SeverityError,
				Source:   res.Source,
				Message:  "webhook conversion not acknowledged",
				Detail: fmt.Sprintf("This resource is written at version %s, but the storage version "+
					"of CRD %s.%s is %s. Reading or writing this resource will invoke a conversion "+
					"webhook on every request, which is a network round-trip and a single point of failure.",
					version, crd.Group, crd.Kind, storageVersion),
				Fix: fmt.Sprintf("Re-author the resource at the storage version %s:\n"+
					"  apiVersion: %s/%s",
					storageVersion, group, storageVersion),
				Related: []types.SourceLocation{crd.Source},
			},
		}
	}

	if res.Annotations["xpc.dev/accept-conversion-webhook"] == "true" {
		return obligation.Result{Status: obligation.Satisfied}
	}

	return obligation.Result{
		Status: obligation.Violated,
		Diag: &types.Diagnostic{
			Code:     "XPC002",
			Severity: types.SeverityError,
			Source:   res.Source,
			Message:  "webhook conversion not acknowledged",
			Detail: fmt.Sprintf("This resource is written at version %s, but the storage version "+
				"of CRD %s.%s is %s. Reading or writing this resource will invoke a conversion "+
				"webhook on every request, which is a network round-trip and a single point of failure.",
				version, crd.Group, crd.Kind, storageVersion),
			Fix: fmt.Sprintf("Re-author the resource at the storage version %s:\n"+
				"  apiVersion: %s/%s\n\n"+
				"Or add annotation xpc.dev/accept-conversion-webhook: \"true\" to acknowledge.",
				storageVersion, group, storageVersion),
			Related: []types.SourceLocation{crd.Source},
		},
	}
}

// sanitizeInstance cleans a resource name for use in an obligation ID.
func sanitizeInstance(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" {
		return "unnamed"
	}
	return name
}
