package schemas

import (
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// SchemaKey identifies a schema by the manifest's (apiVersion, kind) tuple.
// APIVersion is the full Kubernetes-style value (e.g. "example.com/v1alpha1").
type SchemaKey struct {
	APIVersion string
	Kind       string
}

// BuildSchemaIndex consolidates the XRD + CRD schema maps stored on a World
// into a single (apiVersion, kind) → schema-map index. This is the single
// source of truth for both R5 patch type resolution (pkg/checker) and R17
// resource-field validation (pkg/ir).
//
// For XRDs, one entry is emitted per (referenceable, served) version. For
// CRDs, one entry is emitted per version whose schemaDigest is known and
// whose `served` or `storage` flag is set.
//
// The returned map values are the raw schema sub-trees as stored in
// World.Schemas — i.e. the `{openAPIV3Schema: {...}}` wrapper is still present.
// Consumers should walk it via ResolveFieldType / ValidateManifest which both
// unwrap the wrapper.
func BuildSchemaIndex(w *types.World) map[SchemaKey]map[string]interface{} {
	if w == nil {
		return nil
	}
	out := make(map[SchemaKey]map[string]interface{})

	for _, xrd := range w.XRDs {
		for _, v := range xrd.Versions {
			if !v.Referenceable || v.SchemaDigest == "" {
				continue
			}
			si, ok := w.Schemas[v.SchemaDigest]
			if !ok {
				continue
			}
			apiVersion := xrd.Group + "/" + v.Name
			out[SchemaKey{APIVersion: apiVersion, Kind: xrd.Kind}] = si.Schema
		}
	}

	for _, crd := range w.CRDs {
		for _, v := range crd.Versions {
			if v.SchemaDigest == "" {
				continue
			}
			if !v.Served && !v.Storage {
				continue
			}
			si, ok := w.Schemas[v.SchemaDigest]
			if !ok {
				continue
			}
			apiVersion := crd.Group + "/" + v.Name
			out[SchemaKey{APIVersion: apiVersion, Kind: crd.Kind}] = si.Schema
		}
	}

	return out
}
