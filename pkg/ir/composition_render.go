package ir

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/renderer"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// renderCompositions invokes `crossplane render` for every (XR, Composition)
// pair xpc can identify in the loaded doc set. Rendered managed resources
// flow into World.Resources with `Provenance = "rendered:composition:<xr>"`,
// matching the existing `"rendered:helm:<app>"` convention from R18.
//
// XR discovery: for each CompositionInfo whose CompositeTypeRef is
// non-empty, find every loaded doc whose (APIVersion, Kind) equal the
// compositeTypeRef's (Group/Version, Kind). Each match is rendered as a
// separate XR.
//
// Function binding: pipeline Functions referenced by the Composition are
// looked up in the loaded doc set and re-serialized into a single YAML
// stream passed as the third argument to `crossplane render`.
//
// Outcomes flow into World.RenderResults (overloaded — same section R18
// consumes). New ErrorKind values `crossplane-absent`, `crossplane-timeout`,
// and `crossplane-render-failed` distinguish composition failures from Helm
// / Kustomize failures; the kernel routes them to `XPC.H.composition-renders`.
//
// crossplane-absent is treated the same way as helm-absent: one result per
// attempted (XR, Composition) pair, ErrorKind=crossplane-absent. The Shen
// rule surfaces this as warning severity so CI runs without the binary
// still complete, just with reduced coverage.
func (b *Builder) renderCompositions(docs []loader.LoadedDocument) {
	if len(b.world.Compositions) == 0 {
		return
	}
	if b.compositionRenderer == nil {
		b.compositionRenderer = renderer.NewCompositionRenderer(b.CrossplaneBin)
	}

	xrIndex := indexXRDocs(docs)
	fnIndex := indexFunctionDocs(docs)
	allFunctionsYAML := concatFunctionBindings(fnIndex)

	for _, comp := range b.world.Compositions {
		ref := comp.CompositeTypeRef
		if ref.Kind == "" {
			continue
		}
		apiVersion := buildAPIVersion(ref.Group, ref.Version)
		xrKey := apiVersion + "/" + ref.Kind
		xrs, ok := xrIndex[xrKey]
		if !ok || len(xrs) == 0 {
			// No XR available — we emit a single info-level diagnostic
			// per composition elsewhere (R18's render-results path), but
			// here there is nothing to render. This is the common case in
			// fg-manifold: Compositions live in the repo; XRs are typed
			// runtime objects synthesized by Crossplane on claim reconcile.
			continue
		}

		compYAML, err := marshalDoc(comp.Source.File, comp.CompositeTypeRef, docs)
		if err != nil {
			b.world.RenderResults = append(b.world.RenderResults, types.RenderResult{
				AppName:   compositionAppName(comp.Name, ""),
				ChartPath: comp.Name,
				Success:   false,
				Error:     fmt.Sprintf("marshal composition: %v", err),
				ErrorKind: "crossplane-render-failed",
				Source:    comp.Source,
			})
			continue
		}

		for _, xr := range xrs {
			xrYAML, err := yaml.Marshal(xr.Raw)
			if err != nil {
				b.world.RenderResults = append(b.world.RenderResults, types.RenderResult{
					AppName:   compositionAppName(comp.Name, nameFromDoc(xr)),
					ChartPath: comp.Name,
					Success:   false,
					Error:     fmt.Sprintf("marshal XR: %v", err),
					ErrorKind: "crossplane-render-failed",
					Source:    comp.Source,
				})
				continue
			}

			rendered, renderErr := b.compositionRenderer.Render(xrYAML, compYAML, allFunctionsYAML)
			xrName := nameFromDoc(xr)
			result := types.RenderResult{
				AppName:   compositionAppName(comp.Name, xrName),
				ChartPath: comp.Name,
				Success:   renderErr == nil,
				Source:    comp.Source,
			}
			if renderErr != nil {
				result.Error = renderErr.Error()
				result.ErrorKind = renderer.ClassifyCompositionError(renderErr)
				b.world.RenderResults = append(b.world.RenderResults, result)
				continue
			}

			parsedDocs, parseErr := loader.LoadReader(bytes.NewReader(rendered), comp.Source.File)
			if parseErr != nil {
				result.Success = false
				result.Error = fmt.Sprintf("parsing rendered YAML: %v", parseErr)
				result.ErrorKind = "crossplane-render-failed"
				b.world.RenderResults = append(b.world.RenderResults, result)
				continue
			}

			provenance := "rendered:composition:" + xrName
			for _, d := range parsedDocs {
				res := resourceInfoFromDoc(d)
				// Tag every rendered resource's Source at the Composition
				// file so diagnostics land somewhere an author can edit.
				res.Source = comp.Source
				res.Provenance = provenance
				b.world.Resources = append(b.world.Resources, res)
			}
			b.world.RenderResults = append(b.world.RenderResults, result)
		}
	}
}

// indexXRDocs returns a map keyed by "<apiVersion>/<kind>" into every loaded
// doc with that GVK. XRs and claims both match — the caller (a Composition
// whose CompositeTypeRef names the kind) is the authoritative narrower.
func indexXRDocs(docs []loader.LoadedDocument) map[string][]loader.LoadedDocument {
	out := map[string][]loader.LoadedDocument{}
	for _, d := range docs {
		if d.APIVersion == "" || d.Kind == "" {
			continue
		}
		// Exclude xpc's own primary meta-objects — they are never XRs.
		if loader.ClassifyDocument(d) != "resource" {
			continue
		}
		key := d.APIVersion + "/" + d.Kind
		out[key] = append(out[key], d)
	}
	return out
}

// indexFunctionDocs collects every Function resource in the doc set.
// `crossplane render` accepts a single concatenated YAML stream; we emit
// them in the order they were loaded for determinism.
func indexFunctionDocs(docs []loader.LoadedDocument) []loader.LoadedDocument {
	var out []loader.LoadedDocument
	for _, d := range docs {
		if loader.ClassifyDocument(d) == "function" {
			out = append(out, d)
		}
	}
	return out
}

// concatFunctionBindings marshals every function doc back to YAML and joins
// them with --- separators. Returns nil when no functions are loaded; an
// empty argument to `crossplane render` would still render, just without
// any function calls resolving.
func concatFunctionBindings(fns []loader.LoadedDocument) []byte {
	if len(fns) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for i, d := range fns {
		if i > 0 {
			buf.WriteString("---\n")
		}
		data, err := yaml.Marshal(d.Raw)
		if err != nil {
			continue
		}
		buf.Write(data)
	}
	return buf.Bytes()
}

// marshalDoc finds the LoadedDocument whose source file matches a given
// CompositionInfo's Source.File and returns its Raw serialized back to YAML.
// The CompositeTypeRef argument is unused today; kept in the signature so
// we can switch to a more robust lookup (by compositeTypeRef match across
// all Composition docs) if two compositions share a single source file.
func marshalDoc(sourceFile string, _ types.GVK, docs []loader.LoadedDocument) ([]byte, error) {
	for _, d := range docs {
		if d.Source.File == sourceFile && d.Kind == "Composition" {
			return yaml.Marshal(d.Raw)
		}
	}
	return nil, errors.New("composition source doc not found")
}

// compositionAppName builds the human-facing label used in RenderResult.
// Shape is "<composition>:<xrName>" when xrName is non-empty; the shen
// kernel shows this in the diagnostic title.
func compositionAppName(comp, xr string) string {
	if xr == "" {
		return comp
	}
	return comp + ":" + xr
}

// nameFromDoc extracts metadata.name from a loaded doc. Empty when absent.
func nameFromDoc(d loader.LoadedDocument) string {
	meta := getMap(d.Raw, "metadata")
	if meta == nil {
		return ""
	}
	if s, ok := meta["name"].(string); ok {
		return s
	}
	return ""
}

// buildAPIVersion joins a group + version back into the "group/version" form
// (or just "version" when group is empty — core k8s kinds).
func buildAPIVersion(group, version string) string {
	if group == "" {
		return version
	}
	return group + "/" + version
}
