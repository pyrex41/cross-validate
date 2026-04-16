// Package checker bridges the Go side with the Shen type-checking kernel.
// The Shen kernel runs in-process via the embedded shen runtime — no
// subprocess, no external binary.
package checker

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/schemas"
	"github.com/pyrex41/cross-validate-/pkg/shen"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Config holds checker configuration.
type Config struct {
	// KernelPath is the path to the Shen kernel directory.
	// Defaults to "kernel" relative to the executable.
	KernelPath string

	// StrictConversions refuses webhook conversions entirely
	// instead of allowing them with an opt-in annotation.
	StrictConversions bool
}

// Check runs all type-checking rules against the World.
// The Shen kernel is loaded in-process and called directly.
func Check(w *types.World, cfg Config) ([]types.Diagnostic, error) {
	rt := shen.NewRuntime()

	// Set configuration globals the kernel can read.
	rt.SetGlobal("*strict-conversions*", shen.Bool(cfg.StrictConversions))

	// Resolve kernel directory.
	kernelDir := cfg.KernelPath
	if kernelDir == "" {
		kernelDir = "kernel"
	}

	// Load the kernel entry point (which loads all rule files).
	checkPath := filepath.Join(kernelDir, "check.shen")
	if _, err := rt.LoadFile(checkPath); err != nil {
		return nil, fmt.Errorf("loading kernel: %w", err)
	}

	// Enrich the world data with pre-computed information the kernel needs.
	enrichSyncWaves(w)
	resolvePatchTypes(w)

	// Convert the World to a Shen value and call check-world.
	worldVal := worldToShenValue(w)
	result, err := rt.Call("check-world", worldVal)
	if err != nil {
		return nil, fmt.Errorf("running checker: %w", err)
	}

	return valueToDiagnostics(result), nil
}

// ---------------------------------------------------------------------------
// World enrichment — pre-compute data the kernel expects
// ---------------------------------------------------------------------------

// enrichSyncWaves adds sync wave entries derived from resource annotations
// to ArgoApps so the kernel can check wave ordering.
func enrichSyncWaves(w *types.World) {
	for i := range w.ArgoApps {
		existing := make(map[string]bool)
		for _, sw := range w.ArgoApps[i].SyncWaves {
			existing[sw.Kind+"/"+sw.Name] = true
		}
		// Add XRDs
		for _, xrd := range w.XRDs {
			key := "CompositeResourceDefinition/" + xrd.Kind
			if !existing[key] {
				wave := 0
				// XRDs might be in resources with annotations
				for _, res := range w.Resources {
					if res.Kind == "CompositeResourceDefinition" && res.Name == xrd.Kind {
						wave = ir.ParseSyncWave(res.Annotations)
					}
				}
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: "CompositeResourceDefinition", Name: xrd.Kind, Wave: wave,
				})
				existing[key] = true
			}
		}
		// Add all resources
		for _, res := range w.Resources {
			key := res.Kind + "/" + res.Name
			if !existing[key] {
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: res.Kind, Name: res.Name, Wave: ir.ParseSyncWave(res.Annotations),
				})
				existing[key] = true
			}
		}
		// Add compositions and functions
		for _, comp := range w.Compositions {
			key := "Composition/" + comp.Name
			if !existing[key] {
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: "Composition", Name: comp.Name, Wave: 0,
				})
				existing[key] = true
			}
		}
		for _, fn := range w.Functions {
			key := "Function/" + fn.Name
			if !existing[key] {
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: "Function", Name: fn.Name, Wave: 0,
				})
				existing[key] = true
			}
		}
	}
}

// resolvePatchTypes resolves field types for patches in Resources-mode compositions
// using the world's schema data.
func resolvePatchTypes(w *types.World) {
	xrdSchemaMap := make(map[string]map[string]interface{})
	for _, xrd := range w.XRDs {
		for _, v := range xrd.Versions {
			if v.Referenceable && v.SchemaDigest != "" {
				if si, ok := w.Schemas[v.SchemaDigest]; ok {
					xrdSchemaMap[xrd.Group+"/"+xrd.Kind] = si.Schema
				}
			}
		}
	}
	crdSchemaMap := make(map[string]map[string]interface{})
	for _, crd := range w.CRDs {
		for _, v := range crd.Versions {
			if v.Storage && v.SchemaDigest != "" {
				if si, ok := w.Schemas[v.SchemaDigest]; ok {
					crdSchemaMap[crd.Group+"/"+crd.Kind] = si.Schema
				}
			}
		}
	}

	for ci, comp := range w.Compositions {
		xrdKey := comp.CompositeTypeRef.Group + "/" + comp.CompositeTypeRef.Kind
		xrdSchema := xrdSchemaMap[xrdKey]
		for ri, res := range comp.Resources {
			crdKey := ""
			if parts := strings.SplitN(res.Base.APIVersion, "/", 2); len(parts) == 2 {
				crdKey = parts[0] + "/" + res.Base.Kind
			}
			crdSchema := crdSchemaMap[crdKey]
			for pi, patch := range res.Patches {
				if patch.FromFieldPath == "" || patch.ToFieldPath == "" {
					continue
				}
				fromType := "unknown"
				toType := "unknown"
				if xrdSchema != nil {
					ft := schemas.ResolveFieldType(xrdSchema, patch.FromFieldPath)
					if ft != schemas.FieldTypeUnknown {
						fromType = string(ft)
					}
				}
				if crdSchema != nil {
					tt := schemas.ResolveFieldType(crdSchema, patch.ToFieldPath)
					if tt != schemas.FieldTypeUnknown {
						toType = string(tt)
					}
				}
				// Store resolved types in Transforms metadata for kernel use
				w.Compositions[ci].Resources[ri].Patches[pi].Transforms = append(
					w.Compositions[ci].Resources[ri].Patches[pi].Transforms,
					types.TransformInfo{Type: "__resolved_types", Convert: fromType + "→" + toType},
				)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// World → Shen value
// ---------------------------------------------------------------------------

func worldToShenValue(w *types.World) shen.Value {
	sections := []shen.Value{shen.Sym("world")}

	// CRDs
	var crds []shen.Value
	for _, crd := range w.CRDs {
		crds = append(crds, crdToValue(crd))
	}
	sections = append(sections, shen.FromSlice(append([]shen.Value{shen.Sym("crds")}, crds...)))

	// XRDs
	var xrds []shen.Value
	for _, xrd := range w.XRDs {
		xrds = append(xrds, xrdToValue(xrd))
	}
	sections = append(sections, shen.FromSlice(append([]shen.Value{shen.Sym("xrds")}, xrds...)))

	// Compositions
	var comps []shen.Value
	for _, comp := range w.Compositions {
		comps = append(comps, compositionToValue(comp))
	}
	sections = append(sections, shen.FromSlice(append([]shen.Value{shen.Sym("compositions")}, comps...)))

	// Functions
	var fns []shen.Value
	for _, fn := range w.Functions {
		fns = append(fns, functionToValue(fn))
	}
	sections = append(sections, shen.FromSlice(append([]shen.Value{shen.Sym("functions")}, fns...)))

	// Providers
	var provs []shen.Value
	for _, p := range w.Providers {
		provs = append(provs, providerToValue(p))
	}
	sections = append(sections, shen.FromSlice(append([]shen.Value{shen.Sym("providers")}, provs...)))

	// Configurations
	var cfgs []shen.Value
	for _, c := range w.Configurations {
		cfgs = append(cfgs, configToValue(c))
	}
	sections = append(sections, shen.FromSlice(append([]shen.Value{shen.Sym("configurations")}, cfgs...)))

	// Resources
	var ress []shen.Value
	for _, res := range w.Resources {
		ress = append(ress, resourceToValue(res))
	}
	sections = append(sections, shen.FromSlice(append([]shen.Value{shen.Sym("resources")}, ress...)))

	// Argo Apps
	var apps []shen.Value
	for _, app := range w.ArgoApps {
		apps = append(apps, argoAppToValue(app))
	}
	sections = append(sections, shen.FromSlice(append([]shen.Value{shen.Sym("argo-apps")}, apps...)))

	// Schemas
	sections = append(sections, shen.FromSlice([]shen.Value{shen.Sym("schemas")}))

	return shen.FromSlice(sections)
}

func crdToValue(crd types.CRDInfo) shen.Value {
	// [crd-fact Group Kind Scope Versions Conversion Source]
	var vers []shen.Value
	for _, v := range crd.Versions {
		vers = append(vers, shen.FromSlice([]shen.Value{
			shen.Str(v.Name),
			shen.Bool(v.Served),
			shen.Bool(v.Storage),
			shen.Str(v.SchemaDigest),
		}))
	}
	costSym := shen.Sym(strings.ToLower(string(crd.Conversion.CostClass)))
	conv := shen.FromSlice([]shen.Value{
		shen.Str(crd.Conversion.Strategy),
		costSym,
		shen.Str(crd.Conversion.WebhookService),
	})
	return shen.FromSlice([]shen.Value{
		shen.Sym("crd-fact"),
		shen.Str(crd.Group),
		shen.Str(crd.Kind),
		shen.Str(crd.Scope),
		shen.FromSlice(vers),
		conv,
		sourceToValue(crd.Source),
	})
}

func xrdToValue(xrd types.CRDInfo) shen.Value {
	// [xrd-fact Group Kind Scope APIVersion Versions Source]
	var vers []shen.Value
	for _, v := range xrd.Versions {
		vers = append(vers, shen.FromSlice([]shen.Value{
			shen.Str(v.Name),
			shen.Bool(v.Served),
			shen.Bool(v.Referenceable),
			shen.Str(v.SchemaDigest),
		}))
	}
	return shen.FromSlice([]shen.Value{
		shen.Sym("xrd-fact"),
		shen.Str(xrd.Group),
		shen.Str(xrd.Kind),
		shen.Str(xrd.Scope),
		shen.Str(xrd.APIVersion),
		shen.FromSlice(vers),
		sourceToValue(xrd.Source),
	})
}

func compositionToValue(comp types.CompositionInfo) shen.Value {
	// [composition-fact Name GVK Mode Pipeline Resources Source]
	gvk := shen.FromSlice([]shen.Value{
		shen.Sym("gvk"),
		shen.Str(comp.CompositeTypeRef.Group),
		shen.Str(comp.CompositeTypeRef.Version),
		shen.Str(comp.CompositeTypeRef.Kind),
	})

	var steps []shen.Value
	for _, step := range comp.Pipeline {
		steps = append(steps, shen.FromSlice([]shen.Value{
			shen.Str(step.Name),
			shen.Str(step.FunctionRef),
			shen.Str(step.InputAPIVersion),
			shen.Str(step.InputKind),
		}))
	}

	var resources []shen.Value
	for _, res := range comp.Resources {
		var patches []shen.Value
		for _, p := range res.Patches {
			fromType := shen.Str("unknown")
			toType := shen.Str("unknown")
			for _, t := range p.Transforms {
				if t.Type == "__resolved_types" {
					parts := strings.SplitN(t.Convert, "→", 2)
					if len(parts) == 2 {
						fromType = shen.Str(parts[0])
						toType = shen.Str(parts[1])
					}
				}
			}
			patches = append(patches, shen.FromSlice([]shen.Value{
				shen.Sym("patch"),
				shen.Str(p.Type),
				shen.Str(p.FromFieldPath),
				shen.Str(p.ToFieldPath),
				fromType,
				toType,
			}))
		}
		resources = append(resources, shen.FromSlice([]shen.Value{
			shen.Sym("composed-resource"),
			shen.Str(res.Name),
			shen.Str(res.Base.APIVersion),
			shen.Str(res.Base.Kind),
			shen.FromSlice(patches),
		}))
	}

	return shen.FromSlice([]shen.Value{
		shen.Sym("composition-fact"),
		shen.Str(comp.Name),
		gvk,
		shen.Str(comp.Mode),
		shen.FromSlice(steps),
		shen.FromSlice(resources),
		sourceToValue(comp.Source),
	})
}

func functionToValue(fn types.FunctionInfo) shen.Value {
	var vers []shen.Value
	for _, v := range fn.InputVersions {
		vers = append(vers, shen.Str(v))
	}
	return shen.FromSlice([]shen.Value{
		shen.Sym("function-fact"),
		shen.Str(fn.Name),
		shen.Str(fn.Package),
		shen.FromSlice(vers),
		sourceToValue(fn.Source),
	})
}

func providerToValue(p types.ProviderInfo) shen.Value {
	return shen.FromSlice([]shen.Value{
		shen.Sym("provider-fact"),
		shen.Str(p.Name),
		shen.Str(p.Package),
		sourceToValue(p.Source),
	})
}

func configToValue(c types.ConfigurationInfo) shen.Value {
	return shen.FromSlice([]shen.Value{
		shen.Sym("configuration-fact"),
		shen.Str(c.Name),
		shen.Str(c.Package),
		sourceToValue(c.Source),
	})
}

func resourceToValue(res types.ResourceInfo) shen.Value {
	var anns []shen.Value
	for k, v := range res.Annotations {
		anns = append(anns, shen.FromSlice([]shen.Value{shen.Str(k), shen.Str(v)}))
	}
	return shen.FromSlice([]shen.Value{
		shen.Sym("resource-fact"),
		shen.Str(res.APIVersion),
		shen.Str(res.Kind),
		shen.Str(res.Name),
		shen.Str(res.Namespace),
		shen.FromSlice(anns),
		sourceToValue(res.Source),
	})
}

func argoAppToValue(app types.ArgoApplication) shen.Value {
	var waves []shen.Value
	for _, sw := range app.SyncWaves {
		waves = append(waves, shen.FromSlice([]shen.Value{
			shen.Str(sw.Kind),
			shen.Str(sw.Name),
			shen.Num(float64(sw.Wave)),
		}))
	}
	return shen.FromSlice([]shen.Value{
		shen.Sym("argo-app-fact"),
		shen.Str(app.Name),
		shen.Str(app.TrackingMode),
		shen.FromSlice(waves),
		sourceToValue(app.Source),
	})
}

func sourceToValue(src types.SourceLocation) shen.Value {
	return shen.FromSlice([]shen.Value{
		shen.Sym("source"),
		shen.Str(src.File),
		shen.Num(float64(src.Line)),
	})
}

// ---------------------------------------------------------------------------
// Shen result → Diagnostics
// ---------------------------------------------------------------------------

func valueToDiagnostics(result shen.Value) []types.Diagnostic {
	var diags []types.Diagnostic
	elems := shen.ToSlice(result)
	for _, elem := range elems {
		parts := shen.ToSlice(elem)
		if len(parts) < 8 {
			continue
		}
		// [judgment Code Severity Source Message Detail Fix Related]
		if s, ok := parts[0].(shen.Sym); !ok || string(s) != "judgment" {
			continue
		}

		code := shenStr(parts[1])
		sev := parseSeverity(parts[2])
		src := parseSource(parts[3])
		msg := shenStr(parts[4])
		detail := shenStr(parts[5])
		fix := shenStr(parts[6])
		related := parseRelated(parts[7])

		diags = append(diags, types.Diagnostic{
			Code:     code,
			Severity: sev,
			Source:   src,
			Message:  msg,
			Detail:   detail,
			Fix:      fix,
			Related:  related,
		})
	}
	return diags
}

func parseSeverity(v shen.Value) types.Severity {
	switch s := v.(type) {
	case shen.Sym:
		switch string(s) {
		case "error":
			return types.SeverityError
		case "warn":
			return types.SeverityWarning
		case "info":
			return types.SeverityInfo
		}
	}
	return types.SeverityError
}

func parseSource(v shen.Value) types.SourceLocation {
	parts := shen.ToSlice(v)
	if len(parts) < 3 {
		return types.SourceLocation{}
	}
	return types.SourceLocation{
		File: shenStr(parts[1]),
		Line: shenInt(parts[2]),
	}
}

func parseRelated(v shen.Value) []types.SourceLocation {
	elems := shen.ToSlice(v)
	var result []types.SourceLocation
	for _, e := range elems {
		result = append(result, parseSource(e))
	}
	return result
}

func shenStr(v shen.Value) string {
	switch s := v.(type) {
	case shen.Str:
		return string(s)
	case shen.Sym:
		return string(s)
	default:
		return shen.PrintValue(v)
	}
}

func shenInt(v shen.Value) int {
	if n, ok := v.(shen.Num); ok {
		return int(n)
	}
	return 0
}
