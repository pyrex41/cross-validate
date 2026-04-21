// Package ir builds the typed intermediate representation (World) from
// loaded YAML documents. This is the bridge between raw YAML and the
// type checker.
package ir

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/renderer"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Builder constructs a World from loaded documents.
type Builder struct {
	world *types.World

	// SkipRender suppresses Helm and Kustomize rendering. Existing tests
	// that only exercise direct-manifest rules stay hermetic when this is
	// true; callers that want rendered resources in the World set it to
	// false and provide binaries on PATH.
	SkipRender bool
	// HelmBin is the path to the helm binary. Empty means "look up helm on
	// PATH when first needed".
	HelmBin string
	// KustomizeBin is the path to the kustomize binary. Empty means "look
	// up kustomize on PATH when first needed".
	KustomizeBin string
	// SkipAppSetExpand suppresses the ExpandAppSet pass that synthesizes
	// ArgoApplications from ApplicationSet generators. Default is "expand";
	// CLI surfaces --skip-appset-expand for opt-out.
	SkipAppSetExpand bool
	HelmCacheDir     string
	// AppSetContext provides fixtures for generator kinds that can't be
	// simulated offline (pullRequest, scmProvider). Nil is fine; only the
	// PRFixtures-dependent code paths will degrade to an info diag.
	AppSetContext ExpansionContext
	// helmRenderer is constructed lazily on first render attempt.
	helmRenderer *renderer.HelmRenderer
	// kustomizeRenderer is constructed lazily on first render attempt.
	kustomizeRenderer *renderer.KustomizeRenderer
	// ExpansionDiags collects info-level diagnostics produced during
	// AppSet expansion (e.g. unsupported generator kinds). The caller
	// merges these into the final diagnostic stream.
	ExpansionDiags []types.Diagnostic
}

// NewBuilder creates a new IR builder.
func NewBuilder() *Builder {
	return &Builder{
		world: types.NewWorld(),
	}
}

// Build processes all loaded documents and returns the World.
func (b *Builder) Build(docs []loader.LoadedDocument) (*types.World, error) {
	for _, doc := range docs {
		category := loader.ClassifyDocument(doc)
		var err error
		switch category {
		case "crd":
			err = b.addCRD(doc)
		case "xrd":
			err = b.addXRD(doc)
		case "composition":
			err = b.addComposition(doc)
		case "function":
			err = b.addFunction(doc)
		case "provider":
			err = b.addProvider(doc)
		case "configuration":
			err = b.addConfiguration(doc)
		case "argo-application":
			err = b.addArgoApplication(doc)
		case "argo-appproject":
			err = b.addArgoAppProject(doc)
		case "argo-applicationset":
			err = b.addArgoApplicationSet(doc)
		case "resource":
			err = b.addResource(doc)
		}
		if err != nil {
			return nil, fmt.Errorf("processing %s %s from %s:%d: %w",
				doc.Kind, getName(doc.Raw), doc.Source.File, doc.Source.Line, err)
		}
	}
	// Expand ApplicationSets into synthetic ArgoApplications BEFORE the
	// enrichment passes so downstream analysis (trajectory, field
	// validation, later render hooks) sees the expanded fleet. Rendering
	// of each synthetic Application happens here too — otherwise the
	// render-results section would only cover AppSet-authored
	// Applications rendered on some *later* invocation.
	if !b.SkipAppSetExpand {
		b.expandAppSets()
	}
	enrichOwningApp(b.world)
	EnrichTrajectoryData(b.world)
	EnrichFieldValidation(b.world)
	if !b.SkipRender {
		b.runDeterminismChecks()
	}
	return b.world, nil
}

// ownerPrefix pairs an absolute directory prefix with the Argo Application
// that claims it. Longest-match wins when resolving a fact's source file so
// the more specific prefix disambiguates nested apps.
type ownerPrefix struct {
	dir     string
	appName string
}

// buildOwnerPrefixes collects path prefixes for every ArgoApplication. Two
// prefixes are registered per app:
//  1. filepath.Join(repoRoot|appDir, src.Path) — the ideal case where an
//     app's repo-relative spec.source.path corresponds to a physical
//     subdirectory on disk. This is the fg-manifold layout.
//  2. filepath.Dir(app.Source.File) — fallback for co-located fixtures and
//     simpler layouts where app YAML and manifests live in the same dir.
func buildOwnerPrefixes(w *types.World) []ownerPrefix {
	var prefixes []ownerPrefix
	for _, app := range w.ArgoApps {
		appDir := filepath.Dir(app.Source.File)
		absAppDir, err := filepath.Abs(appDir)
		if err == nil {
			prefixes = append(prefixes, ownerPrefix{dir: absAppDir, appName: app.Name})
		}
		repoRoot := findRepoRoot(absAppDir)
		for _, src := range app.Sources {
			if src.Renderer != types.RendererDirectory {
				continue
			}
			if src.Path == "" {
				continue
			}
			if repoRoot != "" {
				if abs, err := filepath.Abs(filepath.Join(repoRoot, src.Path)); err == nil {
					prefixes = append(prefixes, ownerPrefix{dir: abs, appName: app.Name})
				}
			}
			if abs, err := filepath.Abs(filepath.Join(appDir, src.Path)); err == nil {
				prefixes = append(prefixes, ownerPrefix{dir: abs, appName: app.Name})
			}
		}
	}
	return prefixes
}

// resolveOwner returns the app name whose prefix best (longest) contains
// file, or "" if no prefix matches.
func resolveOwner(file string, prefixes []ownerPrefix) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return ""
	}
	best := ""
	bestLen := -1
	for _, p := range prefixes {
		if !pathUnder(abs, p.dir) {
			continue
		}
		if len(p.dir) > bestLen {
			bestLen = len(p.dir)
			best = p.appName
		}
	}
	return best
}

// enrichOwningApp assigns OwningApp on every fact type that carries one
// (ResourceInfo, CRDInfo, CompositionInfo, FunctionInfo, ProviderInfo) by
// matching each fact's source file against the prefix set derived from the
// ArgoApplications in w. Facts that already have OwningApp set (rendered
// resources with "rendered:helm:<app>" provenance) are left alone.
//
// Facts whose file isn't under any app's prefix remain unowned (empty
// OwningApp), and per-app rules (R15, R6a/b/c/d) simply skip them rather
// than cartesian-blaming every Application.
func enrichOwningApp(w *types.World) {
	if len(w.ArgoApps) == 0 {
		return
	}
	prefixes := buildOwnerPrefixes(w)
	if len(prefixes) == 0 {
		return
	}
	for i := range w.Resources {
		if w.Resources[i].OwningApp != "" {
			continue
		}
		if owner := resolveOwner(w.Resources[i].Source.File, prefixes); owner != "" {
			w.Resources[i].OwningApp = owner
		}
	}
	for i := range w.CRDs {
		if w.CRDs[i].OwningApp != "" {
			continue
		}
		if owner := resolveOwner(w.CRDs[i].Source.File, prefixes); owner != "" {
			w.CRDs[i].OwningApp = owner
		}
	}
	for i := range w.XRDs {
		if w.XRDs[i].OwningApp != "" {
			continue
		}
		if owner := resolveOwner(w.XRDs[i].Source.File, prefixes); owner != "" {
			w.XRDs[i].OwningApp = owner
		}
	}
	for i := range w.Compositions {
		if w.Compositions[i].OwningApp != "" {
			continue
		}
		if owner := resolveOwner(w.Compositions[i].Source.File, prefixes); owner != "" {
			w.Compositions[i].OwningApp = owner
		}
	}
	for i := range w.Functions {
		if w.Functions[i].OwningApp != "" {
			continue
		}
		if owner := resolveOwner(w.Functions[i].Source.File, prefixes); owner != "" {
			w.Functions[i].OwningApp = owner
		}
	}
	for i := range w.Providers {
		if w.Providers[i].OwningApp != "" {
			continue
		}
		if owner := resolveOwner(w.Providers[i].Source.File, prefixes); owner != "" {
			w.Providers[i].OwningApp = owner
		}
	}
}

// findRepoRoot walks up from dir looking for a .git directory and returns
// the first ancestor that contains one. Returns "" when nothing is found
// (dir isn't inside a git repo) — callers should skip the repo-relative
// resolution in that case.
func findRepoRoot(dir string) string {
	if dir == "" {
		return ""
	}
	cur := dir
	for {
		if info, err := os.Stat(filepath.Join(cur, ".git")); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// pathUnder reports whether file is inside dir (or equals it).
func pathUnder(file, dir string) bool {
	if file == dir {
		return true
	}
	if !strings.HasPrefix(file, dir) {
		return false
	}
	// Require a separator after dir so /a/b doesn't match /a/bar.
	return file[len(dir)] == os.PathSeparator
}

// expandAppSets walks every parsed ApplicationSet, expands its generators
// into concrete ArgoApplications, and runs the normal render hooks on each
// synthetic Application. Any info-level diagnostics produced by expansion
// (unsupported generators, unrenderable templates) are stashed on
// Builder.ExpansionDiags for the caller to merge into the final stream.
func (b *Builder) expandAppSets() {
	for _, as := range b.world.ArgoAppSets {
		res := ExpandAppSet(as, b.AppSetContext)
		b.ExpansionDiags = append(b.ExpansionDiags, res.Diagnostics...)
		for _, app := range res.Applications {
			b.world.ArgoApps = append(b.world.ArgoApps, app)
			if !b.SkipRender {
				b.renderHelmSources(app, as.Source.File)
				b.renderKustomizeSources(app, as.Source.File)
			}
		}
	}
}

func (b *Builder) addCRD(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	if spec == nil {
		return fmt.Errorf("CRD missing spec")
	}

	group, _ := spec["group"].(string)
	names := getMap(spec, "names")
	kind := ""
	if names != nil {
		kind, _ = names["kind"].(string)
	}
	scope, _ := spec["scope"].(string)

	crd := types.CRDInfo{
		Group:  group,
		Kind:   kind,
		Scope:  scope,
		Source: doc.Source,
	}

	versions := getSlice(spec, "versions")
	for _, v := range versions {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := vm["name"].(string)
		served, _ := vm["served"].(bool)
		storage, _ := vm["storage"].(bool)

		ver := types.CRDVersion{
			Name:    name,
			Served:  served,
			Storage: storage,
		}

		// Hash the schema for deduplication
		if schema := getMap(vm, "schema"); schema != nil {
			digest := hashSchema(schema)
			ver.SchemaDigest = digest
			b.world.Schemas[digest] = types.SchemaInfo{
				Digest: digest,
				Schema: schema,
			}
		}

		crd.Versions = append(crd.Versions, ver)
	}

	// Parse conversion info
	conv := getMap(spec, "conversion")
	if conv != nil {
		strategy, _ := conv["strategy"].(string)
		crd.Conversion.Strategy = strategy
		switch strategy {
		case "Webhook":
			crd.Conversion.CostClass = types.CostClassWebhook
			// Try to extract webhook service info
			if wh := getMap(conv, "webhook"); wh != nil {
				if cc := getMap(wh, "clientConfig"); cc != nil {
					if svc := getMap(cc, "service"); svc != nil {
						ns, _ := svc["namespace"].(string)
						name, _ := svc["name"].(string)
						crd.Conversion.WebhookService = ns + "/" + name
					}
				}
			}
		case "None", "":
			if len(crd.Versions) <= 1 {
				crd.Conversion.CostClass = types.CostClassNone
			} else {
				// Check if schemas are identical across versions
				allSame := true
				var firstDigest string
				for i, v := range crd.Versions {
					if i == 0 {
						firstDigest = v.SchemaDigest
					} else if v.SchemaDigest != firstDigest {
						allSame = false
						break
					}
				}
				if allSame {
					crd.Conversion.CostClass = types.CostClassIdentity
				} else {
					crd.Conversion.CostClass = types.CostClassStructural
				}
			}
		}
	} else {
		crd.Conversion.CostClass = types.CostClassNone
	}

	b.world.CRDs = append(b.world.CRDs, crd)
	return nil
}

func (b *Builder) addXRD(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	if spec == nil {
		return fmt.Errorf("XRD missing spec")
	}

	group, _ := spec["group"].(string)
	names := getMap(spec, "names")
	kind := ""
	if names != nil {
		kind, _ = names["kind"].(string)
	}
	claimNames := getMap(spec, "claimNames")
	scope := "Cluster"
	if claimNames != nil {
		scope = "Namespaced"
	}

	xrd := types.CRDInfo{
		Group:      group,
		Kind:       kind,
		Scope:      scope,
		Source:     doc.Source,
		IsXRD:      true,
		APIVersion: doc.APIVersion,
	}

	versions := getSlice(spec, "versions")
	for _, v := range versions {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := vm["name"].(string)
		served, _ := vm["served"].(bool)
		referenceable, _ := vm["referenceable"].(bool)

		ver := types.CRDVersion{
			Name:          name,
			Served:        served,
			Storage:       referenceable, // In XRDs, referenceable ≈ storage
			Referenceable: referenceable,
		}

		if schema := getMap(vm, "schema"); schema != nil {
			digest := hashSchema(schema)
			ver.SchemaDigest = digest
			b.world.Schemas[digest] = types.SchemaInfo{
				Digest: digest,
				Schema: schema,
			}
		}

		xrd.Versions = append(xrd.Versions, ver)
	}

	b.world.XRDs = append(b.world.XRDs, xrd)
	return nil
}

func (b *Builder) addComposition(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	if spec == nil {
		return fmt.Errorf("Composition missing spec")
	}

	metadata := getMap(doc.Raw, "metadata")
	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	comp := types.CompositionInfo{
		Name:   name,
		Source: doc.Source,
	}

	// Parse compositeTypeRef
	ctr := getMap(spec, "compositeTypeRef")
	if ctr != nil {
		apiVersion, _ := ctr["apiVersion"].(string)
		kind, _ := ctr["kind"].(string)
		comp.CompositeTypeRef = parseGVK(apiVersion, kind)
	}

	// Determine mode
	if pipeline := getSlice(spec, "pipeline"); pipeline != nil {
		comp.Mode = "Pipeline"
		for _, step := range pipeline {
			sm, ok := step.(map[string]interface{})
			if !ok {
				continue
			}
			ps := types.PipelineStep{}
			ps.Name, _ = sm["step"].(string)

			if fr := getMap(sm, "functionRef"); fr != nil {
				ps.FunctionRef, _ = fr["name"].(string)
			}

			if input := getMap(sm, "input"); input != nil {
				ps.InputAPIVersion, _ = input["apiVersion"].(string)
				ps.InputKind, _ = input["kind"].(string)
				digest := hashSchema(input)
				ps.InputDigest = digest
				b.world.Schemas[digest] = types.SchemaInfo{
					Digest: digest,
					Schema: input,
				}
			}
			comp.Pipeline = append(comp.Pipeline, ps)
		}
	} else if resources := getSlice(spec, "resources"); resources != nil {
		comp.Mode = "Resources"
		for _, res := range resources {
			rm, ok := res.(map[string]interface{})
			if !ok {
				continue
			}
			cr := types.ComposedResource{}
			cr.Name, _ = rm["name"].(string)

			if base := getMap(rm, "base"); base != nil {
				av, _ := base["apiVersion"].(string)
				k, _ := base["kind"].(string)
				cr.Base = types.ResourceBase{APIVersion: av, Kind: k}
			}

			if patches := getSlice(rm, "patches"); patches != nil {
				for _, p := range patches {
					pm, ok := p.(map[string]interface{})
					if !ok {
						continue
					}
					pi := types.PatchInfo{}
					pi.Type, _ = pm["type"].(string)
					pi.FromFieldPath, _ = pm["fromFieldPath"].(string)
					pi.ToFieldPath, _ = pm["toFieldPath"].(string)

					if transforms := getSlice(pm, "transforms"); transforms != nil {
						for _, t := range transforms {
							tm, ok := t.(map[string]interface{})
							if !ok {
								continue
							}
							ti := types.TransformInfo{}
							ti.Type, _ = tm["type"].(string)
							if conv := getMap(tm, "convert"); conv != nil {
								ti.Convert, _ = conv["toType"].(string)
							}
							pi.Transforms = append(pi.Transforms, ti)
						}
					}
					cr.Patches = append(cr.Patches, pi)
				}
			}
			comp.Resources = append(comp.Resources, cr)
		}
	}

	b.world.Compositions = append(b.world.Compositions, comp)
	return nil
}

func (b *Builder) addFunction(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	metadata := getMap(doc.Raw, "metadata")

	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	fn := types.FunctionInfo{
		Name:   name,
		Source: doc.Source,
	}

	if spec != nil {
		pkg, _ := spec["package"].(string)
		fn.Package = pkg
	}

	// Infer input versions from well-known functions
	fn.InputVersions = inferFunctionInputVersions(name, fn.Package)

	b.world.Functions = append(b.world.Functions, fn)
	return nil
}

func (b *Builder) addProvider(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	metadata := getMap(doc.Raw, "metadata")

	name := ""
	annotations := map[string]string{}
	if metadata != nil {
		name, _ = metadata["name"].(string)
		if ann := getMap(metadata, "annotations"); ann != nil {
			for k, v := range ann {
				if vs, ok := v.(string); ok {
					annotations[k] = vs
				}
			}
		}
	}

	prov := types.ProviderInfo{
		Name:        name,
		Annotations: annotations,
		Source:      doc.Source,
	}

	if spec != nil {
		pkg, _ := spec["package"].(string)
		prov.Package = pkg
	}

	b.world.Providers = append(b.world.Providers, prov)
	return nil
}

func (b *Builder) addConfiguration(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	metadata := getMap(doc.Raw, "metadata")

	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	cfg := types.ConfigurationInfo{
		Name:   name,
		Source: doc.Source,
	}

	if spec != nil {
		pkg, _ := spec["package"].(string)
		cfg.Package = pkg
	}

	b.world.Configurations = append(b.world.Configurations, cfg)
	return nil
}

func (b *Builder) addArgoApplication(doc loader.LoadedDocument) error {
	metadata := getMap(doc.Raw, "metadata")
	name := ""
	ns := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
		ns, _ = metadata["namespace"].(string)
	}

	app := types.ArgoApplication{
		Name:         name,
		Namespace:    ns,
		TrackingMode: "annotation", // default
		Source:       doc.Source,
	}

	// Check for tracking mode annotation
	if metadata != nil {
		annotations := getMap(metadata, "annotations")
		if annotations != nil {
			if tm, ok := annotations["argocd.argoproj.io/tracking-method"].(string); ok {
				app.TrackingMode = tm
			}
		}
	}

	spec := getMap(doc.Raw, "spec")
	if spec != nil {
		app.Project, _ = spec["project"].(string)

		// Parse destination
		if dest := getMap(spec, "destination"); dest != nil {
			app.Destination.Server, _ = dest["server"].(string)
			app.Destination.Name, _ = dest["name"].(string)
			app.Destination.Namespace, _ = dest["namespace"].(string)
		}

		// Parse sources (multi-source or single-source)
		if sources := getSlice(spec, "sources"); sources != nil {
			for _, s := range sources {
				if sm, ok := s.(map[string]interface{}); ok {
					app.Sources = append(app.Sources, b.parseArgoSource(sm))
				}
			}
		} else if source := getMap(spec, "source"); source != nil {
			app.Sources = append(app.Sources, b.parseArgoSource(source))
		}

		// Parse syncPolicy
		if sp := getMap(spec, "syncPolicy"); sp != nil {
			app.SyncPolicy = b.parseArgoSyncPolicy(sp)
		}

		// Parse ignoreDifferences
		if diffs := getSlice(spec, "ignoreDifferences"); diffs != nil {
			for _, d := range diffs {
				if dm, ok := d.(map[string]interface{}); ok {
					app.IgnoreDifferences = append(app.IgnoreDifferences, parseArgoIgnoreDiff(dm))
				}
			}
		}
	}

	b.world.ArgoApps = append(b.world.ArgoApps, app)

	// Rendering hook: if rendering is enabled, walk every Helm and
	// Kustomize source on this Application, invoke the renderer, and
	// merge rendered resources into the World. A RenderResult is always
	// appended so R18/R19 can emit judgments about the outcome.
	if !b.SkipRender {
		b.renderHelmSources(app, doc.Source.File)
		b.renderKustomizeSources(app, doc.Source.File)
	}
	return nil
}

// renderHelmSources invokes the HelmRenderer for each Helm source on `app`
// and appends one RenderResult per source. Successful renders also
// contribute their rendered resources to World.Resources with a provenance
// tag keyed on the Application name.
func (b *Builder) renderHelmSources(app types.ArgoApplication, appFile string) {
	if len(app.Sources) == 0 {
		return
	}
	cwd := filepath.Dir(appFile)
	if b.helmRenderer == nil {
		b.helmRenderer = renderer.NewHelmRenderer(b.HelmBin, b.HelmCacheDir)
	}

	for _, src := range app.Sources {
		if src.Renderer != types.RendererHelm {
			continue
		}
		chartPath, resolveErr := renderer.ResolveChart(src, cwd)
		if errors.Is(resolveErr, renderer.ErrRemoteChart) {
			if b.HelmCacheDir == "" {
				b.world.RenderResults = append(b.world.RenderResults, types.RenderResult{
					AppName:   app.Name,
					ChartPath: "",
					Success:   false,
					Error:     "remote Helm chart requires --helm-cache-dir",
					ErrorKind: "helm-remote-unsupported",
					Source:    app.Source,
				})
				continue
			}
			pulled, pullErr := b.helmRenderer.PullRemote(src)
			if pullErr != nil {
				b.world.RenderResults = append(b.world.RenderResults, types.RenderResult{
					AppName:   app.Name,
					ChartPath: "",
					Success:   false,
					Error:     fmt.Sprintf("helm pull: %v", pullErr),
					ErrorKind: "helm-pull-failed",
					Source:    app.Source,
				})
				continue
			}
			chartPath = pulled
			resolveErr = nil
		} else if resolveErr != nil {
			b.world.RenderResults = append(b.world.RenderResults, types.RenderResult{
				AppName:   app.Name,
				ChartPath: chartPath,
				Success:   false,
				Error:     resolveErr.Error(),
				ErrorKind: "other",
				Source:    app.Source,
			})
			continue
		}

		// Values-schema validation comes FIRST so we report issues even
		// if the render itself happens to fail (and symmetrically so we
		// still flag schema violations on successful renders).
		var valuesIssues []types.ValuesIssue
		schemaPath := filepath.Join(chartPath, "values.schema.json")
		if schemaBytes, err := readFileQuietly(schemaPath); err == nil && len(schemaBytes) > 0 {
			merged, _ := renderer.MergedValues(chartPath, src.Helm)
			issues, verr := renderer.ValidateValues(schemaBytes, merged)
			if verr == nil {
				valuesIssues = issues
			}
		}

		rendered, renderErr := b.helmRenderer.RenderChart(chartPath, src.Helm, app.Destination.Namespace)
		result := types.RenderResult{
			AppName:      app.Name,
			ChartPath:    chartPath,
			Success:      renderErr == nil,
			ValuesIssues: valuesIssues,
			Source:       app.Source,
		}
		if renderErr != nil {
			result.Error = renderErr.Error()
			result.ErrorKind = classifyRenderError(renderErr)
			b.world.RenderResults = append(b.world.RenderResults, result)
			continue
		}

		docs, parseErr := loader.LoadReader(bytes.NewReader(rendered), appFile)
		if parseErr != nil {
			result.Success = false
			result.Error = fmt.Sprintf("parsing rendered YAML: %v", parseErr)
			result.ErrorKind = "other"
			b.world.RenderResults = append(b.world.RenderResults, result)
			continue
		}

		provenance := "rendered:helm:" + app.Name
		for _, d := range docs {
			// Tag every rendered resource's Source at the Application file
			// so diagnostics land somewhere an MR author can edit, and
			// stamp Provenance for downstream rule reasoning.
			res := resourceInfoFromDoc(d)
			res.Source = app.Source
			res.Provenance = provenance
			res.OwningApp = app.Name
			b.world.Resources = append(b.world.Resources, res)
		}
		b.world.RenderResults = append(b.world.RenderResults, result)
	}
}

// renderKustomizeSources invokes the KustomizeRenderer for each Kustomize
// source on `app` and appends one RenderResult per source. Successful
// renders also contribute their rendered resources to World.Resources with
// a provenance tag keyed on the Application name.
//
// Mirrors renderHelmSources intentionally — two renderers do not justify a
// shared helper, but the on-disk output and the World shape must match.
func (b *Builder) renderKustomizeSources(app types.ArgoApplication, appFile string) {
	if len(app.Sources) == 0 {
		return
	}
	cwd := filepath.Dir(appFile)
	if b.kustomizeRenderer == nil {
		b.kustomizeRenderer = renderer.NewKustomizeRenderer(b.KustomizeBin)
	}

	for _, src := range app.Sources {
		if src.Renderer != types.RendererKustomize {
			continue
		}
		overlayPath, resolveErr := renderer.ResolveChart(src, cwd)
		if resolveErr != nil {
			b.world.RenderResults = append(b.world.RenderResults, types.RenderResult{
				AppName:   app.Name,
				ChartPath: overlayPath,
				Success:   false,
				Error:     resolveErr.Error(),
				ErrorKind: "other",
				Source:    app.Source,
			})
			continue
		}

		rendered, renderErr := b.kustomizeRenderer.RenderOverlay(overlayPath, src.Kustomize)
		result := types.RenderResult{
			AppName:   app.Name,
			ChartPath: overlayPath,
			Success:   renderErr == nil,
			Source:    app.Source,
		}
		if renderErr != nil {
			result.Error = renderErr.Error()
			result.ErrorKind = classifyKustomizeError(renderErr)
			b.world.RenderResults = append(b.world.RenderResults, result)
			continue
		}

		docs, parseErr := loader.LoadReader(bytes.NewReader(rendered), appFile)
		if parseErr != nil {
			result.Success = false
			result.Error = fmt.Sprintf("parsing rendered YAML: %v", parseErr)
			result.ErrorKind = "other"
			b.world.RenderResults = append(b.world.RenderResults, result)
			continue
		}

		provenance := "rendered:kustomize:" + app.Name
		for _, d := range docs {
			res := resourceInfoFromDoc(d)
			res.Source = app.Source
			res.Provenance = provenance
			res.OwningApp = app.Name
			b.world.Resources = append(b.world.Resources, res)
		}
		b.world.RenderResults = append(b.world.RenderResults, result)
	}
}

// runDeterminismChecks double-renders every Application source and records
// the per-source DeterminismResult on the World. Sources whose first render
// errored are skipped silently — R18 / kustomize-renders already covered
// them; re-flagging here would be duplicative noise.
func (b *Builder) runDeterminismChecks() {
	for _, app := range b.world.ArgoApps {
		if len(app.Sources) == 0 {
			continue
		}
		cwd := filepath.Dir(app.Source.File)
		for _, src := range app.Sources {
			switch src.Renderer {
			case types.RendererHelm:
				if b.helmRenderer == nil {
					b.helmRenderer = renderer.NewHelmRenderer(b.HelmBin, b.HelmCacheDir)
				}
				chartPath, err := renderer.ResolveChart(src, cwd)
				if err != nil {
					continue
				}
				mismatch, summary, err := renderer.DoubleRenderHelm(b.helmRenderer, chartPath, app.Destination.Namespace, src.Helm)
				if err != nil {
					continue
				}
				b.world.DeterminismResults = append(b.world.DeterminismResults, types.DeterminismResult{
					AppName:      app.Name,
					RendererKind: "helm",
					Mismatch:     mismatch,
					DiffSummary:  summary,
					Source:       app.Source,
				})
			case types.RendererKustomize:
				if b.kustomizeRenderer == nil {
					b.kustomizeRenderer = renderer.NewKustomizeRenderer(b.KustomizeBin)
				}
				overlayPath, err := renderer.ResolveChart(src, cwd)
				if err != nil {
					continue
				}
				mismatch, summary, err := renderer.DoubleRenderKustomize(b.kustomizeRenderer, overlayPath, src.Kustomize)
				if err != nil {
					continue
				}
				b.world.DeterminismResults = append(b.world.DeterminismResults, types.DeterminismResult{
					AppName:      app.Name,
					RendererKind: "kustomize",
					Mismatch:     mismatch,
					DiffSummary:  summary,
					Source:       app.Source,
				})
			}
		}
	}
}

// classifyKustomizeError mirrors classifyRenderError for the Kustomize
// renderer. Uppercase identifiers in Shen patterns are variables, so every
// kind stays lowercase-dashed.
func classifyKustomizeError(err error) string {
	if err == nil {
		return ""
	}
	if renderer.IsKustomizeAbsent(err) {
		return "kustomize-absent"
	}
	msg := err.Error()
	if strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "timed out") {
		return "kustomize-timeout"
	}
	if strings.Contains(msg, "kustomize build") {
		return "kustomize-build-failed"
	}
	return "other"
}

// resourceInfoFromDoc projects a LoadedDocument into a ResourceInfo. Mirrors
// the shape of addResource so rendered manifests look identical to direct
// ones downstream.
func resourceInfoFromDoc(doc loader.LoadedDocument) types.ResourceInfo {
	name := getName(doc.Raw)
	ns := ""
	if meta := getMap(doc.Raw, "metadata"); meta != nil {
		ns, _ = meta["namespace"].(string)
	}
	annotations := map[string]string{}
	labels := map[string]string{}
	if meta := getMap(doc.Raw, "metadata"); meta != nil {
		if am := getMap(meta, "annotations"); am != nil {
			for k, v := range am {
				if vs, ok := v.(string); ok {
					annotations[k] = vs
				}
			}
		}
		if lm := getMap(meta, "labels"); lm != nil {
			for k, v := range lm {
				if vs, ok := v.(string); ok {
					labels[k] = vs
				}
			}
		}
	}
	return types.ResourceInfo{
		APIVersion:  doc.APIVersion,
		Kind:        doc.Kind,
		Name:        name,
		Namespace:   ns,
		Annotations: annotations,
		Labels:      labels,
		Source:      doc.Source,
		Raw:         doc.Raw,
	}
}

// classifyRenderError maps a renderer error to the kebab-case kind tag used
// by Shen rule patterns. Uppercase identifiers are Shen variables, so every
// tag must stay lowercase.
func classifyRenderError(err error) string {
	if err == nil {
		return ""
	}
	if renderer.IsHelmAbsent(err) {
		return "helm-absent"
	}
	msg := err.Error()
	if strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "timed out") {
		return "helm-timeout"
	}
	if strings.Contains(msg, "helm template") {
		return "helm-template-failed"
	}
	return "other"
}

// readFileQuietly is a thin wrapper that hides the "does not exist" error,
// so callers can probe for optional files (values.schema.json) without
// pattern-matching fs errors.
func readFileQuietly(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

func (b *Builder) parseArgoSource(m map[string]interface{}) types.ArgoSource {
	src := types.ArgoSource{}
	src.RepoURL, _ = m["repoURL"].(string)
	src.Path, _ = m["path"].(string)
	src.TargetRevision, _ = m["targetRevision"].(string)
	src.Chart, _ = m["chart"].(string)

	if helm := getMap(m, "helm"); helm != nil {
		src.Renderer = types.RendererHelm
		h := &types.ArgoHelmSource{}
		if vf := getSlice(helm, "valueFiles"); vf != nil {
			for _, v := range vf {
				if vs, ok := v.(string); ok {
					h.ValueFiles = append(h.ValueFiles, vs)
				}
			}
		}
		if vo := getMap(helm, "valuesObject"); vo != nil {
			h.ValuesObject = vo
		}
		h.Values, _ = helm["values"].(string)
		h.ReleaseName, _ = helm["releaseName"].(string)
		if params := getSlice(helm, "parameters"); params != nil {
			for _, p := range params {
				if pm, ok := p.(map[string]interface{}); ok {
					name, _ := pm["name"].(string)
					value, _ := pm["value"].(string)
					h.Parameters = append(h.Parameters, types.ArgoHelmParam{Name: name, Value: value})
				}
			}
		}
		src.Helm = h
	} else if kust := getMap(m, "kustomize"); kust != nil {
		src.Renderer = types.RendererKustomize
		k := &types.ArgoKustomizeSource{}
		k.NamePrefix, _ = kust["namePrefix"].(string)
		k.NameSuffix, _ = kust["nameSuffix"].(string)
		if imgs := getSlice(kust, "images"); imgs != nil {
			for _, img := range imgs {
				if is, ok := img.(string); ok {
					k.Images = append(k.Images, is)
				}
			}
		}
		if cl := getMap(kust, "commonLabels"); cl != nil {
			k.CommonLabels = make(map[string]string)
			for key, val := range cl {
				if vs, ok := val.(string); ok {
					k.CommonLabels[key] = vs
				}
			}
		}
		if ca := getMap(kust, "commonAnnotations"); ca != nil {
			k.CommonAnnotations = make(map[string]string)
			for key, val := range ca {
				if vs, ok := val.(string); ok {
					k.CommonAnnotations[key] = vs
				}
			}
		}
		src.Kustomize = k
	} else if plugin := getMap(m, "plugin"); plugin != nil {
		src.Renderer = types.RendererPlugin
		p := &types.ArgoPluginSource{}
		p.Name, _ = plugin["name"].(string)
		if envs := getSlice(plugin, "env"); envs != nil {
			for _, e := range envs {
				if em, ok := e.(map[string]interface{}); ok {
					n, _ := em["name"].(string)
					v, _ := em["value"].(string)
					p.Env = append(p.Env, types.ArgoPluginEnv{Name: n, Value: v})
				}
			}
		}
		src.Plugin = p
	} else if dir := getMap(m, "directory"); dir != nil {
		src.Renderer = types.RendererDirectory
		d := &types.ArgoDirectorySource{}
		d.Recurse, _ = dir["recurse"].(bool)
		d.Exclude, _ = dir["exclude"].(string)
		d.Include, _ = dir["include"].(string)
		src.Directory = d
	} else {
		src.Renderer = types.RendererDirectory // default
	}

	return src
}

func (b *Builder) parseArgoSyncPolicy(m map[string]interface{}) types.ArgoSyncPolicy {
	sp := types.ArgoSyncPolicy{}

	if auto := getMap(m, "automated"); auto != nil {
		a := &types.ArgoAutomatedSync{}
		a.Prune, _ = auto["prune"].(bool)
		a.SelfHeal, _ = auto["selfHeal"].(bool)
		sp.Automated = a
	}

	// Parse syncOptions — Argo stores these as a list of "Key=Value" strings
	if opts := getSlice(m, "syncOptions"); opts != nil {
		for _, o := range opts {
			if s, ok := o.(string); ok {
				switch s {
				case "Replace=true":
					sp.SyncOptions.Replace = true
				case "ServerSideApply=true":
					sp.SyncOptions.ServerSideApply = true
				case "Prune=true":
					sp.SyncOptions.Prune = true
				case "PruneLast=true":
					sp.SyncOptions.PruneLast = true
				case "CreateNamespace=true":
					sp.SyncOptions.CreateNamespace = true
				case "ApplyOutOfSyncOnly=true":
					sp.SyncOptions.ApplyOutOfSyncOnly = true
				case "Validate=true":
					sp.SyncOptions.Validate = true
				case "FailOnSharedResource=true":
					sp.SyncOptions.FailOnSharedResource = true
				case "RespectIgnoreDifferences=true":
					sp.SyncOptions.RespectIgnoreDifferences = true
				}
			}
		}
	}

	if retry := getMap(m, "retry"); retry != nil {
		r := &types.ArgoRetryPolicy{}
		if limit, ok := retry["limit"].(float64); ok {
			r.Limit = int(limit)
		}
		sp.Retry = r
	}

	return sp
}

func parseArgoIgnoreDiff(m map[string]interface{}) types.ArgoIgnoreDiff {
	d := types.ArgoIgnoreDiff{}
	d.Group, _ = m["group"].(string)
	d.Kind, _ = m["kind"].(string)
	d.Name, _ = m["name"].(string)
	d.Namespace, _ = m["namespace"].(string)

	if jp := getSlice(m, "jsonPointers"); jp != nil {
		for _, p := range jp {
			if ps, ok := p.(string); ok {
				d.JSONPointers = append(d.JSONPointers, ps)
			}
		}
	}
	if jq := getSlice(m, "jqPathExpressions"); jq != nil {
		for _, e := range jq {
			if es, ok := e.(string); ok {
				d.JQPathExpressions = append(d.JQPathExpressions, es)
			}
		}
	}
	if mf := getSlice(m, "managedFieldsManagers"); mf != nil {
		for _, f := range mf {
			if fs, ok := f.(string); ok {
				d.ManagedFieldsManagers = append(d.ManagedFieldsManagers, fs)
			}
		}
	}
	return d
}

func (b *Builder) addArgoAppProject(doc loader.LoadedDocument) error {
	metadata := getMap(doc.Raw, "metadata")
	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	proj := types.ArgoAppProject{
		Name:   name,
		Source: doc.Source,
	}

	spec := getMap(doc.Raw, "spec")
	if spec != nil {
		// sourceRepos
		if repos := getSlice(spec, "sourceRepos"); repos != nil {
			for _, r := range repos {
				if rs, ok := r.(string); ok {
					proj.SourceRepos = append(proj.SourceRepos, rs)
				}
			}
		}

		// destinations
		if dests := getSlice(spec, "destinations"); dests != nil {
			for _, d := range dests {
				if dm, ok := d.(map[string]interface{}); ok {
					pd := types.ArgoProjectDestination{}
					pd.Server, _ = dm["server"].(string)
					pd.Name, _ = dm["name"].(string)
					pd.Namespace, _ = dm["namespace"].(string)
					proj.Destinations = append(proj.Destinations, pd)
				}
			}
		}

		// resource whitelists/blacklists
		proj.ClusterResourceWhitelist = parseGroupKindList(spec, "clusterResourceWhitelist")
		proj.ClusterResourceBlacklist = parseGroupKindList(spec, "clusterResourceBlacklist")
		proj.NamespaceResourceWhitelist = parseGroupKindList(spec, "namespaceResourceWhitelist")
		proj.NamespaceResourceBlacklist = parseGroupKindList(spec, "namespaceResourceBlacklist")

		// syncWindows
		if wins := getSlice(spec, "syncWindows"); wins != nil {
			for _, w := range wins {
				if wm, ok := w.(map[string]interface{}); ok {
					sw := types.ArgoSyncWindow{}
					sw.Kind, _ = wm["kind"].(string)
					sw.Schedule, _ = wm["schedule"].(string)
					sw.Duration, _ = wm["duration"].(string)
					sw.Applications = getStringSlice(wm, "applications")
					sw.Namespaces = getStringSlice(wm, "namespaces")
					sw.Clusters = getStringSlice(wm, "clusters")
					proj.SyncWindows = append(proj.SyncWindows, sw)
				}
			}
		}

		// signatureKeys
		if keys := getSlice(spec, "signatureKeys"); keys != nil {
			for _, k := range keys {
				if km, ok := k.(map[string]interface{}); ok {
					if keyID, ok := km["keyID"].(string); ok {
						proj.SignatureKeys = append(proj.SignatureKeys, keyID)
					}
				}
			}
		}
	}

	b.world.ArgoProjects = append(b.world.ArgoProjects, proj)
	return nil
}

func (b *Builder) addArgoApplicationSet(doc loader.LoadedDocument) error {
	metadata := getMap(doc.Raw, "metadata")
	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	appSet := types.ArgoApplicationSet{
		Name:   name,
		Source: doc.Source,
	}

	spec := getMap(doc.Raw, "spec")
	if spec != nil {
		// Parse generators
		if gens := getSlice(spec, "generators"); gens != nil {
			for _, g := range gens {
				if gm, ok := g.(map[string]interface{}); ok {
					appSet.Generators = append(appSet.Generators, b.parseAppSetGenerator(gm))
				}
			}
		}

		// Parse template
		if tmpl := getMap(spec, "template"); tmpl != nil {
			appSet.Template = b.parseAppSetTemplate(tmpl)
		}
	}

	b.world.ArgoAppSets = append(b.world.ArgoAppSets, appSet)
	return nil
}

func (b *Builder) parseAppSetGenerator(m map[string]interface{}) types.ArgoAppSetGenerator {
	gen := types.ArgoAppSetGenerator{}

	if list := getMap(m, "list"); list != nil {
		gen.Kind = types.AppSetGenList
		if elems := getSlice(list, "elements"); elems != nil {
			for _, e := range elems {
				if em, ok := e.(map[string]interface{}); ok {
					elem := make(map[string]string)
					for k, v := range em {
						if vs, ok := v.(string); ok {
							elem[k] = vs
						}
					}
					gen.ListElements = append(gen.ListElements, elem)
				}
			}
		}
	} else if clusters := getMap(m, "clusters"); clusters != nil {
		gen.Kind = types.AppSetGenCluster
		if sel := getMap(clusters, "selector"); sel != nil {
			if matchLabels := getMap(sel, "matchLabels"); matchLabels != nil {
				gen.ClusterSelector = make(map[string]string)
				for k, v := range matchLabels {
					if vs, ok := v.(string); ok {
						gen.ClusterSelector[k] = vs
					}
				}
			}
		}
	} else if git := getMap(m, "git"); git != nil {
		gen.Kind = types.AppSetGenGit
		g := &types.ArgoAppSetGitGenerator{}
		g.RepoURL, _ = git["repoURL"].(string)
		g.Revision, _ = git["revision"].(string)
		if dirs := getSlice(git, "directories"); dirs != nil {
			for _, d := range dirs {
				if dm, ok := d.(map[string]interface{}); ok {
					gd := types.ArgoAppSetGitDir{}
					gd.Path, _ = dm["path"].(string)
					gd.Exclude, _ = dm["exclude"].(bool)
					g.Directories = append(g.Directories, gd)
				}
			}
		}
		if files := getSlice(git, "files"); files != nil {
			for _, f := range files {
				if fm, ok := f.(map[string]interface{}); ok {
					gf := types.ArgoAppSetGitFile{}
					gf.Path, _ = fm["path"].(string)
					g.Files = append(g.Files, gf)
				}
			}
		}
		gen.Git = g
	} else if matrix := getMap(m, "matrix"); matrix != nil {
		gen.Kind = types.AppSetGenMatrix
		if subs := getSlice(matrix, "generators"); subs != nil {
			for _, s := range subs {
				if sm, ok := s.(map[string]interface{}); ok {
					gen.MatrixGenerators = append(gen.MatrixGenerators, b.parseAppSetGenerator(sm))
				}
			}
		}
	} else if merge := getMap(m, "merge"); merge != nil {
		gen.Kind = types.AppSetGenMerge
		gen.MergeKeys = getStringSlice(merge, "mergeKeys")
		if subs := getSlice(merge, "generators"); subs != nil {
			for _, s := range subs {
				if sm, ok := s.(map[string]interface{}); ok {
					gen.MergeGenerators = append(gen.MergeGenerators, b.parseAppSetGenerator(sm))
				}
			}
		}
	} else if _, ok := m["pullRequest"]; ok {
		gen.Kind = types.AppSetGenPullRequest
	} else if _, ok := m["scmProvider"]; ok {
		gen.Kind = types.AppSetGenSCMProvider
	}

	return gen
}

func (b *Builder) parseAppSetTemplate(m map[string]interface{}) types.ArgoAppSetTemplate {
	tmpl := types.ArgoAppSetTemplate{}

	if meta := getMap(m, "metadata"); meta != nil {
		tmpl.Name, _ = meta["name"].(string)
		tmpl.Namespace, _ = meta["namespace"].(string)
	}

	if spec := getMap(m, "spec"); spec != nil {
		tmpl.Project, _ = spec["project"].(string)

		if source := getMap(spec, "source"); source != nil {
			s := b.parseArgoSource(source)
			tmpl.Source = &s
		}
		if sources := getSlice(spec, "sources"); sources != nil {
			for _, src := range sources {
				if sm, ok := src.(map[string]interface{}); ok {
					tmpl.Sources = append(tmpl.Sources, b.parseArgoSource(sm))
				}
			}
		}
		if dest := getMap(spec, "destination"); dest != nil {
			tmpl.Destination.Server, _ = dest["server"].(string)
			tmpl.Destination.Name, _ = dest["name"].(string)
			tmpl.Destination.Namespace, _ = dest["namespace"].(string)
		}
		if sp := getMap(spec, "syncPolicy"); sp != nil {
			tmpl.SyncPolicy = b.parseArgoSyncPolicy(sp)
		}
	}

	return tmpl
}

func (b *Builder) addResource(doc loader.LoadedDocument) error {
	metadata := getMap(doc.Raw, "metadata")
	name := ""
	ns := ""
	annotations := map[string]string{}
	labels := map[string]string{}

	if metadata != nil {
		name, _ = metadata["name"].(string)
		ns, _ = metadata["namespace"].(string)

		if ann := getMap(metadata, "annotations"); ann != nil {
			for k, v := range ann {
				if vs, ok := v.(string); ok {
					annotations[k] = vs
				}
			}
		}
		if lbl := getMap(metadata, "labels"); lbl != nil {
			for k, v := range lbl {
				if vs, ok := v.(string); ok {
					labels[k] = vs
				}
			}
		}
	}

	res := types.ResourceInfo{
		APIVersion:  doc.APIVersion,
		Kind:        doc.Kind,
		Name:        name,
		Namespace:   ns,
		Annotations: annotations,
		Labels:      labels,
		Source:      doc.Source,
		Raw:         doc.Raw,
	}

	b.world.Resources = append(b.world.Resources, res)
	return nil
}

// Helper functions

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	v, ok := m[key]
	if !ok {
		return nil
	}
	vm, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return vm
}

func getStringSlice(m map[string]interface{}, key string) []string {
	raw := getSlice(m, key)
	if raw == nil {
		return nil
	}
	var result []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func parseGroupKindList(spec map[string]interface{}, key string) []types.ArgoGroupKind {
	items := getSlice(spec, key)
	if items == nil {
		return nil
	}
	var result []types.ArgoGroupKind
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			gk := types.ArgoGroupKind{}
			gk.Group, _ = m["group"].(string)
			gk.Kind, _ = m["kind"].(string)
			result = append(result, gk)
		}
	}
	return result
}

func getSlice(m map[string]interface{}, key string) []interface{} {
	v, ok := m[key]
	if !ok {
		return nil
	}
	vs, ok := v.([]interface{})
	if !ok {
		return nil
	}
	return vs
}

func getName(raw map[string]interface{}) string {
	metadata := getMap(raw, "metadata")
	if metadata == nil {
		return "<unknown>"
	}
	name, _ := metadata["name"].(string)
	if name == "" {
		return "<unknown>"
	}
	return name
}

func hashSchema(schema map[string]interface{}) string {
	data, _ := json.Marshal(schema)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:16]) // truncated for readability
}

func parseGVK(apiVersion, kind string) types.GVK {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		return types.GVK{Group: parts[0], Version: parts[1], Kind: kind}
	}
	return types.GVK{Group: "", Version: apiVersion, Kind: kind}
}

// inferFunctionInputVersions returns known input versions for well-known functions.
func inferFunctionInputVersions(name, pkg string) []string {
	// Map of well-known functions to their accepted input versions
	knownFunctions := map[string][]string{
		"function-patch-and-transform": {"pt.fn.crossplane.io/v1beta1"},
		"function-kcl":                 {"krm.kcl.dev/v1alpha1"},
		"function-go-templating":       {"gotemplating.fn.crossplane.io/v1beta1"},
		"function-auto-ready":          {}, // no input
		"function-cel-filter":          {"celfilter.fn.crossplane.io/v1beta1"},
		"function-status-transformer":  {"statustransformer.fn.crossplane.io/v1beta1"},
		"function-environment-configs": {"environmentconfigs.fn.crossplane.io/v1beta1"},
		"function-extra-resources":     {"extraresources.fn.crossplane.io/v1beta1"},
		"function-sequencer":           {"sequencer.fn.crossplane.io/v1beta1"},
	}

	for knownName, versions := range knownFunctions {
		if name == knownName || strings.Contains(pkg, knownName) {
			return versions
		}
	}
	return nil // unknown function
}

// ParseSyncWave extracts the sync wave integer from a resource's annotations.
func ParseSyncWave(annotations map[string]string) int {
	wave, ok := annotations["argocd.argoproj.io/sync-wave"]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(wave)
	if err != nil {
		return 0
	}
	return n
}
