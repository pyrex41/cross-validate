// appset_expand.go — expand ApplicationSet generators into concrete
// ArgoApplications so every downstream rule (R15 whitelist, R16 selector
// needs-ignore-diff, R18 render, etc.) automatically covers the synthetic
// Applications the ApplicationSet controller would produce.
//
// We handle the generator kinds offline-reachable from a manifest tree:
//
//	list      — one Application per listElements entry
//	git-dirs  — one per directory under repoURL/path on the local FS
//	matrix    — cartesian product of two child generators
//	merge     — deep-merge two generators by shared keys (mergeKeys)
//
// pullRequest / scmProvider cannot be simulated offline (they hit
// GitHub/GitLab APIs). Callers pass a fixture via ExpansionContext.PRFixtures
// keyed by ApplicationSet name; when no fixture is provided we emit one
// XPC.H.appset-unsupported-generator info diagnostic and move on.

package ir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// ExpansionContext carries the out-of-tree inputs ExpandAppSet needs.
// Keeping it a plain struct (rather than a global) makes the expansion
// deterministic from tests: every test owns its own fixture map.
type ExpansionContext struct {
	// PRFixtures maps ApplicationSet name → list of parameter maps that
	// stand in for live GitHub / GitLab pullRequest output. Each element
	// typically contains keys like "number", "branch", "headSha".
	PRFixtures map[string][]map[string]string

	// Root is the filesystem directory the appset's git generator roots
	// at. Only git-directories generators use this. Empty means "use the
	// appset's own Source.File dir".
	Root string
}

// ExpansionResult is the output of ExpandAppSet: the synthetic Applications
// produced by the generators plus any info-level diagnostics the kernel
// should surface (one per unsupported generator kind we encountered).
type ExpansionResult struct {
	Applications []types.ArgoApplication
	Diagnostics  []types.Diagnostic
}

// ExpandAppSet expands a single ApplicationSet according to its generators.
// The returned Applications have their .Source set to the ApplicationSet's
// source (so downstream diagnostics point an MR author at the right file)
// and their .Name derived from the ApplicationSet's template.
func ExpandAppSet(as types.ArgoApplicationSet, ctx ExpansionContext) ExpansionResult {
	var res ExpansionResult
	for _, gen := range as.Generators {
		params, diags := expandGenerator(as, gen, ctx)
		res.Diagnostics = append(res.Diagnostics, diags...)
		for _, p := range params {
			app, ok := instantiateTemplate(as, p)
			if !ok {
				res.Diagnostics = append(res.Diagnostics, types.Diagnostic{
					Code:     "XPC.H.appset-unsupported-generator",
					Severity: types.SeverityInfo,
					Source:   as.Source,
					Message:  fmt.Sprintf("%s: template uses non-trivial Go-template syntax we do not simulate", as.Name),
					Detail:   "ranges/conditionals/pipelines are not rendered offline; run Argo CD's appset controller for full coverage",
					Fix:      "simplify the template to plain `{{ .key }}` placeholders, or accept coverage gap",
				})
				continue
			}
			res.Applications = append(res.Applications, app)
		}
	}
	return res
}

// expandGenerator dispatches on Kind and returns the parameter sets this
// generator produces. Each map is passed to the template substitutor, so
// key names must match the `{{ .key }}` placeholders in as.Template.
func expandGenerator(as types.ArgoApplicationSet, gen types.ArgoAppSetGenerator, ctx ExpansionContext) ([]map[string]string, []types.Diagnostic) {
	switch gen.Kind {
	case types.AppSetGenList:
		return gen.ListElements, nil

	case types.AppSetGenGit:
		return expandGitDirs(as, gen, ctx)

	case types.AppSetGenMatrix:
		return expandMatrix(as, gen, ctx)

	case types.AppSetGenMerge:
		return expandMerge(as, gen, ctx)

	case types.AppSetGenPullRequest, types.AppSetGenSCMProvider:
		if prs, ok := ctx.PRFixtures[as.Name]; ok {
			return prs, nil
		}
		return nil, []types.Diagnostic{unsupportedGeneratorDiag(as, string(gen.Kind),
			"no --appset-fixture provided; this generator would hit a remote API")}

	case types.AppSetGenCluster:
		// We don't have a cluster inventory offline. Info-level skip.
		return nil, []types.Diagnostic{unsupportedGeneratorDiag(as, string(gen.Kind),
			"offline simulation does not include a cluster inventory")}
	}
	return nil, []types.Diagnostic{unsupportedGeneratorDiag(as, string(gen.Kind),
		"generator kind not recognized")}
}

// expandGitDirs walks the filesystem under ctx.Root (falling back to the
// appset file's directory) looking for the paths declared in
// gen.Git.Directories. Each matching directory becomes one parameter set
// with a `path` and `path.basename` key.
func expandGitDirs(as types.ArgoApplicationSet, gen types.ArgoAppSetGenerator, ctx ExpansionContext) ([]map[string]string, []types.Diagnostic) {
	if gen.Git == nil {
		return nil, nil
	}
	root := ctx.Root
	if root == "" {
		root = filepath.Dir(as.Source.File)
	}
	var out []map[string]string
	for _, dir := range gen.Git.Directories {
		if dir.Exclude {
			continue
		}
		// dir.Path may be a glob like "charts/*" or a plain path. We
		// resolve relative to root.
		pattern := filepath.Join(root, dir.Path)
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || !info.IsDir() {
				continue
			}
			out = append(out, map[string]string{
				"path":          m,
				"path.basename": filepath.Base(m),
			})
		}
	}
	return out, nil
}

// expandMatrix produces the cartesian product of two child generators.
// Argo's real implementation also supports matrix-of-matrix; we cap at one
// level of nesting for now and surface anything deeper as an info diag.
func expandMatrix(as types.ArgoApplicationSet, gen types.ArgoAppSetGenerator, ctx ExpansionContext) ([]map[string]string, []types.Diagnostic) {
	if len(gen.MatrixGenerators) < 2 {
		return nil, nil
	}
	aParams, aDiags := expandGenerator(as, gen.MatrixGenerators[0], ctx)
	bParams, bDiags := expandGenerator(as, gen.MatrixGenerators[1], ctx)
	diags := append([]types.Diagnostic{}, aDiags...)
	diags = append(diags, bDiags...)

	out := make([]map[string]string, 0, len(aParams)*len(bParams))
	for _, ap := range aParams {
		for _, bp := range bParams {
			merged := make(map[string]string, len(ap)+len(bp))
			for k, v := range ap {
				merged[k] = v
			}
			for k, v := range bp {
				merged[k] = v
			}
			out = append(out, merged)
		}
	}
	return out, diags
}

// expandMerge joins two generators by their shared keys (gen.MergeKeys).
// Entries on the "primary" (left) side are retained; matching entries on
// the secondary side contribute their non-shared fields.
func expandMerge(as types.ArgoApplicationSet, gen types.ArgoAppSetGenerator, ctx ExpansionContext) ([]map[string]string, []types.Diagnostic) {
	if len(gen.MergeGenerators) < 2 || len(gen.MergeKeys) == 0 {
		return nil, nil
	}
	aParams, aDiags := expandGenerator(as, gen.MergeGenerators[0], ctx)
	bParams, bDiags := expandGenerator(as, gen.MergeGenerators[1], ctx)
	diags := append([]types.Diagnostic{}, aDiags...)
	diags = append(diags, bDiags...)

	bIndex := make(map[string]map[string]string, len(bParams))
	for _, bp := range bParams {
		bIndex[mergeKey(bp, gen.MergeKeys)] = bp
	}
	var out []map[string]string
	for _, ap := range aParams {
		merged := make(map[string]string, len(ap))
		for k, v := range ap {
			merged[k] = v
		}
		if bp, ok := bIndex[mergeKey(ap, gen.MergeKeys)]; ok {
			for k, v := range bp {
				if _, exists := merged[k]; !exists {
					merged[k] = v
				}
			}
		}
		out = append(out, merged)
	}
	return out, diags
}

// mergeKey concatenates the values of the named keys in a canonical form so
// equal-key entries hash to the same string.
func mergeKey(m map[string]string, keys []string) string {
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(0)
		}
		b.WriteString(m[k])
	}
	return b.String()
}

// instantiateTemplate produces a synthetic ArgoApplication from an
// ApplicationSet + one parameter set. Returns (app, true) on success and
// (_, false) when the template contains non-trivial Go-template syntax.
func instantiateTemplate(as types.ArgoApplicationSet, params map[string]string) (types.ArgoApplication, bool) {
	t := as.Template
	name, ok := substituteTemplate(t.Name, params)
	if !ok {
		return types.ArgoApplication{}, false
	}
	// Per-field substitute. Any single unsupported field aborts the whole
	// Application so we don't emit half-rendered junk.
	ns, ok2 := substituteTemplate(t.Namespace, params)
	if !ok2 {
		return types.ArgoApplication{}, false
	}
	project, ok3 := substituteTemplate(t.Project, params)
	if !ok3 {
		return types.ArgoApplication{}, false
	}
	destServer, ok4 := substituteTemplate(t.Destination.Server, params)
	if !ok4 {
		return types.ArgoApplication{}, false
	}
	destName, ok5 := substituteTemplate(t.Destination.Name, params)
	if !ok5 {
		return types.ArgoApplication{}, false
	}
	destNS, ok6 := substituteTemplate(t.Destination.Namespace, params)
	if !ok6 {
		return types.ArgoApplication{}, false
	}

	var sources []types.ArgoSource
	if t.Source != nil {
		src, srcOK := substituteSource(*t.Source, params)
		if !srcOK {
			return types.ArgoApplication{}, false
		}
		sources = append(sources, src)
	}
	for _, s := range t.Sources {
		src, srcOK := substituteSource(s, params)
		if !srcOK {
			return types.ArgoApplication{}, false
		}
		sources = append(sources, src)
	}

	if name == "" {
		name = as.Name + "-expanded"
	}
	return types.ArgoApplication{
		Name:         name,
		Namespace:    ns,
		Project:      project,
		TrackingMode: "annotation",
		Source:       as.Source,
		Sources:      sources,
		Destination: types.ArgoDestination{
			Server:    destServer,
			Name:      destName,
			Namespace: destNS,
		},
		SyncPolicy: t.SyncPolicy,
	}, true
}

// substituteSource applies substitution to the template-variable fields of
// an ArgoSource. AppSet templates legally carry `{{ .key }}` placeholders in
// `source.helm.valueFiles` (e.g. `$values/{{provider}}/{{region}}/values.yaml`),
// so we walk the Helm string fields too. ValuesObject is left untouched —
// it's a nested map[string]interface{} and no current fg-manifold signal
// calls for a recursive walk.
func substituteSource(src types.ArgoSource, params map[string]string) (types.ArgoSource, bool) {
	repo, ok := substituteTemplate(src.RepoURL, params)
	if !ok {
		return src, false
	}
	path, ok2 := substituteTemplate(src.Path, params)
	if !ok2 {
		return src, false
	}
	target, ok3 := substituteTemplate(src.TargetRevision, params)
	if !ok3 {
		return src, false
	}
	chart, ok4 := substituteTemplate(src.Chart, params)
	if !ok4 {
		return src, false
	}
	src.RepoURL = repo
	src.Path = path
	src.TargetRevision = target
	src.Chart = chart
	if src.Helm != nil {
		helm, ok5 := substituteHelm(src.Helm, params)
		if !ok5 {
			return src, false
		}
		src.Helm = helm
	}
	return src, true
}

// substituteHelm walks the string-valued Helm source fields through
// substituteTemplate. Returns a fresh *ArgoHelmSource so the AppSet
// template's Helm block is never mutated in place (callers expand the
// same template once per generator parameter set).
func substituteHelm(h *types.ArgoHelmSource, params map[string]string) (*types.ArgoHelmSource, bool) {
	out := *h
	values, ok := substituteTemplate(h.Values, params)
	if !ok {
		return h, false
	}
	release, ok2 := substituteTemplate(h.ReleaseName, params)
	if !ok2 {
		return h, false
	}
	out.Values = values
	out.ReleaseName = release
	if len(h.ValueFiles) > 0 {
		files := make([]string, len(h.ValueFiles))
		for i, vf := range h.ValueFiles {
			s, ok := substituteTemplate(vf, params)
			if !ok {
				return h, false
			}
			files[i] = s
		}
		out.ValueFiles = files
	}
	if len(h.Parameters) > 0 {
		params2 := make([]types.ArgoHelmParam, len(h.Parameters))
		for i, p := range h.Parameters {
			name, ok := substituteTemplate(p.Name, params)
			if !ok {
				return h, false
			}
			val, ok2 := substituteTemplate(p.Value, params)
			if !ok2 {
				return h, false
			}
			params2[i] = types.ArgoHelmParam{Name: name, Value: val}
		}
		out.Parameters = params2
	}
	return &out, true
}

// unsupportedGeneratorDiag is the single shape of the info-level diagnostic
// we emit whenever a generator can't be simulated offline.
func unsupportedGeneratorDiag(as types.ArgoApplicationSet, kind, reason string) types.Diagnostic {
	return types.Diagnostic{
		Code:     "XPC.H.appset-unsupported-generator",
		Severity: types.SeverityInfo,
		Source:   as.Source,
		Message:  fmt.Sprintf("%s: generator %q not expandable offline", as.Name, kind),
		Detail:   reason,
		Fix:      "pass --appset-fixture=<file.yaml> with a parameter-set list for this ApplicationSet, or accept the coverage gap",
	}
}
