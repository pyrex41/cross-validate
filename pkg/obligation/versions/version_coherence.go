// Package versions implements Category C (version-coherence) obligation generators.
package versions

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// VersionCoherence is a Category C generator that checks CRD/XRD version
// coherence: all versions served, exactly one storage version for CRDs,
// and at least one referenceable version for XRDs.
//
// Absorbs legacy rule R1 (XPC001).
type VersionCoherence struct{}

var _ obligation.Generator = VersionCoherence{}

func (VersionCoherence) Name() string                  { return "version-coherence" }
func (VersionCoherence) Category() obligation.Category { return obligation.CatVersionCoherence }

func (VersionCoherence) Description() string {
	return `CRD/XRD version coherence: all versions served, exactly one storage version for CRDs, at least one referenceable version for XRDs.

Every CRD must have all versions marked as served and exactly one version
marked as the storage version. Every XRD must have all versions served and
at least one version marked as referenceable.

Fix: Set served: true on all versions, mark exactly one storage version on
CRDs, and ensure at least one referenceable version on XRDs.

Legacy code: XPC001`
}

// Generate emits one obligation per CRD and one per XRD in the World.
func (VersionCoherence) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	var obs []obligation.Obligation

	for _, crd := range w.CRDs {
		crd := crd // capture for closure
		instance := sanitizeInstance(crd.Group + "." + crd.Kind)

		obs = append(obs, obligation.Obligation{
			ID:       obligation.MakeID(obligation.CatVersionCoherence, "version-coherence", instance),
			Category: obligation.CatVersionCoherence,
			Subject:  crd.Source,
			Claim: fmt.Sprintf("CRD %s/%s has all versions served and exactly one storage version",
				crd.Group, crd.Kind),
			Provenance: obligation.Provenance{
				Generator: "version-coherence",
				Category:  obligation.CatVersionCoherence,
				InputHash: obligation.ContentHash(crd.Group + crd.Kind),
			},
			LegacyCode: "XPC001",
			Discharge: func(ctx *obligation.Context) obligation.Result {
				return dischargeCRDCoherence(crd)
			},
		})
	}

	for _, xrd := range w.XRDs {
		xrd := xrd // capture for closure
		instance := sanitizeInstance(xrd.Group + "." + xrd.Kind)

		obs = append(obs, obligation.Obligation{
			ID:       obligation.MakeID(obligation.CatVersionCoherence, "version-coherence", instance),
			Category: obligation.CatVersionCoherence,
			Subject:  xrd.Source,
			Claim: fmt.Sprintf("XRD %s/%s has all versions served and at least one referenceable version",
				xrd.Group, xrd.Kind),
			Provenance: obligation.Provenance{
				Generator: "version-coherence",
				Category:  obligation.CatVersionCoherence,
				InputHash: obligation.ContentHash(xrd.Group + xrd.Kind),
			},
			LegacyCode: "XPC001",
			Discharge: func(ctx *obligation.Context) obligation.Result {
				return dischargeXRDCoherence(xrd)
			},
		})
	}

	return obs
}

func dischargeCRDCoherence(crd types.CRDInfo) obligation.Result {
	// R1a: all versions must be served
	for _, v := range crd.Versions {
		if !v.Served {
			return obligation.Result{
				Status: obligation.Violated,
				Diag: &types.Diagnostic{
					Code:     "XPC001",
					Severity: types.SeverityError,
					Source:   crd.Source,
					Message:  fmt.Sprintf("version %s of CRD %s/%s is not served", v.Name, crd.Group, crd.Kind),
					Detail: fmt.Sprintf("CRD %s.%s declares version %s but it is not marked as served. "+
						"Clients cannot use this version.", crd.Group, crd.Kind, v.Name),
					Fix: fmt.Sprintf("Set served: true for version %s or remove the version entry.", v.Name),
				},
			}
		}
	}

	// R1b: exactly one storage version
	storageCount := 0
	for _, v := range crd.Versions {
		if v.Storage {
			storageCount++
		}
	}
	if len(crd.Versions) > 0 && storageCount != 1 {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC001",
				Severity: types.SeverityError,
				Source:   crd.Source,
				Message: fmt.Sprintf("CRD %s/%s has %d storage versions (expected exactly 1)",
					crd.Group, crd.Kind, storageCount),
				Detail: "Every CRD must have exactly one version marked as the storage version.",
				Fix:    "Mark exactly one version with storage: true.",
			},
		}
	}

	return obligation.Result{Status: obligation.Satisfied}
}

func dischargeXRDCoherence(xrd types.CRDInfo) obligation.Result {
	// R1a: versions must be served
	for _, v := range xrd.Versions {
		if !v.Served {
			return obligation.Result{
				Status: obligation.Violated,
				Diag: &types.Diagnostic{
					Code:     "XPC001",
					Severity: types.SeverityError,
					Source:   xrd.Source,
					Message:  fmt.Sprintf("version %s of XRD %s/%s is not served", v.Name, xrd.Group, xrd.Kind),
					Detail: fmt.Sprintf("XRD %s.%s declares version %s but it is not marked as served.",
						xrd.Group, xrd.Kind, v.Name),
					Fix: fmt.Sprintf("Set served: true for version %s.", v.Name),
				},
			}
		}
	}

	// R1b: at least one referenceable version
	refCount := 0
	for _, v := range xrd.Versions {
		if v.Referenceable {
			refCount++
		}
	}
	if len(xrd.Versions) > 0 && refCount == 0 {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC001",
				Severity: types.SeverityError,
				Source:   xrd.Source,
				Message:  fmt.Sprintf("XRD %s/%s has no referenceable version", xrd.Group, xrd.Kind),
				Detail:   "Every XRD must have at least one version marked as referenceable for Compositions to reference.",
				Fix:      "Set referenceable: true on the version Compositions should use.",
			},
		}
	}

	return obligation.Result{Status: obligation.Satisfied}
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
