// Package checker bridges the Go side with the Shen type-checking kernel.
// The Shen kernel runs in-process via the embedded shen-go runtime — no
// subprocess, no external binary.
package checker

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pyrex41/cross-validate-/internal/shenfull"
	"github.com/pyrex41/cross-validate-/kernel"
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

	// RuleAllowlist, when non-empty, restricts kernel rule dispatch to
	// the listed rule codes. Empty (the default) means run all rules.
	// The kernel skips per-rule dispatches whose code isn't on the list,
	// so non-listed rules don't even compute satisfied markers.
	RuleAllowlist []string
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
//
// When kernelPath is empty the embedded kernel (kernel/*.shen baked into the
// binary via go:embed) is materialised into a content-addressed temp
// directory and loaded from there — so the xpc binary is self-contained and
// works without a kernel/ tree on disk. When kernelPath is non-empty it is
// honoured directly; this preserves the `--kernel-path` escape hatch.
func initShen(kernelPath string) error {
	shenOnce.Do(func() {
		if err := shenfull.Init(&shenCF); err != nil {
			shenErr = fmt.Errorf("shenfull.Init: %w", err)
			return
		}

		resolved, err := resolveOrMaterialiseKernel(kernelPath)
		if err != nil {
			shenErr = err
			return
		}

		absKernel, absErr := filepath.Abs(resolved)
		if absErr != nil {
			shenErr = fmt.Errorf("resolve kernel path: %w", absErr)
			return
		}

		// Shen's read-file-as-bytelist primitive opens files using the
		// literal path argument, so `(load "prelude.shen")` only works when
		// cwd is the kernel directory. Chdir into the kernel, do the load,
		// then chdir back so we don't perturb the caller's environment.
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

		// Mute the runtime banner emitted while rules register defines.
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

// resolveOrMaterialiseKernel returns the on-disk kernel directory.
// - explicitPath != "": honoured as-is (no embed extraction)
// - explicitPath == "": embedded kernel/*.shen are extracted to a stable
//   temp directory whose name is a hash of the embedded content. Re-runs of
//   xpc with the same kernel content reuse the same directory; a kernel
//   change produces a new directory and leaves the old one for /tmp turnover.
//
// We touch disk only because the AOT-compiled Shen prelude
// (internal/shenfull/reader.go) calls PrimReadFileAsByteList directly,
// bypassing symbol-level overrides. A future build-time AOT pass over
// kernel/*.shen would let us drop disk entirely.
func resolveOrMaterialiseKernel(explicitPath string) (string, error) {
	if explicitPath != "" {
		return explicitPath, nil
	}

	entries, err := fs.ReadDir(kernel.FS, ".")
	if err != nil {
		return "", fmt.Errorf("read embedded kernel: %w", err)
	}

	h := sha256.New()
	type fileBytes struct {
		name string
		data []byte
	}
	files := make([]fileBytes, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, ok := kernel.Read(e.Name())
		if !ok {
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00", e.Name(), len(data))
		h.Write(data)
		files = append(files, fileBytes{e.Name(), data})
	}
	digest := hex.EncodeToString(h.Sum(nil))[:16]
	dir := filepath.Join(os.TempDir(), "xpc-kernel-"+digest)

	marker := filepath.Join(dir, ".xpc-kernel-digest")
	if data, err := os.ReadFile(marker); err == nil && string(data) == digest {
		return dir, nil
	}

	staging, err := os.MkdirTemp(os.TempDir(), "xpc-kernel-stage-")
	if err != nil {
		return "", fmt.Errorf("create kernel staging dir: %w", err)
	}
	for _, f := range files {
		dst := filepath.Join(staging, f.name)
		if err := os.WriteFile(dst, f.data, 0o600); err != nil {
			os.RemoveAll(staging)
			return "", fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, ".xpc-kernel-digest"), []byte(digest), 0o600); err != nil {
		os.RemoveAll(staging)
		return "", fmt.Errorf("write digest marker: %w", err)
	}
	if err := os.Rename(staging, dir); err != nil {
		os.RemoveAll(staging)
		if _, statErr := os.Stat(filepath.Join(dir, "check.shen")); statErr == nil {
			return dir, nil
		}
		return "", fmt.Errorf("publish kernel dir: %w", err)
	}
	return dir, nil
}

// executablePath is overridable in tests; defaults to os.Executable.
var executablePath = os.Executable

// resolveKernelPath finds the kernel directory. Explicit paths are honoured
// as-is; empty paths are resolved by walking upwards from cwd looking for a
// "kernel" directory containing check.shen. If that fails, the search is
// repeated from the directory containing the running xpc executable, which
// lets `xpc check` and `xpc plan` work when invoked from a worktree or any
// other directory outside the cross-validate repo.
func resolveKernelPath(p string) (string, error) {
	if p != "" {
		return p, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if found, ok := searchKernelUpward(cwd); ok {
		return found, nil
	}
	// Fallback: search upward from the xpc executable's own directory.
	// Covers the "run from arbitrary cwd" case (plan worktree, `go install`
	// placed binary, invocation from a sibling repo) where the CWD-based
	// search finds nothing but the binary still sits above a kernel/ tree.
	if exe, exeErr := executablePath(); exeErr == nil {
		if resolved, rlErr := filepath.EvalSymlinks(exe); rlErr == nil {
			exe = resolved
		}
		if found, ok := searchKernelUpward(filepath.Dir(exe)); ok {
			return found, nil
		}
	}
	return "", fmt.Errorf("could not locate kernel directory (searched upwards from %s and from the xpc executable location)", cwd)
}

// searchKernelUpward walks upward from start looking for a directory
// containing kernel/check.shen. Returns (kernelDir, true) on success, empty
// + false if the root is reached without a match.
func searchKernelUpward(start string) (string, bool) {
	dir := start
	for {
		candidate := filepath.Join(dir, "kernel", "check.shen")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Join(dir, "kernel"), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
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
	timing := os.Getenv("XPC_TIMING") != ""
	tInit := time.Now()
	if err := initShen(cfg.KernelPath); err != nil {
		return RunResult{Diagnostics: []types.Diagnostic{{
			Code:     "XPC000",
			Severity: types.SeverityError,
			Message:  err.Error(),
		}}}
	}
	if timing {
		fmt.Fprintf(os.Stderr, "  [timing] init-shen   %v\n", time.Since(tInit))
	}

	tEnrich := time.Now()
	enrichSyncWaves(w)
	resolvePatchTypes(w)
	trajectories := trajectory.Simulate(w)
	if timing {
		fmt.Fprintf(os.Stderr, "  [timing] enrich      %v\n", time.Since(tEnrich))
	}

	tSerialize := time.Now()
	worldObj := worldToShenObj(w, trajectories, cfg.RuleAllowlist)
	if timing {
		fmt.Fprintf(os.Stderr, "  [timing] serialize   %v\n", time.Since(tSerialize))
	}

	tCall := time.Now()
	checkWorld := kl.PrimFunc(kl.MakeSymbol("check-world"))
	result := kl.Call(&shenCF, checkWorld, worldObj)
	if timing {
		fmt.Fprintf(os.Stderr, "  [timing] kernel-call %v\n", time.Since(tCall))
	}
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
//
// The XRD and CRD schemas are looked up through schemas.BuildSchemaIndex, the
// shared (apiVersion, kind) → schema index also consumed by
// ir.EnrichFieldValidation for R17. For XRDs we pick the composite type ref's
// explicit version; for composed CRDs we use the base.apiVersion directly.
func resolvePatchTypes(w *types.World) {
	index := schemas.BuildSchemaIndex(w)

	for ci, comp := range w.Compositions {
		xrdKey := schemas.SchemaKey{
			APIVersion: comp.CompositeTypeRef.Group + "/" + comp.CompositeTypeRef.Version,
			Kind:       comp.CompositeTypeRef.Kind,
		}
		xrdSchema := index[xrdKey]

		for ri, res := range comp.Resources {
			crdSchema := index[schemas.SchemaKey{APIVersion: res.Base.APIVersion, Kind: res.Base.Kind}]
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

func worldToShenObj(w *types.World, trajectories []trajectory.Step, ruleAllowlist []string) kl.Obj {
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
		sortedSection("appset-finalizer-facts", w.ArgoAppSets, appSetFinalizerCmp, appSetFinalizerToObj),
		sortedSection("appset-autosync-facts", w.ArgoAppSets, appSetFinalizerCmp, appSetAutosyncToObj),
		section("schemas", nil),
		section("resolved-patches", patchObjs),
		sortedSection("mount-refs", w.MountRefs, mountRefCmp, mountRefToObj),
		sortedSection("sa-refs", w.SARefs, saRefCmp, saRefToObj),
		sortedSection("rbac-bindings", w.RBACBindings, rbacBindingCmp, rbacBindingToObj),
		sortedSection("rbac-rules", w.RBACRules, rbacRuleCmp, rbacRuleToObj),
		sortedSection("immutable-fields", w.ImmutableFields, immutableFieldCmp, immutableFieldToObj),
		sortedSection("selector-mappings", w.SelectorMappings, selectorMappingCmp, selectorMappingToObj),
		sortedSection("selector-usages", w.SelectorUsages, selectorUsageCmp, selectorUsageToObj),
		sortedSection("late-init-mappings", w.LateInitMappings, lateInitMappingCmp, lateInitMappingToObj),
		sortedSection("late-init-usages", w.LateInitUsages, lateInitUsageCmp, lateInitUsageToObj),
		sortedSection("ignore-diff-entries", buildIgnoreDiffEntries(w.ArgoApps), ignoreDiffEntryCmp, ignoreDiffEntryToObj),
		sortedSection("resource-field-facts", w.ResourceFieldFacts, resourceFieldFactCmp, resourceFieldFactToObj),
		sortedSection("render-results", w.RenderResults, renderResultCmp, renderResultToObj),
		sortedSection("determinism-results", w.DeterminismResults, determinismResultCmp, determinismResultToObj),
		sortedSection("ssa-mp-conflicts", w.SSAMPConflicts, ssaMPConflictCmp, ssaMPConflictToObj),
		ssaMPModeSection(w.SSAMPMode),
		sortedSection("crossplane-deletion-policy-facts", w.CPDeletionPolicyFacts, cpDeletionPolicyCmp, cpDeletionPolicyToObj),
		prodPatternsSection(w.ProdPatterns),
		stringListSection("crossplane-state-needs-orphan-carveouts",
			w.NameCarveouts["crossplane-state-needs-orphan"]),
		stringListSection("rule-allowlist", ruleAllowlist),
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
		str(xrd.OwningApp),
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
		str(comp.OwningApp),
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
		str(fn.OwningApp),
	})
}

func providerToObj(p types.ProviderInfo) kl.Obj {
	return makeList([]kl.Obj{
		sym("provider-fact"),
		str(p.Name), str(p.Package),
		sourceToObj(p.Source),
		str(p.OwningApp),
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
		str(res.OwningApp),
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

// appSetAutosyncToObj emits one `(appset-autosync-fact Name AutoSym Src)`
// row per ApplicationSet. AutoSym is `auto-yes` when the template's
// syncPolicy has an `automated` block (any non-nil pointer counts — presence
// is the trigger, regardless of prune/selfHeal subfields), else `auto-no`.
// Same symbol-as-discriminator convention as R22/R23/R24.
func appSetAutosyncToObj(a types.ArgoApplicationSet) kl.Obj {
	autoSym := "auto-no"
	if a.Template.SyncPolicy.Automated != nil {
		autoSym = "auto-yes"
	}
	return makeList([]kl.Obj{
		sym("appset-autosync-fact"),
		str(a.Name),
		sym(autoSym),
		sourceToObj(a.Source),
	})
}

func appSetFinalizerCmp(a, b types.ArgoApplicationSet) int {
	if c := cmp.Compare(a.Source.File, b.Source.File); c != 0 {
		return c
	}
	return cmp.Compare(a.Name, b.Name)
}

// appSetFinalizerToObj emits one `(appset-finalizer-fact …)` row per
// ApplicationSet. PreserveOnDeletion is projected to `preserve-yes` /
// `preserve-no` symbols (never Shen booleans) so the Shen pattern match is a
// plain symbol compare — uppercase identifiers are Shen variables, so every
// discriminator stays lowercase-dashed. This matches the `ssa-yes`/`ssa-no`
// convention already established by R22.
func appSetFinalizerToObj(a types.ArgoApplicationSet) kl.Obj {
	preserveSym := "preserve-no"
	if a.SyncPolicy.PreserveResourcesOnDeletion {
		preserveSym = "preserve-yes"
	}
	finalizers := make([]kl.Obj, 0, len(a.Template.Finalizers))
	for _, f := range a.Template.Finalizers {
		finalizers = append(finalizers, str(f))
	}
	return makeList([]kl.Obj{
		sym("appset-finalizer-fact"),
		str(a.Name),
		makeList(finalizers),
		sym(preserveSym),
		sourceToObj(a.Source),
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
	// NOTE: RoleNamespace is emitted as a trailing positional slot, AFTER
	// RoleName and BEFORE source. Every Shen pattern matching on
	// `rbac-binding-fact` in kernel/ must be updated in lockstep.
	return makeList([]kl.Obj{
		sym("rbac-binding-fact"),
		str(b.BindingKind), str(b.BindingName), str(b.BindingNamespace),
		str(b.SubjectKind), str(b.SubjectName), str(b.SubjectNamespace),
		str(b.RoleKind), str(b.RoleName), str(b.RoleNamespace),
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

func selectorMappingCmp(a, b types.SelectorMapping) int {
	if c := cmp.Compare(a.Group, b.Group); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.SelectorPath, b.SelectorPath); c != 0 {
		return c
	}
	return cmp.Compare(a.ResolvedPath, b.ResolvedPath)
}

func selectorMappingToObj(m types.SelectorMapping) kl.Obj {
	return makeList([]kl.Obj{
		sym("selector-mapping-fact"),
		str(m.Group), str(m.Kind), str(m.SelectorPath), str(m.ResolvedPath), str(m.Reason),
	})
}

func selectorUsageCmp(a, b types.SelectorUsage) int {
	if c := cmp.Compare(a.ResourceKind, b.ResourceKind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.ResourceName, b.ResourceName); c != 0 {
		return c
	}
	return cmp.Compare(a.SelectorPath, b.SelectorPath)
}

func selectorUsageToObj(u types.SelectorUsage) kl.Obj {
	return makeList([]kl.Obj{
		sym("selector-usage-fact"),
		str(u.ResourceGroup), str(u.ResourceKind), str(u.ResourceName), str(u.ResourceNamespace),
		str(u.SelectorPath), str(u.ResolvedPath),
		sourceToObj(u.Source),
	})
}

func lateInitMappingCmp(a, b types.LateInitMapping) int {
	if c := cmp.Compare(a.Group, b.Group); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	return cmp.Compare(a.FieldPath, b.FieldPath)
}

func lateInitMappingToObj(m types.LateInitMapping) kl.Obj {
	return makeList([]kl.Obj{
		sym("late-init-mapping-fact"),
		str(m.Group), str(m.Kind), str(m.FieldPath), str(m.FixPattern), str(m.Reason),
	})
}

func lateInitUsageCmp(a, b types.LateInitUsage) int {
	if c := cmp.Compare(a.ResourceKind, b.ResourceKind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.ResourceName, b.ResourceName); c != 0 {
		return c
	}
	return cmp.Compare(a.FieldPath, b.FieldPath)
}

func lateInitUsageToObj(u types.LateInitUsage) kl.Obj {
	return makeList([]kl.Obj{
		sym("late-init-usage-fact"),
		str(u.ResourceGroup), str(u.ResourceKind), str(u.ResourceName), str(u.ResourceNamespace),
		str(u.FieldPath),
		sourceToObj(u.Source),
	})
}

func resourceFieldFactCmp(a, b types.ResourceFieldFact) int {
	if c := cmp.Compare(a.APIVersion, b.APIVersion); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Name, b.Name); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Path, b.Path); c != 0 {
		return c
	}
	return cmp.Compare(string(a.Violation), string(b.Violation))
}

func resourceFieldFactToObj(f types.ResourceFieldFact) kl.Obj {
	return makeList([]kl.Obj{
		sym("resource-field-fact"),
		str(f.APIVersion), str(f.Kind), str(f.Namespace), str(f.Name),
		str(f.Path),
		sym(violationSym(f.Violation)),
		str(f.Message),
		sourceToObj(f.Source),
	})
}

// violationSym maps a ViolationKind to a lowercase, dashed symbol name used
// in Shen patterns. Lowercase symbols avoid Shen's pattern-match convention
// where uppercase identifiers are variables.
func violationSym(v types.ViolationKind) string {
	switch v {
	case types.ViolationUnknownField:
		return "unknown-field"
	case types.ViolationWrongType:
		return "wrong-type"
	case types.ViolationMissingRequired:
		return "missing-required"
	case types.ViolationInvalidEnum:
		return "invalid-enum"
	}
	return "unknown"
}

// renderResultCmp orders RenderResults deterministically so the Shen
// `render-results` section is stable across runs.
func renderResultCmp(a, b types.RenderResult) int {
	if c := cmp.Compare(a.AppName, b.AppName); c != 0 {
		return c
	}
	return cmp.Compare(a.ChartPath, b.ChartPath)
}

// renderResultToObj serializes one RenderResult as a Shen s-expression of
// the shape Shen rule R18/R19 expect to pattern-match. All discriminator
// tags are lowercase-dashed symbols (uppercase identifiers are Shen
// variables).
func renderResultToObj(r types.RenderResult) kl.Obj {
	// Use distinct discriminator symbols rather than Shen's built-in
	// true/false booleans so the Shen pattern-match stays a pure
	// symbol-compare.
	successSym := "render-failed"
	if r.Success {
		successSym = "render-ok"
	}
	errorKind := r.ErrorKind
	if errorKind == "" {
		if r.Success {
			errorKind = "none"
		} else {
			errorKind = "other"
		}
	}

	issueObjs := make([]kl.Obj, 0, len(r.ValuesIssues))
	for _, vi := range r.ValuesIssues {
		issueObjs = append(issueObjs, makeList([]kl.Obj{
			sym("values-issue"),
			str(vi.Path),
			str(vi.Message),
		}))
	}
	issuesList := makeList(append([]kl.Obj{sym("values-issues")}, issueObjs...))

	return makeList([]kl.Obj{
		sym("render-result"),
		str(r.AppName),
		str(r.ChartPath),
		sym(successSym),
		sym(errorKind),
		str(r.Error),
		issuesList,
		sourceToObj(r.Source),
	})
}

// determinismResultCmp orders DeterminismResults deterministically so the
// Shen `determinism-results` section is stable across runs.
func determinismResultCmp(a, b types.DeterminismResult) int {
	if c := cmp.Compare(a.AppName, b.AppName); c != 0 {
		return c
	}
	return cmp.Compare(a.RendererKind, b.RendererKind)
}

// determinismResultToObj serializes one DeterminismResult as the
// s-expression Shen rule R20 expects to pattern-match. The Mismatch bool is
// projected into a lowercase-dashed discriminator symbol (`determ-match` /
// `determ-mismatch`) so Shen's pattern match stays a plain symbol compare —
// Shen's literal true/false booleans would be interpreted specially.
func determinismResultToObj(d types.DeterminismResult) kl.Obj {
	statusSym := "determ-match"
	if d.Mismatch {
		statusSym = "determ-mismatch"
	}
	return makeList([]kl.Obj{
		sym("determinism-result"),
		str(d.AppName),
		str(d.RendererKind),
		sym(statusSym),
		str(d.DiffSummary),
		sourceToObj(d.Source),
	})
}

// ssaMPConflictCmp orders SSAMPConflicts deterministically so the Shen
// `ssa-mp-conflicts` section is stable across runs.
func ssaMPConflictCmp(a, b types.SSAMPConflict) int {
	if c := cmp.Compare(a.AppName, b.AppName); c != 0 {
		return c
	}
	if c := cmp.Compare(a.ResourceKind, b.ResourceKind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.ResourceNamespace, b.ResourceNamespace); c != 0 {
		return c
	}
	return cmp.Compare(a.ResourceName, b.ResourceName)
}

// ssaMPConflictToObj serializes one SSAMPConflict as the s-expression Shen
// rule R22 expects to pattern-match. The boolean ServerSideApply is
// projected to `ssa-yes` / `ssa-no` symbols (never Shen booleans) so the
// pattern match is a plain symbol compare — uppercase identifiers are Shen
// variables, so every discriminator stays lowercase-dashed.
func ssaMPConflictToObj(c types.SSAMPConflict) kl.Obj {
	ssaSym := "ssa-no"
	if c.ServerSideApply {
		ssaSym = "ssa-yes"
	}
	policyObjs := make([]kl.Obj, 0, len(c.ManagementPolicies))
	for _, p := range c.ManagementPolicies {
		policyObjs = append(policyObjs, str(p))
	}
	return makeList([]kl.Obj{
		sym("ssa-mp-conflict-fact"),
		str(c.AppName),
		sym(ssaSym),
		makeList(policyObjs),
		str(c.ResourceGroup),
		str(c.ResourceKind),
		str(c.ResourceName),
		str(c.ResourceNamespace),
		sourceToObj(c.Source),
	})
}

// cpDeletionPolicyCmp orders CPDeletionPolicyFacts deterministically so the
// Shen `crossplane-deletion-policy-facts` section is stable across runs.
func cpDeletionPolicyCmp(a, b types.CPDeletionPolicyFact) int {
	if c := cmp.Compare(a.Group, b.Group); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
		return c
	}
	return cmp.Compare(a.Name, b.Name)
}

// cpDeletionPolicyToObj serializes one CPDeletionPolicyFact as the s-expression
// Shen rule R23 expects to pattern-match. Bypass is projected to
// `bypass-yes` / `bypass-no` symbols (never Shen booleans) so the pattern match
// is a plain symbol compare — uppercase identifiers are Shen variables, so
// every discriminator stays lowercase-dashed. Same convention as R22's
// `ssa-yes`/`ssa-no` and R24's `preserve-yes`/`preserve-no`.
func cpDeletionPolicyToObj(f types.CPDeletionPolicyFact) kl.Obj {
	bypassSym := "bypass-no"
	if f.Bypass {
		bypassSym = "bypass-yes"
	}
	return makeList([]kl.Obj{
		sym("cp-deletion-policy-fact"),
		str(f.Group), str(f.Kind),
		str(f.Name), str(f.Namespace),
		str(f.DeletionPolicy),
		sym(bypassSym),
		sourceToObj(f.Source),
	})
}

// ssaMPModeSection emits the R22 mode as a single-symbol section the Shen
// kernel can extract with `extract-section`. Empty or unknown modes fall
// back to `observe` — the narrowest setting — so a bridge bug never
// silently upgrades diagnostic coverage. The symbol is always
// lowercase-dashed (observe/partial/any) so it doesn't collide with Shen's
// uppercase-variable convention.
func ssaMPModeSection(mode string) kl.Obj {
	normalized := "observe"
	switch mode {
	case "observe", "partial", "any":
		normalized = mode
	}
	return makeList([]kl.Obj{
		sym("ssa-mp-mode"),
		sym(normalized),
	})
}

// prodPatternsSection emits R25's resolved substring list as a single section.
// Shape: (prod-patterns "-prod" "prod-" ...). Each element is a Shen string
// because Shen's string-contains? primitive operates on strings, not symbols.
// Empty list emits (prod-patterns) — the kernel treats that as "match
// nothing", which is the safe failure mode (no false fires).
func prodPatternsSection(patterns []string) kl.Obj {
	return stringListSection("prod-patterns", patterns)
}

// stringListSection emits a generic (tag "s1" "s2" ...) section. Used for
// R23's name-carveout list and any other flat string-list sections that
// don't need their own dedicated converter.
func stringListSection(tag string, items []string) kl.Obj {
	objs := make([]kl.Obj, 0, len(items))
	for _, s := range items {
		objs = append(objs, str(s))
	}
	return makeList(append([]kl.Obj{sym(tag)}, objs...))
}

// buildIgnoreDiffEntries flattens the ignoreDifferences of all ArgoApplications
// into a list of IgnoreDiffEntry values, one per JSONPointer and one per
// JQPathExpression. If both are empty, a single entry with empty strings is
// emitted so the kernel can still see the group/kind scope.
func buildIgnoreDiffEntries(apps []types.ArgoApplication) []types.IgnoreDiffEntry {
	var out []types.IgnoreDiffEntry
	for _, app := range apps {
		for _, diff := range app.IgnoreDifferences {
			emitted := false
			for _, jp := range diff.JSONPointers {
				out = append(out, types.IgnoreDiffEntry{
					AppName:     app.Name,
					Group:       diff.Group,
					Kind:        diff.Kind,
					JSONPointer: jp,
					JQPath:      "",
				})
				emitted = true
			}
			for _, jq := range diff.JQPathExpressions {
				out = append(out, types.IgnoreDiffEntry{
					AppName:     app.Name,
					Group:       diff.Group,
					Kind:        diff.Kind,
					JSONPointer: "",
					JQPath:      jq,
				})
				emitted = true
			}
			if !emitted {
				// Preserve the scope entry even when no path expressions are set.
				out = append(out, types.IgnoreDiffEntry{
					AppName: app.Name,
					Group:   diff.Group,
					Kind:    diff.Kind,
				})
			}
		}
	}
	return out
}

func ignoreDiffEntryCmp(a, b types.IgnoreDiffEntry) int {
	if c := cmp.Compare(a.AppName, b.AppName); c != 0 {
		return c
	}
	if c := cmp.Compare(a.JSONPointer, b.JSONPointer); c != 0 {
		return c
	}
	return cmp.Compare(a.JQPath, b.JQPath)
}

func ignoreDiffEntryToObj(e types.IgnoreDiffEntry) kl.Obj {
	return makeList([]kl.Obj{
		sym("ignore-diff-entry"),
		str(e.AppName), str(e.Group), str(e.Kind),
		str(e.JSONPointer), str(e.JQPath),
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
