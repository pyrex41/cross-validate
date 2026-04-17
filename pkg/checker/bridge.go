// Package checker bridges the Go side with the Shen type-checking kernel.
// The Shen kernel runs in-process via the embedded shen-go runtime — no
// subprocess, no external binary.
package checker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

func worldToShenObj(w *types.World, trajectories []trajectory.Step) kl.Obj {
	// Copy and sort each slice so the serialized world is deterministic.

	crds := append([]types.CRDInfo(nil), w.CRDs...)
	sort.Slice(crds, func(i, j int) bool {
		if crds[i].Group != crds[j].Group {
			return crds[i].Group < crds[j].Group
		}
		return crds[i].Kind < crds[j].Kind
	})

	xrds := append([]types.CRDInfo(nil), w.XRDs...)
	sort.Slice(xrds, func(i, j int) bool {
		if xrds[i].Group != xrds[j].Group {
			return xrds[i].Group < xrds[j].Group
		}
		return xrds[i].Kind < xrds[j].Kind
	})

	comps := append([]types.CompositionInfo(nil), w.Compositions...)
	sort.Slice(comps, func(i, j int) bool { return comps[i].Name < comps[j].Name })

	fns := append([]types.FunctionInfo(nil), w.Functions...)
	sort.Slice(fns, func(i, j int) bool { return fns[i].Name < fns[j].Name })

	provs := append([]types.ProviderInfo(nil), w.Providers...)
	sort.Slice(provs, func(i, j int) bool { return provs[i].Name < provs[j].Name })

	cfgs := append([]types.ConfigurationInfo(nil), w.Configurations...)
	sort.Slice(cfgs, func(i, j int) bool { return cfgs[i].Name < cfgs[j].Name })

	ress := append([]types.ResourceInfo(nil), w.Resources...)
	sort.Slice(ress, func(i, j int) bool {
		if ress[i].Kind != ress[j].Kind {
			return ress[i].Kind < ress[j].Kind
		}
		return ress[i].Name < ress[j].Name
	})

	apps := append([]types.ArgoApplication(nil), w.ArgoApps...)
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })

	// Build section facts.
	var crdObjs []kl.Obj
	for _, c := range crds {
		crdObjs = append(crdObjs, crdToObj(c))
	}
	var xrdObjs []kl.Obj
	for _, x := range xrds {
		xrdObjs = append(xrdObjs, xrdToObj(x))
	}
	var compObjs []kl.Obj
	for _, c := range comps {
		compObjs = append(compObjs, compositionToObj(c))
	}
	var fnObjs []kl.Obj
	for _, f := range fns {
		fnObjs = append(fnObjs, functionToObj(f))
	}
	var provObjs []kl.Obj
	for _, p := range provs {
		provObjs = append(provObjs, providerToObj(p))
	}
	var cfgObjs []kl.Obj
	for _, c := range cfgs {
		cfgObjs = append(cfgObjs, configToObj(c))
	}
	var resObjs []kl.Obj
	for _, r := range ress {
		resObjs = append(resObjs, resourceToObj(r))
	}
	var appObjs []kl.Obj
	for _, a := range apps {
		appObjs = append(appObjs, argoAppToObj(a))
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

	mounts := append([]types.MountRef(nil), w.MountRefs...)
	sort.Slice(mounts, func(i, j int) bool { return mountRefLess(mounts[i], mounts[j]) })
	var mountObjs []kl.Obj
	for _, m := range mounts {
		mountObjs = append(mountObjs, mountRefToObj(m))
	}

	sas := append([]types.SARef(nil), w.SARefs...)
	sort.Slice(sas, func(i, j int) bool { return saRefLess(sas[i], sas[j]) })
	var saObjs []kl.Obj
	for _, s := range sas {
		saObjs = append(saObjs, saRefToObj(s))
	}

	bindings := append([]types.RBACBinding(nil), w.RBACBindings...)
	sort.Slice(bindings, func(i, j int) bool { return rbacBindingLess(bindings[i], bindings[j]) })
	var bindingObjs []kl.Obj
	for _, b := range bindings {
		bindingObjs = append(bindingObjs, rbacBindingToObj(b))
	}

	rules := append([]types.RBACRule(nil), w.RBACRules...)
	sort.Slice(rules, func(i, j int) bool { return rbacRuleLess(rules[i], rules[j]) })
	var ruleObjs []kl.Obj
	for _, r := range rules {
		ruleObjs = append(ruleObjs, rbacRuleToObj(r))
	}

	immutables := append([]types.ImmutableField(nil), w.ImmutableFields...)
	sort.Slice(immutables, func(i, j int) bool {
		if immutables[i].Group != immutables[j].Group {
			return immutables[i].Group < immutables[j].Group
		}
		if immutables[i].Kind != immutables[j].Kind {
			return immutables[i].Kind < immutables[j].Kind
		}
		return immutables[i].FieldPath < immutables[j].FieldPath
	})
	var immutableObjs []kl.Obj
	for _, f := range immutables {
		immutableObjs = append(immutableObjs, immutableFieldToObj(f))
	}

	sections := []kl.Obj{
		sym("world"),
		section("crds", crdObjs),
		section("xrds", xrdObjs),
		section("compositions", compObjs),
		section("functions", fnObjs),
		section("providers", provObjs),
		section("configurations", cfgObjs),
		section("resources", resObjs),
		section("argo-apps", appObjs),
		section("schemas", nil),
		section("resolved-patches", patchObjs),
		section("mount-refs", mountObjs),
		section("sa-refs", saObjs),
		section("rbac-bindings", bindingObjs),
		section("rbac-rules", ruleObjs),
		section("immutable-fields", immutableObjs),
		trajectoryToObj(trajectories),
	}
	return makeList(sections)
}

func mountRefLess(a, b types.MountRef) bool {
	if a.OwnerKind != b.OwnerKind {
		return a.OwnerKind < b.OwnerKind
	}
	if a.OwnerName != b.OwnerName {
		return a.OwnerName < b.OwnerName
	}
	if a.TargetKind != b.TargetKind {
		return a.TargetKind < b.TargetKind
	}
	if a.TargetName != b.TargetName {
		return a.TargetName < b.TargetName
	}
	return a.MountKind < b.MountKind
}

func saRefLess(a, b types.SARef) bool {
	if a.OwnerKind != b.OwnerKind {
		return a.OwnerKind < b.OwnerKind
	}
	if a.OwnerName != b.OwnerName {
		return a.OwnerName < b.OwnerName
	}
	return a.SAName < b.SAName
}

func rbacBindingLess(a, b types.RBACBinding) bool {
	if a.BindingKind != b.BindingKind {
		return a.BindingKind < b.BindingKind
	}
	if a.BindingName != b.BindingName {
		return a.BindingName < b.BindingName
	}
	if a.SubjectKind != b.SubjectKind {
		return a.SubjectKind < b.SubjectKind
	}
	return a.SubjectName < b.SubjectName
}

func rbacRuleLess(a, b types.RBACRule) bool {
	if a.OwnerKind != b.OwnerKind {
		return a.OwnerKind < b.OwnerKind
	}
	if a.OwnerName != b.OwnerName {
		return a.OwnerName < b.OwnerName
	}
	if len(a.Verbs) != len(b.Verbs) {
		return len(a.Verbs) < len(b.Verbs)
	}
	return strings.Join(a.Verbs, ",") < strings.Join(b.Verbs, ",")
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
	keys := make([]string, 0, len(res.Annotations))
	for k := range res.Annotations {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		if keys[i].Namespace != keys[j].Namespace {
			return keys[i].Namespace < keys[j].Namespace
		}
		return keys[i].Name < keys[j].Name
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
	sort.Strings(ids)
	return RunResult{
		Diagnostics:      visible,
		TotalObligations: len(ids),
		Satisfied:        satisfied,
		Violated:         violated,
		ObligationIDs:    ids,
	}
}
