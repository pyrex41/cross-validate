package ir

import (
	"github.com/pyrex41/cross-validate-/pkg/schemas"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// EnrichFieldValidation walks every resource in the World against the
// corresponding CRD/XRD schema (looked up by APIVersion + Kind) and populates
// World.ResourceFieldFacts with one fact per violation found.
//
// Resources whose (apiVersion, kind) does not appear in the schema index are
// skipped silently — not every resource in a world is covered by a CRD (core
// Kubernetes types, Argo CD resources, etc.).
func EnrichFieldValidation(w *types.World) {
	if w == nil {
		return
	}
	index := schemas.BuildSchemaIndex(w)
	if len(index) == 0 {
		return
	}

	for _, res := range w.Resources {
		key := schemas.SchemaKey{APIVersion: res.APIVersion, Kind: res.Kind}
		schema, ok := index[key]
		if !ok {
			continue
		}
		facts := schemas.ValidateManifest(schema, res.Raw)
		for i := range facts {
			facts[i].APIVersion = res.APIVersion
			facts[i].Kind = res.Kind
			facts[i].Name = res.Name
			facts[i].Namespace = res.Namespace
			facts[i].Source = res.Source
		}
		w.ResourceFieldFacts = append(w.ResourceFieldFacts, facts...)
	}
}
