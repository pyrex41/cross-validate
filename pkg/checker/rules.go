package checker

import (
	"fmt"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/schemas"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// R1: served-and-storage version coherence
func checkR1(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	for _, crd := range w.CRDs {
		diags = append(diags, checkR1CRD(crd)...)
	}
	for _, xrd := range w.XRDs {
		diags = append(diags, checkR1XRD(xrd)...)
	}
	return diags
}

func checkR1CRD(crd types.CRDInfo) []types.Diagnostic {
	var diags []types.Diagnostic

	// R1a: all versions must be served
	for _, v := range crd.Versions {
		if !v.Served {
			diags = append(diags, types.Diagnostic{
				Code:     "XPC001",
				Severity: types.SeverityError,
				Source:   crd.Source,
				Message:  fmt.Sprintf("version %s of CRD %s/%s is not served", v.Name, crd.Group, crd.Kind),
				Detail: fmt.Sprintf("CRD %s.%s declares version %s but it is not marked as served. "+
					"Clients cannot use this version.", crd.Group, crd.Kind, v.Name),
				Fix: fmt.Sprintf("Set served: true for version %s or remove the version entry.", v.Name),
			})
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
		diags = append(diags, types.Diagnostic{
			Code:     "XPC001",
			Severity: types.SeverityError,
			Source:   crd.Source,
			Message: fmt.Sprintf("CRD %s/%s has %d storage versions (expected exactly 1)",
				crd.Group, crd.Kind, storageCount),
			Detail: "Every CRD must have exactly one version marked as the storage version.",
			Fix:    "Mark exactly one version with storage: true.",
		})
	}

	return diags
}

func checkR1XRD(xrd types.CRDInfo) []types.Diagnostic {
	var diags []types.Diagnostic

	// R1a: versions must be served
	for _, v := range xrd.Versions {
		if !v.Served {
			diags = append(diags, types.Diagnostic{
				Code:     "XPC001",
				Severity: types.SeverityError,
				Source:   xrd.Source,
				Message:  fmt.Sprintf("version %s of XRD %s/%s is not served", v.Name, xrd.Group, xrd.Kind),
				Detail: fmt.Sprintf("XRD %s.%s declares version %s but it is not marked as served.",
					xrd.Group, xrd.Kind, v.Name),
				Fix: fmt.Sprintf("Set served: true for version %s.", v.Name),
			})
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
		diags = append(diags, types.Diagnostic{
			Code:     "XPC001",
			Severity: types.SeverityError,
			Source:   xrd.Source,
			Message:  fmt.Sprintf("XRD %s/%s has no referenceable version", xrd.Group, xrd.Kind),
			Detail:   "Every XRD must have at least one version marked as referenceable for Compositions to reference.",
			Fix:      "Set referenceable: true on the version Compositions should use.",
		})
	}

	return diags
}

// R2: conversion cost opt-in
func checkR2(w *types.World, strictConversions bool) []types.Diagnostic {
	var diags []types.Diagnostic

	// Build a lookup map for CRDs by group+kind
	crdMap := make(map[string]*types.CRDInfo)
	for i := range w.CRDs {
		key := w.CRDs[i].Group + "/" + w.CRDs[i].Kind
		crdMap[key] = &w.CRDs[i]
	}

	for _, res := range w.Resources {
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

		if strictConversions {
			diags = append(diags, types.Diagnostic{
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
			})
			continue
		}

		if res.Annotations["xpc.dev/accept-conversion-webhook"] == "true" {
			continue
		}

		diags = append(diags, types.Diagnostic{
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
		})
	}

	return diags
}

// R3: composition type references resolve
func checkR3(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	// Build XRD lookup by group+kind
	type xrdEntry struct {
		xrd      *types.CRDInfo
		versions map[string]bool
	}
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

	for _, comp := range w.Compositions {
		ref := comp.CompositeTypeRef
		key := ref.Group + "/" + ref.Kind

		entry, ok := xrdMap[key]
		if !ok {
			diags = append(diags, types.Diagnostic{
				Code:     "XPC003",
				Severity: types.SeverityError,
				Source:   comp.Source,
				Message: fmt.Sprintf("Composition %s references unknown XRD %s/%s",
					comp.Name, ref.Group, ref.Kind),
				Detail: fmt.Sprintf("compositeTypeRef references %s/%s/%s but no "+
					"CompositeResourceDefinition for this group/kind was found.",
					ref.Group, ref.Version, ref.Kind),
				Fix: "Ensure the XRD is defined and included in the checked manifests.",
			})
			continue
		}

		if !entry.versions[ref.Version] {
			diags = append(diags, types.Diagnostic{
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
			})
		}
	}

	return diags
}

// R4: pipeline functions resolve
func checkR4(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	// Build function lookup
	fnMap := make(map[string]*types.FunctionInfo)
	for i := range w.Functions {
		fnMap[w.Functions[i].Name] = &w.Functions[i]
	}

	for _, comp := range w.Compositions {
		if comp.Mode != "Pipeline" {
			continue
		}
		for _, step := range comp.Pipeline {
			fn, ok := fnMap[step.FunctionRef]
			if !ok {
				diags = append(diags, types.Diagnostic{
					Code:     "XPC004",
					Severity: types.SeverityError,
					Source:   comp.Source,
					Message: fmt.Sprintf("Composition %s step %s references unknown function %s",
						comp.Name, step.Name, step.FunctionRef),
					Detail: fmt.Sprintf("Pipeline step \"%s\" references function \"%s\" but no "+
						"Function resource with this name was found.", step.Name, step.FunctionRef),
					Fix: fmt.Sprintf("Ensure Function \"%s\" is defined and included in the checked manifests.",
						step.FunctionRef),
				})
				continue
			}

			if step.InputAPIVersion == "" || len(fn.InputVersions) == 0 {
				continue
			}

			found := false
			for _, v := range fn.InputVersions {
				if v == step.InputAPIVersion {
					found = true
					break
				}
			}
			if !found {
				diags = append(diags, types.Diagnostic{
					Code:     "XPC004",
					Severity: types.SeverityError,
					Source:   comp.Source,
					Message: fmt.Sprintf("Composition %s step %s input version mismatch for function %s",
						comp.Name, step.Name, step.FunctionRef),
					Detail: fmt.Sprintf("Pipeline step \"%s\" passes input at %s but function \"%s\" accepts: %s",
						step.Name, step.InputAPIVersion, step.FunctionRef,
						strings.Join(fn.InputVersions, ", ")),
					Fix: fmt.Sprintf("Change the input apiVersion to one accepted by the function: %s",
						strings.Join(fn.InputVersions, ", ")),
					Related: []types.SourceLocation{fn.Source},
				})
			}
		}
	}

	return diags
}

// R5: patch typecheck
func checkR5(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	// Build XRD schema lookup
	xrdSchemaMap := make(map[string]map[string]interface{}) // group/kind -> schema
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

	// Build CRD schema lookup
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

	for _, comp := range w.Compositions {
		ref := comp.CompositeTypeRef
		xrdKey := ref.Group + "/" + ref.Kind
		xrdSchema := xrdSchemaMap[xrdKey]

		// Check legacy Resources mode patches
		for _, res := range comp.Resources {
			if xrdSchema == nil {
				continue
			}
			crdKey := ""
			if parts := strings.SplitN(res.Base.APIVersion, "/", 2); len(parts) == 2 {
				crdKey = parts[0] + "/" + res.Base.Kind
			}
			crdSchema := crdSchemaMap[crdKey]

			for _, patch := range res.Patches {
				if patch.Type != "FromCompositeFieldPath" && patch.Type != "" {
					continue
				}
				if patch.FromFieldPath == "" || patch.ToFieldPath == "" {
					continue
				}

				fromType := schemas.ResolveFieldType(xrdSchema, patch.FromFieldPath)
				if crdSchema == nil {
					continue
				}
				toType := schemas.ResolveFieldType(crdSchema, patch.ToFieldPath)

				if fromType == schemas.FieldTypeUnknown || toType == schemas.FieldTypeUnknown {
					continue
				}

				// Check if transforms make the assignment valid
				finalType := fromType
				for _, t := range patch.Transforms {
					if t.Type == "convert" && t.Convert != "" {
						finalType = schemas.FieldType(t.Convert)
					}
				}

				if !schemas.TypeAssignable(finalType, toType) {
					diags = append(diags, types.Diagnostic{
						Code:     "XPC005",
						Severity: types.SeverityError,
						Source:   comp.Source,
						Message:  fmt.Sprintf("patch type mismatch in Composition %s", comp.Name),
						Detail: fmt.Sprintf("Field %s has type %s but target field %s has type %s. "+
							"These types are not compatible without an explicit transform.",
							patch.FromFieldPath, fromType, patch.ToFieldPath, toType),
						Fix: fmt.Sprintf("Add a transform (e.g., convert: { toType: %s }) to the patch.", toType),
					})
				}
			}
		}
	}

	return diags
}

// R6: argo wave ordering
func checkR6(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	// Build sync-wave lookup from all resources
	waveMap := make(map[string]int) // kind/name -> wave
	for _, res := range w.Resources {
		wave := ir.ParseSyncWave(res.Annotations)
		key := res.Kind + "/" + res.Name
		waveMap[key] = wave
	}

	// Also include XRDs, Compositions, Functions with their waves
	for _, xrd := range w.XRDs {
		key := "CompositeResourceDefinition/" + xrd.Kind
		// XRDs might be in the resources list already; if not, default wave 0
		if _, ok := waveMap[key]; !ok {
			waveMap[key] = 0
		}
	}

	for _, app := range w.ArgoApps {
		// Use sync waves from the Argo Application if available
		for _, sw := range app.SyncWaves {
			key := sw.Kind + "/" + sw.Name
			waveMap[key] = sw.Wave
		}

		// R6a: XRD wave < XR wave
		for _, xrd := range w.XRDs {
			xrdKey := "CompositeResourceDefinition/" + xrd.Kind
			xrdWave := waveMap[xrdKey]

			for _, res := range w.Resources {
				if res.Kind != xrd.Kind {
					continue
				}
				resKey := res.Kind + "/" + res.Name
				resWave := waveMap[resKey]

				if xrdWave >= resWave {
					diags = append(diags, types.Diagnostic{
						Code:     "XPC006",
						Severity: types.SeverityError,
						Source:   app.Source,
						Message: fmt.Sprintf("XRD %s (wave %d) must have a lower sync-wave than XR %s (wave %d)",
							xrd.Kind, xrdWave, res.Name, resWave),
						Detail: fmt.Sprintf("CompositeResourceDefinition %s must be Established before any XR "+
							"of this kind can be applied. The XRD sync-wave must be strictly less than the XR sync-wave.",
							xrd.Kind),
						Fix: fmt.Sprintf("Set sync-wave on the XRD to a value less than %d.", resWave),
						Related: []types.SourceLocation{xrd.Source},
					})
				}
			}
		}

		// R6b: Function wave < Composition wave
		for _, comp := range w.Compositions {
			if comp.Mode != "Pipeline" {
				continue
			}
			compKey := "Composition/" + comp.Name
			compWave := waveMap[compKey]

			for _, step := range comp.Pipeline {
				fnKey := "Function/" + step.FunctionRef
				fnWave := waveMap[fnKey]

				if fnWave >= compWave {
					diags = append(diags, types.Diagnostic{
						Code:     "XPC006",
						Severity: types.SeverityError,
						Source:   app.Source,
						Message: fmt.Sprintf("Function %s (wave %d) must have a lower sync-wave than Composition %s (wave %d)",
							step.FunctionRef, fnWave, comp.Name, compWave),
						Detail: fmt.Sprintf("Function %s must be Healthy before Composition %s can use it. "+
							"The Function sync-wave must be strictly less than the Composition sync-wave.",
							step.FunctionRef, comp.Name),
						Fix: fmt.Sprintf("Set sync-wave on Function %s to a value less than %d.",
							step.FunctionRef, compWave),
						Related: []types.SourceLocation{comp.Source},
					})
				}
			}
		}
	}

	return diags
}

// R7: owner reference coherence
func checkR7(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	for _, app := range w.ArgoApps {
		if app.TrackingMode != "label" {
			continue
		}
		for _, comp := range w.Compositions {
			diags = append(diags, types.Diagnostic{
				Code:     "XPC007",
				Severity: types.SeverityWarning,
				Source:   app.Source,
				Message: fmt.Sprintf("Argo CD label tracking conflicts with Crossplane Composition %s",
					comp.Name),
				Detail: fmt.Sprintf("Argo Application \"%s\" uses label-based tracking, but Composition "+
					"\"%s\" produces resources that Crossplane will label-propagate to. This causes Argo CD "+
					"to either prune Crossplane-created resources or fight Crossplane for ownership "+
					"(see crossplane/crossplane#2121).", app.Name, comp.Name),
				Fix: "Switch Argo CD tracking mode to annotation: set argocd.argoproj.io/tracking-method: annotation on the Application.",
				Related: []types.SourceLocation{comp.Source},
			})
		}
	}

	return diags
}

// R8: v1 vs v2 spec machinery
func checkR8(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	// Build XRD lookup
	xrdAPIVersionMap := make(map[string]string) // group/kind -> apiVersion
	for _, xrd := range w.XRDs {
		key := xrd.Group + "/" + xrd.Kind
		xrdAPIVersionMap[key] = xrd.APIVersion
	}

	v1MachineryFields := []string{
		"publishConnectionDetailsTo",
		"writeConnectionSecretToRef",
		"compositionRef",
		"compositionSelector",
		"compositionRevisionRef",
		"compositionRevisionSelector",
		"compositionUpdatePolicy",
	}

	for _, res := range w.Resources {
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

		// Check for top-level machinery fields
		spec, _ := res.Raw["spec"].(map[string]interface{})
		if spec == nil {
			continue
		}

		for _, field := range v1MachineryFields {
			if _, exists := spec[field]; exists {
				// Check if spec.crossplane exists
				if _, hasCrossplane := spec["crossplane"]; !hasCrossplane {
					diags = append(diags, types.Diagnostic{
						Code:     "XPC008",
						Severity: types.SeverityError,
						Source:   res.Source,
						Message: fmt.Sprintf("Resource %s uses v1-style machinery fields with a v2 XRD",
							res.Name),
						Detail: fmt.Sprintf("%s \"%s\" uses top-level machinery field \"%s\" "+
							"but its XRD uses apiextensions.crossplane.io/v2. In v2, these fields "+
							"must be under spec.crossplane.", res.Kind, res.Name, field),
						Fix: "Move machinery fields under spec.crossplane. See the Crossplane v2 migration guide.",
					})
					break // one error per resource is enough
				}
			}
		}
	}

	return diags
}

// R9: required resources bootstrappable
func checkR9(w *types.World) []types.Diagnostic {
	var diags []types.Diagnostic

	for _, res := range w.Resources {
		if res.Annotations["xpc.dev/required-resource-missing"] != "true" {
			continue
		}
		if res.Annotations["xpc.dev/accept-bootstrap-gap"] == "true" {
			continue
		}

		diags = append(diags, types.Diagnostic{
			Code:     "XPC009",
			Severity: types.SeverityError,
			Source:   res.Source,
			Message:  fmt.Sprintf("required resource not bootstrappable for %s", res.Name),
			Detail: fmt.Sprintf("%s \"%s\" references a required resource that may not exist on "+
				"first reconcile. The Composition pipeline depends on a resource that isn't produced "+
				"by an earlier step or known to exist at bootstrap time.", res.Kind, res.Name),
			Fix: "Ensure the required resource is produced by an earlier pipeline step, " +
				"or add annotation xpc.dev/accept-bootstrap-gap: \"true\" to acknowledge.",
		})
	}

	return diags
}
