package refs

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/obligation"
	"github.com/pyrex41/cross-validate-/pkg/schemas"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// PatchCompat is a Category B generator that checks whether each patch in a
// Resources-mode Composition has compatible source and target field types.
//
// Absorbs legacy rule R5 (XPC005).
type PatchCompat struct{}

var _ obligation.Generator = PatchCompat{}

func (PatchCompat) Name() string                  { return "patch-compat" }
func (PatchCompat) Category() obligation.Category { return obligation.CatReference }

func (PatchCompat) Description() string {
	return `Patch field types must be compatible between source and target.

For FromCompositeFieldPath patches, the source field type (from the XRD schema)
must be assignable to the target field type (from the managed resource CRD
schema), optionally after applying transforms.

Fix: Add an explicit convert transform to make the types compatible.

Legacy code: XPC005`
}

// Generate emits one obligation per patch in each Resources-mode Composition
// that uses FromCompositeFieldPath with both from/to field paths set.
func (PatchCompat) Generate(ctx *obligation.Context) []obligation.Obligation {
	w := ctx.World

	// Build XRD schema lookup: group/kind -> schema
	xrdSchemaMap := make(map[string]map[string]interface{})
	for _, xrd := range w.XRDs {
		for _, v := range xrd.Versions {
			if v.Referenceable && v.SchemaDigest != "" {
				if si, ok := w.Schemas[v.SchemaDigest]; ok {
					key := xrd.Group + "/" + xrd.Kind
					xrdSchemaMap[key] = si.Schema
				}
			}
		}
	}

	// Build CRD schema lookup: group/kind -> schema
	crdSchemaMap := make(map[string]map[string]interface{})
	for _, crd := range w.CRDs {
		for _, v := range crd.Versions {
			if v.Storage && v.SchemaDigest != "" {
				if si, ok := w.Schemas[v.SchemaDigest]; ok {
					key := crd.Group + "/" + crd.Kind
					crdSchemaMap[key] = si.Schema
				}
			}
		}
	}

	var obs []obligation.Obligation
	for _, comp := range w.Compositions {
		comp := comp // capture for closure
		ref := comp.CompositeTypeRef
		xrdKey := ref.Group + "/" + ref.Kind
		xrdSchema := xrdSchemaMap[xrdKey]

		for _, res := range comp.Resources {
			res := res // capture for closure
			if xrdSchema == nil {
				continue
			}
			crdKey := ""
			if parts := strings.SplitN(res.Base.APIVersion, "/", 2); len(parts) == 2 {
				crdKey = parts[0] + "/" + res.Base.Kind
			}
			crdSchema := crdSchemaMap[crdKey]

			for _, patch := range res.Patches {
				patch := patch // capture for closure
				if patch.Type != "FromCompositeFieldPath" && patch.Type != "" {
					continue
				}
				if patch.FromFieldPath == "" || patch.ToFieldPath == "" {
					continue
				}

				instance := sanitizeInstance(comp.Name + "." + patch.FromFieldPath + "->" + patch.ToFieldPath)

				obs = append(obs, obligation.Obligation{
					ID:       obligation.MakeID(obligation.CatReference, "patch-compat", instance),
					Category: obligation.CatReference,
					Subject:  comp.Source,
					Claim: fmt.Sprintf("Composition %s patch %s -> %s has compatible types",
						comp.Name, patch.FromFieldPath, patch.ToFieldPath),
					Provenance: obligation.Provenance{
						Generator: "patch-compat",
						Category:  obligation.CatReference,
						InputHash: obligation.ContentHash(comp.Name + patch.FromFieldPath + patch.ToFieldPath),
					},
					LegacyCode: "XPC005",
					Discharge: func(ctx *obligation.Context) obligation.Result {
						return dischargePatchCompat(comp, patch, xrdSchema, crdSchema)
					},
				})
			}
		}
	}

	return obs
}

func dischargePatchCompat(comp types.CompositionInfo, patch types.PatchInfo, xrdSchema, crdSchema map[string]interface{}) obligation.Result {
	fromType := schemas.ResolveFieldType(xrdSchema, patch.FromFieldPath)
	if crdSchema == nil {
		return obligation.Result{Status: obligation.Satisfied}
	}
	toType := schemas.ResolveFieldType(crdSchema, patch.ToFieldPath)

	if fromType == schemas.FieldTypeUnknown || toType == schemas.FieldTypeUnknown {
		return obligation.Result{Status: obligation.Satisfied}
	}

	// Check if transforms make the assignment valid
	finalType := fromType
	for _, t := range patch.Transforms {
		if t.Type == "convert" && t.Convert != "" {
			finalType = schemas.FieldType(t.Convert)
		}
	}

	if !schemas.TypeAssignable(finalType, toType) {
		return obligation.Result{
			Status: obligation.Violated,
			Diag: &types.Diagnostic{
				Code:     "XPC005",
				Severity: types.SeverityError,
				Source:   comp.Source,
				Message:  fmt.Sprintf("patch type mismatch in Composition %s", comp.Name),
				Detail: fmt.Sprintf("Field %s has type %s but target field %s has type %s. "+
					"These types are not compatible without an explicit transform.",
					patch.FromFieldPath, finalType, patch.ToFieldPath, toType),
				Fix: fmt.Sprintf("Add a transform (e.g., convert: { toType: %s }) to the patch.", toType),
			},
		}
	}

	return obligation.Result{Status: obligation.Satisfied}
}
