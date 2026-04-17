// Package refs implements Category B (reference-resolution) obligation generators.
package refs

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// xrdEntry is a lookup entry for XRDs indexed by group/kind.
type xrdEntry struct {
	xrd      *types.CRDInfo
	versions map[string]bool // referenceable version names
}

// CompXRDRef is a Category B generator that checks whether each Composition's
// compositeTypeRef resolves to a referenceable XRD version.
//
// Absorbs legacy rule R3 (XPC003).
type CompXRDRef struct{}

var _ obligation.Generator = CompXRDRef{}

func (CompXRDRef) Name() string                    { return "comp-xrd-ref" }
func (CompXRDRef) Category() obligation.Category   { return obligation.CatReference }

func (CompXRDRef) Description() string {
	return `Composition compositeTypeRef must resolve to a referenceable XRD version.

A Composition's compositeTypeRef points to a group/version/kind that either:
(a) has no matching XRD at all, or (b) has an XRD but the referenced version
is not marked referenceable.

This will cause the Composition to fail silently — Crossplane won't render any
resources for it because it can't resolve the composite type.

Fix: Ensure the XRD exists and the referenced version is referenceable.

Legacy code: XPC003`
}

// Generate emits one obligation per Composition in the World.
func (CompXRDRef) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	// Build XRD lookup: group/kind -> { xrd, referenceable versions }
	xrdMap := make(map[string]*xrdEntry)
	for i := range w.XRDs {
		xrd := &w.XRDs[i]
		key := xrd.Group + "/" + xrd.Kind
		entry := &xrdEntry{xrd: xrd, versions: make(map[string]bool)}
		for _, v := range xrd.Versions {
			if v.Referenceable {
				entry.versions[v.Name] = true
			}
		}
		xrdMap[key] = entry
	}

	var obs []obligation.Obligation
	for _, comp := range w.Compositions {
		comp := comp // capture for closure
		ref := comp.CompositeTypeRef
		instance := sanitizeInstance(comp.Name)

		obs = append(obs, obligation.Obligation{
			ID:       obligation.MakeID(obligation.CatReference, "comp-xrd-ref", instance),
			Category: obligation.CatReference,
			Subject:  comp.Source,
			Claim: fmt.Sprintf("Composition %s compositeTypeRef %s/%s/%s resolves to a referenceable XRD version",
				comp.Name, ref.Group, ref.Version, ref.Kind),
			Provenance: obligation.Provenance{
				Generator: "comp-xrd-ref",
				Category:  obligation.CatReference,
				InputHash: obligation.ContentHash(comp.Name + ref.Group + ref.Version + ref.Kind),
			},
			LegacyCode: "XPC003",
			Discharge: func(ctx *obligation.Context) obligation.Result {
				return dischargeCompXRDRef(comp, xrdMap)
			},
		})
	}

	return obs
}

func dischargeCompXRDRef(comp types.CompositionInfo, xrdMap map[string]*xrdEntry) obligation.Result {
	ref := comp.CompositeTypeRef
	key := ref.Group + "/" + ref.Kind

	entry, ok := xrdMap[key]
	if !ok {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC003",
				Severity: types.SeverityError,
				Source:   comp.Source,
				Message: fmt.Sprintf("Composition %s references unknown XRD %s/%s",
					comp.Name, ref.Group, ref.Kind),
				Detail: fmt.Sprintf("compositeTypeRef references %s/%s/%s but no "+
					"CompositeResourceDefinition for this group/kind was found.",
					ref.Group, ref.Version, ref.Kind),
				Fix: "Ensure the XRD is defined and included in the checked manifests.",
			},
		}
	}

	if !entry.versions[ref.Version] {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC003",
				Severity: types.SeverityError,
				Source:   comp.Source,
				Message: fmt.Sprintf("Composition %s uses version %s which is not referenceable on XRD %s/%s",
					comp.Name, ref.Version, ref.Group, ref.Kind),
				Detail: fmt.Sprintf("The Composition references %s/%s/%s but this version is not "+
					"marked referenceable on the XRD. Only referenceable versions can be used by Compositions.",
					ref.Group, ref.Version, ref.Kind),
				Fix: fmt.Sprintf("Use a referenceable version, or set referenceable: true on version %s in the XRD.",
					ref.Version),
				Related: []types.SourceLocation{entry.xrd.Source},
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
