// Package checker bridges the Go side with the Shen type-checking kernel.
// The Shen kernel runs in-process via the embedded shen-go runtime — no
// subprocess, no external binary.
package checker

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/pyrex41/cross-validate-/internal/shenfull"
	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/schemas"
	"github.com/pyrex41/cross-validate-/pkg/trajectory"
	"github.com/pyrex41/cross-validate-/pkg/types"
	"github.com/tiancaiamao/shen-go/kl"
)

// Config holds checker configuration.
type Config struct {
	// StrictConversions refuses webhook conversions entirely
	// instead of allowing them with an opt-in annotation.
	StrictConversions bool

	// KernelPath is the path to the Shen kernel directory. If empty,
	// the bridge searches upwards from the current working directory for
	// a "kernel" directory containing check.shen.
	KernelPath string
}

// ---------------------------------------------------------------------------
// Shen runtime lifecycle
// ---------------------------------------------------------------------------

var (
	shenOnce sync.Once
	shenCF   kl.ControlFlow
	shenErr  error
)

// initShen bootstraps the Shen runtime and loads kernel/check.shen.
// Idempotent; only the first call performs initialization.
func initShen(kernelPath string) error {
	shenOnce.Do(func() {
		if err := shenfull.Init(&shenCF); err != nil {
			shenErr = fmt.Errorf("shenfull.Init: %w", err)
			return
		}

		resolved, err := resolveKernelPath(kernelPath)
		if err != nil {
			shenErr = err
			return
		}

		absKernel, absErr := filepath.Abs(resolved)
		if absErr != nil {
			shenErr = fmt.Errorf("resolve kernel path: %w", absErr)
			return
		}

		// Shen's `read-file` primitive opens files using the literal path
		// argument, so `(load "prelude.shen")` only works when cwd is the
		// kernel directory. Chdir into the kernel, do the load, then chdir
		// back so we don't perturb the caller's environment.
		origDir, cwdErr := os.Getwd()
		if cwdErr != nil {
			shenErr = fmt.Errorf("getwd: %w", cwdErr)
			return
		}
		if err := os.Chdir(absKernel); err != nil {
			shenErr = fmt.Errorf("chdir kernel: %w", err)
			return
		}
		defer func() { _ = os.Chdir(origDir) }()

		// Rebind Shen's `*stoutput*` symbol to /dev/null while loading so
		// the transpiled runtime's banner ("(fn …) / run time: … / loaded"
		// lines emitted by `(load ...)`) doesn't leak into `xpc check`
		// output. shen-go captures `os.Stdout` once at init into a stream
		// object, so redirecting os.Stdout at the Go level has no effect.
		stoutSym := kl.MakeSymbol("*stoutput*")
		origStout := kl.PrimValue(stoutSym)
		devnull, dnErr := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if dnErr == nil {
			kl.PrimSet(stoutSym, kl.MakeStream(devnull))
		}

		loadExpr := kl.Cons(kl.MakeSymbol("load"),
			kl.Cons(kl.MakeString("check.shen"), kl.Nil))
		res := kl.Eval(&shenCF, loadExpr)

		if dnErr == nil {
			kl.PrimSet(stoutSym, origStout)
			_ = devnull.Close()
		}
		if kl.IsError(res) {
			shenErr = fmt.Errorf("load check.shen: %s", kl.ObjString(res))
			return
		}
	})
	return shenErr
}

// resolveKernelPath finds the kernel directory. Explicit paths are honoured
// as-is; empty paths are resolved by walking upwards from cwd looking for a
// "kernel" directory containing check.shen.
func resolveKernelPath(p string) (string, error) {
	if p != "" {
		return p, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "kernel", "check.shen")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Join(dir, "kernel"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate kernel directory (searched upwards from %s)", dir)
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Check runs all type-checking rules against the World.
func Check(w *types.World, cfg Config) ([]types.Diagnostic, error) {
	result := CheckWithObligations(w, cfg)
	return result.Diagnostics, nil
}

// CheckWithObligations runs the Shen kernel and returns a RunResult.
func CheckWithObligations(w *types.World, cfg Config) RunResult {
	if err := initShen(cfg.KernelPath); err != nil {
		return RunResult{Diagnostics: []types.Diagnostic{{
			Code:     "XPC000",
			Severity: types.SeverityError,
			Message:  err.Error(),
		}}}
	}

	enrichSyncWaves(w)
	resolvePatchTypes(w)
	trajectories := trajectory.Simulate(w)

	worldObj := worldToShenObj(w, trajectories)
	checkWorld := kl.PrimFunc(kl.MakeSymbol("check-world"))
	result := kl.Call(&shenCF, checkWorld, worldObj)
	if kl.IsError(result) {
		return RunResult{Diagnostics: []types.Diagnostic{{
			Code:     "XPC000",
			Severity: types.SeverityError,
			Message:  fmt.Sprintf("check-world: %s", kl.ObjString(result)),
		}}}
	}

	diags := objToDiagnostics(result)
	return buildRunResult(diags)
}

// ---------------------------------------------------------------------------
// World enrichment
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
			key := types.KindCompositeResourceDefinition + "/" + xrd.Kind
			if !existing[key] {
				wave := 0
				for _, res := range w.Resources {
					if res.Kind == types.KindCompositeResourceDefinition && res.Name == xrd.Kind {
						wave = ir.ParseSyncWave(res.Annotations)
					}
				}
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: types.KindCompositeResourceDefinition, Name: xrd.Kind, Wave: wave,
				})
				existing[key] = true
			}
		}
		for _, res := range w.Resources {
			key := res.Kind + "/" + res.Name
			if !existing[key] {
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: res.Kind, Name: res.Name, Wave: ir.ParseSyncWave(res.Annotations),
				})
				existing[key] = true
			}
		}
		for _, comp := range w.Compositions {
			key := types.KindComposition + "/" + comp.Name
			if !existing[key] {
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: types.KindComposition, Name: comp.Name, Wave: 0,
				})
				existing[key] = true
			}
		}
		for _, fn := range w.Functions {
			key := types.KindFunction + "/" + fn.Name
			if !existing[key] {
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: types.KindFunction, Name: fn.Name, Wave: 0,
				})
				existing[key] = true
			}
		}
		for _, prov := range w.Providers {
			key := types.KindProvider + "/" + prov.Name
			if !existing[key] {
				w.ArgoApps[i].SyncWaves = append(w.ArgoApps[i].SyncWaves, types.SyncWaveEntry{
					Kind: types.KindProvider, Name: prov.Name, Wave: ir.ParseSyncWave(prov.Annotations),
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
				w.Compositions[ci].Resources[ri].Patches[pi].Transforms = append(
					w.Compositions[ci].Resources[ri].Patches[pi].Transforms,
					types.TransformInfo{Type: "__resolved_types", Convert: fromType + "→" + toType},
				)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// World → Shen Obj
// ---------------------------------------------------------------------------

// sym/str/num/boolean helpers keep call sites terse.
func sym(s string) kl.Obj { return kl.MakeSymbol(s) }
func str(s string) kl.Obj { return kl.MakeString(s) }
func num(n int) kl.Obj    { return kl.MakeInteger(n) }
func boolean(b bool) kl.Obj {
	if b {
		return kl.True
	}
	return kl.False
}

// makeList builds a proper Shen list terminated by kl.Nil from a slice of Objs.
func makeList(items []kl.Obj) kl.Obj {
	out := kl.Nil
	for i := len(items) - 1; i >= 0; i-- {
		out = kl.Cons(items[i], out)
	}
	return out
}

// section wraps a list of facts as (tag fact1 fact2 ...).
func section(tag string, facts []kl.Obj) kl.Obj {
	return makeList(append([]kl.Obj{sym(tag)}, facts...))
}

// sortedSection copies src, sorts it with less, maps each element via toObj,
// and wraps the result as `(tag obj1 obj2 ...)`.
func sortedSection[T any](tag string, src []T, less func(a, b T) int, toObj func(T) kl.Obj) kl.Obj {
	cp := slices.Clone(src)
	slices.SortFunc(cp, less)
	objs := make([]kl.Obj, 0, len(cp))
	for _, x := range cp {
		objs = append(objs, toObj(x))
	}
	return section(tag, objs)
}

func worldToShenObj(w *types.World, trajectories []trajectory.Step) kl.Obj {
	// Compositions sort once and feed both the `compositions` section and the
	// `resolved-patches` section, so patches follow the composition ordering.
	comps := slices.Clone(w.Compositions)
	slices.SortFunc(comps, compositionCmp)

	compObjs := make([]kl.Obj, 0, len(comps))
	for _, c := range comps {
		compObjs = append(compObjs, compositionToObj(c))
	}

	// Resolved patch facts let Shen R5 do type-assignability checks without
	// needing schema-walking primitives.
	var patchObjs []kl.Obj
	for _, c := range comps {
		for _, res := range c.Resources {
			for _, p := range res.Patches {
				if p.Type != "" && p.Type != "FromCompositeFieldPath" {
					continue
				}
				if p.FromFieldPath == "" || p.ToFieldPath == "" {
					continue
				}
				fromType, toType := "unknown", "unknown"
				for _, t := range p.Transforms {
					if t.Type == "__resolved_types" {
						parts := strings.SplitN(t.Convert, "→", 2)
						if len(parts) == 2 {
							fromType, toType = parts[0], parts[1]
						}
					}
				}
				// Apply user-supplied convert transforms to fromType.
				for _, t := range p.Transforms {
					if t.Type == "convert" && t.Convert != "" {
						fromType = t.Convert
					}
				}
				patchObjs = append(patchObjs, makeList([]kl.Obj{
					sym("resolved-patch"),
					str(c.Name),
					sourceToObj(c.Source),
					str(p.FromFieldPath),
					str(p.ToFieldPath),
					str(fromType),
					str(toType),
				}))
			}
		}
	}

	sections := []kl.Obj{
		sym("world"),
		sortedSection("crds", w.CRDs, crdCmp, crdToObj),
		sortedSection("xrds", w.XRDs, crdCmp, xrdToObj),
		section("compositions", compObjs),
		sortedSection("functions", w.Functions, functionCmp, functionToObj),
		sortedSection("providers", w.Providers, providerCmp, providerToObj),
		sortedSection("configurations", w.Configurations, configCmp, configToObj),
		sortedSection("resources", w.Resources, resourceCmp, resourceToObj),
		sortedSection("argo-apps", w.ArgoApps, argoAppCmp, argoAppToObj),
		sortedSection("argo-app-proj-links", w.ArgoApps, argoAppCmp, argoAppProjLinkToObj),
		sortedSection("argo-appprojects", w.ArgoProjects, argoAppProjectCmp, argoAppProjectToObj),
		section("schemas", nil),
		section("resolved-patches", patchObjs),
		sortedSection("mount-refs", w.MountRefs, mountRefCmp, mountRefToObj),
		sortedSection("sa-refs", w.SARefs, saRefCmp, saRefToObj),
		sortedSection("rbac-bindings", w.RBACBindings, rbacBindingCmp, rbacBindingToObj),
		sortedSection("rbac-rules", w.RBACRules, rbacRuleCmp, rbacRuleToObj),
		sortedSection("immutable-fields", w.ImmutableFields, immutableFieldCmp, immutableFieldToObj),
		trajectoryToObj(trajectories),
	}
	return makeList(sections)
}

func crdCmp(a, b types.CRDInfo) int {
	if c := cmp.Compare(a.Group, b.Group); c != 0 {
		return c
	}
	return cmp.Compare(a.Kind, b.Kind)
}

func compositionCmp(a, b types.CompositionInfo) int { return cmp.Compare(a.Name, b.Name) }
func functionCmp(a, b types.FunctionInfo) int       { return cmp.Compare(a.Name, b.Name) }
func providerCmp(a, b types.ProviderInfo) int       { return cmp.Compare(a.Name, b.Name) }
func configCmp(a, b types.ConfigurationInfo) int    { return cmp.Compare(a.Name, b.Name) }
func argoAppCmp(a, b types.ArgoApplication) int     { return cmp.Compare(a.Name, b.Name) }

func resourceCmp(a, b types.ResourceInfo) int {
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	return cmp.Compare(a.Name, b.Name)
}

func immutableFieldCmp(a, b types.ImmutableField) int {
	if c := cmp.Compare(a.Group, b.Group); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	return cmp.Compare(a.FieldPath, b.FieldPath)
}

func mountRefCmp(a, b types.MountRef) int {
	if c := cmp.Compare(a.OwnerKind, b.OwnerKind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.OwnerName, b.OwnerName); c != 0 {
		return c
	}
	if c := cmp.Compare(a.TargetKind, b.TargetKind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.TargetName, b.TargetName); c != 0 {
		return c
	}
	return cmp.Compare(a.MountKind, b.MountKind)
}

func saRefCmp(a, b types.SARef) int {
	if c := cmp.Compare(a.OwnerKind, b.OwnerKind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.OwnerName, b.OwnerName); c != 0 {
		return c
	}
	return cmp.Compare(a.SAName, b.SAName)
}

func rbacBindingCmp(a, b types.RBACBinding) int {
	if c := cmp.Compare(a.BindingKind, b.BindingKind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.BindingName, b.BindingName); c != 0 {
		return c
	}
	if c := cmp.Compare(a.SubjectKind, b.SubjectKind); c != 0 {
		return c
	}
	return cmp.Compare(a.SubjectName, b.SubjectName)
}

func rbacRuleCmp(a, b types.RBACRule) int {
	if c := cmp.Compare(a.OwnerKind, b.OwnerKind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.OwnerName, b.OwnerName); c != 0 {
		return c
	}
	if c := cmp.Compare(len(a.Verbs), len(b.Verbs)); c != 0 {
		return c
	}
	return cmp.Compare(strings.Join(a.Verbs, ","), strings.Join(b.Verbs, ","))
}

func crdToObj(crd types.CRDInfo) kl.Obj {
	var vers []kl.Obj
	for _, v := range crd.Versions {
		vers = append(vers, makeList([]kl.Obj{
			str(v.Name), boolean(v.Served), boolean(v.Storage), str(v.SchemaDigest),
		}))
	}
	cost := sym(strings.ToLower(string(crd.Conversion.CostClass)))
	conv := makeList([]kl.Obj{str(crd.Conversion.Strategy), cost, str(crd.Conversion.WebhookService)})
	return makeList([]kl.Obj{
		sym("crd-fact"),
		str(crd.Group), str(crd.Kind), str(crd.Scope),
		makeList(vers),
		conv,
		sourceToObj(crd.Source),
	})
}

func xrdToObj(xrd types.CRDInfo) kl.Obj {
	var vers []kl.Obj
	for _, v := range xrd.Versions {
		vers = append(vers, makeList([]kl.Obj{
			str(v.Name), boolean(v.Served), boolean(v.Referenceable), str(v.SchemaDigest),
		}))
	}
	return makeList([]kl.Obj{
		sym("xrd-fact"),
		str(xrd.Group), str(xrd.Kind), str(xrd.Scope), str(xrd.APIVersion),
		makeList(vers),
		sourceToObj(xrd.Source),
	})
}

func compositionToObj(comp types.CompositionInfo) kl.Obj {
	gvk := makeList([]kl.Obj{
		sym("gvk"),
		str(comp.CompositeTypeRef.Group),
		str(comp.CompositeTypeRef.Version),
		str(comp.CompositeTypeRef.Kind),
	})

	var steps []kl.Obj
	for _, s := range comp.Pipeline {
		steps = append(steps, makeList([]kl.Obj{
			str(s.Name), str(s.FunctionRef), str(s.InputAPIVersion), str(s.InputKind),
		}))
	}

	return makeList([]kl.Obj{
		sym("composition-fact"),
		str(comp.Name),
		gvk,
		str(comp.Mode),
		makeList(steps),
		sourceToObj(comp.Source),
	})
}

func functionToObj(fn types.FunctionInfo) kl.Obj {
	var vers []kl.Obj
	for _, v := range fn.InputVersions {
		vers = append(vers, str(v))
	}
	return makeList([]kl.Obj{
		sym("function-fact"),
		str(fn.Name), str(fn.Package),
		makeList(vers),
		sourceToObj(fn.Source),
	})
}

func providerToObj(p types.ProviderInfo) kl.Obj {
	return makeList([]kl.Obj{
		sym("provider-fact"),
		str(p.Name), str(p.Package),
		sourceToObj(p.Source),
	})
}

func configToObj(c types.ConfigurationInfo) kl.Obj {
	return makeList([]kl.Obj{
		sym("configuration-fact"),
		str(c.Name), str(c.Package),
		sourceToObj(c.Source),
	})
}

func resourceToObj(res types.ResourceInfo) kl.Obj {
	keys := slices.Sorted(maps.Keys(res.Annotations))
	var anns []kl.Obj
	for _, k := range keys {
		anns = append(anns, makeList([]kl.Obj{str(k), str(res.Annotations[k])}))
	}
	return makeList([]kl.Obj{
		sym("resource-fact"),
		str(res.APIVersion), str(res.Kind), str(res.Name), str(res.Namespace),
		makeList(anns),
		sourceToObj(res.Source),
	})
}

func argoAppToObj(app types.ArgoApplication) kl.Obj {
	var waves []kl.Obj
	for _, sw := range app.SyncWaves {
		waves = append(waves, makeList([]kl.Obj{
			str(sw.Kind), str(sw.Name), num(sw.Wave),
		}))
	}
	return makeList([]kl.Obj{
		sym("argo-app-fact"),
		str(app.Name), str(app.TrackingMode),
		makeList(waves),
		sourceToObj(app.Source),
	})
}

func argoAppProjectCmp(a, b types.ArgoAppProject) int {
	if c := cmp.Compare(a.Source.File, b.Source.File); c != 0 {
		return c
	}
	return cmp.Compare(a.Name, b.Name)
}

func argoGroupKindToObj(gk types.ArgoGroupKind) kl.Obj {
	return makeList([]kl.Obj{sym("group-kind"), str(gk.Group), str(gk.Kind)})
}

func argoAppProjectToObj(proj types.ArgoAppProject) kl.Obj {
	var cwl []kl.Obj
	for _, gk := range proj.ClusterResourceWhitelist {
		cwl = append(cwl, argoGroupKindToObj(gk))
	}
	var nwl []kl.Obj
	for _, gk := range proj.NamespaceResourceWhitelist {
		nwl = append(nwl, argoGroupKindToObj(gk))
	}
	return makeList([]kl.Obj{
		sym("argo-appproject"),
		str(proj.Name),
		str(proj.Source.File),
		makeList(cwl),
		makeList(nwl),
	})
}

func argoAppProjLinkToObj(app types.ArgoApplication) kl.Obj {
	proj := app.Project
	if proj == "" {
		proj = "default"
	}
	return makeList([]kl.Obj{
		sym("argo-app-proj-link"),
		str(app.Name),
		str(proj),
	})
}

func mountRefToObj(m types.MountRef) kl.Obj {
	return makeList([]kl.Obj{
		sym("mount-ref-fact"),
		str(m.OwnerKind), str(m.OwnerName), str(m.OwnerNamespace),
		str(m.TargetKind), str(m.TargetName), str(m.TargetNamespace),
		str(m.MountKind), boolean(m.Optional),
		sourceToObj(m.Source),
	})
}

func saRefToObj(s types.SARef) kl.Obj {
	return makeList([]kl.Obj{
		sym("sa-ref-fact"),
		str(s.OwnerKind), str(s.OwnerName), str(s.OwnerNamespace),
		str(s.SAName), str(s.SANamespace),
		sourceToObj(s.Source),
	})
}

func rbacBindingToObj(b types.RBACBinding) kl.Obj {
	return makeList([]kl.Obj{
		sym("rbac-binding-fact"),
		str(b.BindingKind), str(b.BindingName), str(b.BindingNamespace),
		str(b.SubjectKind), str(b.SubjectName), str(b.SubjectNamespace),
		str(b.RoleKind), str(b.RoleName),
		sourceToObj(b.Source),
	})
}

func rbacRuleToObj(r types.RBACRule) kl.Obj {
	return makeList([]kl.Obj{
		sym("rbac-rule-fact"),
		str(r.OwnerKind), str(r.OwnerName), str(r.OwnerNamespace),
		stringsToList(r.APIGroups),
		stringsToList(r.Resources),
		stringsToList(r.Verbs),
		stringsToList(r.ResourceNames),
		sourceToObj(r.Source),
	})
}

func immutableFieldToObj(f types.ImmutableField) kl.Obj {
	return makeList([]kl.Obj{
		sym("immutable-field-fact"),
		str(f.Group), str(f.Kind), str(f.FieldPath), str(f.Reason),
	})
}

func resourceKeyToObj(k trajectory.ResourceKey) kl.Obj {
	return makeList([]kl.Obj{
		sym("resource-key"),
		str(k.APIVersion), str(k.Kind), str(k.Namespace), str(k.Name),
	})
}

func resourceKeyObjs(keys []trajectory.ResourceKey) []kl.Obj {
	var out []kl.Obj
	for _, k := range keys {
		out = append(out, resourceKeyToObj(k))
	}
	return out
}

func deltaToObj(d trajectory.Delta) kl.Obj {
	return makeList([]kl.Obj{
		sym("delta"),
		section("created", resourceKeyObjs(d.Created)),
		section("updated", resourceKeyObjs(d.Updated)),
		section("deleted", resourceKeyObjs(d.Deleted)),
	})
}

func stepToObj(s trajectory.Step) kl.Obj {
	var stateKeys []trajectory.ResourceKey
	for k := range s.State.Resources {
		stateKeys = append(stateKeys, k)
	}
	sortResourceKeys(stateKeys)
	return makeList([]kl.Obj{
		sym("step"),
		str(s.AppName), num(s.Wave),
		deltaToObj(s.Delta),
		section("state", resourceKeyObjs(stateKeys)),
	})
}

func trajectoryToObj(steps []trajectory.Step) kl.Obj {
	var stepObjs []kl.Obj
	for _, s := range steps {
		stepObjs = append(stepObjs, stepToObj(s))
	}
	return section("trajectory", stepObjs)
}

func sortResourceKeys(keys []trajectory.ResourceKey) {
	slices.SortFunc(keys, func(a, b trajectory.ResourceKey) int {
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
}

func stringsToList(ss []string) kl.Obj {
	out := make([]kl.Obj, 0, len(ss))
	for _, s := range ss {
		out = append(out, str(s))
	}
	return makeList(out)
}

func sourceToObj(src types.SourceLocation) kl.Obj {
	return makeList([]kl.Obj{
		sym("source"),
		str(src.File),
		num(src.Line),
	})
}

// ---------------------------------------------------------------------------
// Shen judgment list → []types.Diagnostic
// ---------------------------------------------------------------------------

// objToDiagnostics walks the list of (judgment Code Sev Src Msg Detail Fix Related)
// tuples returned by check-world and decodes each into a Diagnostic.
func objToDiagnostics(o kl.Obj) []types.Diagnostic {
	var diags []types.Diagnostic
	for _, j := range kl.ListToSlice(o) {
		parts := kl.ListToSlice(j)
		if len(parts) < 8 {
			continue
		}
		head := parts[0]
		if !kl.IsSymbol(head) || kl.GetSymbol(head) != "judgment" {
			continue
		}
		code := objToString(parts[1])
		diags = append(diags, types.Diagnostic{
			Code:       code,
			Severity:   objToSeverity(parts[2]),
			Source:     objToSource(parts[3]),
			Message:    objToString(parts[4]),
			Detail:     objToString(parts[5]),
			Fix:        objToString(parts[6]),
			Related:    objToRelatedList(parts[7]),
			Obligation: obligationRefForCode(code),
		})
	}
	return diags
}

// obligationRefForCode maps an XPC code to its (Category, Generator) provenance.
// Codes outside the table return nil — legacy/unclassified diagnostics.
func obligationRefForCode(code string) *types.ObligationRef {
	type entry struct {
		category, generator string
	}
	table := map[string]entry{
		"XPC001": {"C", "version-coherence"},
		"XPC002": {"J", "conversion-cost-opt-in"},
		"XPC003": {"B", "comp-xrd-ref"},
		"XPC004": {"B", "pipeline-fn-ref"},
		"XPC005": {"B", "patch-compat"},
		"XPC006": {"F", "trajectory-wave-order"},
		"XPC007": {"G", "cross-app-label-tracking"},
		"XPC008": {"C", "crossplane-machinery-placement"},
		"XPC009": {"F", "trajectory-bootstrap"},
		"XPC010": {"K", "secret-source-sink"},
		"XPC011": {"L", "api-deprecation-calendar"},
		"XPC012": {"F", "no-dangling-mount"},
		"XPC013": {"F", "no-immutable-change"},
		"XPC014": {"F", "no-rbac-regression"},
	}
	e, ok := table[code]
	if !ok {
		return nil
	}
	return &types.ObligationRef{
		ID:        "XPC." + e.category + "." + e.generator,
		Category:  e.category,
		Generator: e.generator,
	}
}

func objToString(o kl.Obj) string {
	if kl.IsString(o) {
		return kl.GetString(o)
	}
	if kl.IsSymbol(o) {
		return kl.GetSymbol(o)
	}
	return kl.ObjString(o)
}

// severitySatisfied is an internal sentinel severity attached to "rule ran
// and found no violations" marker judgments emitted by Shen's mark-rule.
// The bridge filters these before returning diagnostics but counts them in
// RunResult.Satisfied / TotalObligations.
const severitySatisfied types.Severity = "satisfied"

func objToSeverity(o kl.Obj) types.Severity {
	if !kl.IsSymbol(o) {
		return types.SeverityError
	}
	switch kl.GetSymbol(o) {
	case "error":
		return types.SeverityError
	case "warn", "warning":
		return types.SeverityWarning
	case "info":
		return types.SeverityInfo
	case "satisfied":
		return severitySatisfied
	}
	return types.SeverityError
}

func objToSource(o kl.Obj) types.SourceLocation {
	parts := kl.ListToSlice(o)
	if len(parts) < 3 {
		return types.SourceLocation{}
	}
	// [source File Line] — parts[0] is the tag, [1] file, [2] line.
	line := 0
	if kl.IsNumber(parts[2]) {
		line = kl.GetInteger(parts[2])
	}
	return types.SourceLocation{
		File: objToString(parts[1]),
		Line: line,
	}
}

func objToRelatedList(o kl.Obj) []types.SourceLocation {
	var out []types.SourceLocation
	for _, e := range kl.ListToSlice(o) {
		out = append(out, objToSource(e))
	}
	return out
}

// ---------------------------------------------------------------------------
// RunResult assembly
// ---------------------------------------------------------------------------

// buildRunResult partitions the raw judgment stream into visible diagnostics
// (errors + warnings + info) and satisfied markers, and derives RunSummary
// counts. Each distinct obligation ID (e.g. XPC.J.conversion-cost-opt-in)
// counts once toward TotalObligations. Codes without an obligation mapping
// fall back to "XPC.<code>" as their ID.
func buildRunResult(all []types.Diagnostic) RunResult {
	seen := make(map[string]bool)
	var ids []string
	var visible []types.Diagnostic
	var violated, satisfied int
	for _, d := range all {
		id := "XPC." + d.Code
		if d.Obligation != nil && d.Obligation.ID != "" {
			id = d.Obligation.ID
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		if d.Severity == severitySatisfied {
			satisfied++
			continue
		}
		visible = append(visible, d)
		if d.Severity == types.SeverityError {
			violated++
		}
	}
	slices.Sort(ids)
	return RunResult{
		Diagnostics:      visible,
		TotalObligations: len(ids),
		Satisfied:        satisfied,
		Violated:         violated,
		ObligationIDs:    ids,
	}
}
