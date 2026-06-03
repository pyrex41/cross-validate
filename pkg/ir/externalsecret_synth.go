package ir

import (
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// synthesizeExternalSecretTargets injects a virtual Secret ResourceInfo for
// every ExternalSecret's target Secret, so trajectory-aware rules treat
// operator-materialized Secrets as present in cluster state.
//
// An external-secrets.io ExternalSecret declares spec.target.name (defaulting
// to the ExternalSecret's own metadata.name) — the Secret the external-secrets
// operator creates at reconcile time. That Secret never appears as a committed
// manifest, so without this pass R12 (XPC012, no-dangling-mount) false-fires on
// every workload that mounts an ESO-backed Secret: the mount target is "absent
// from the trajectory state" purely because it is produced at runtime.
//
// The synthesized Secret copies the ExternalSecret's namespace and owning app,
// and is modeled at sync-wave 0 (present from the start of the trajectory)
// rather than at the ExternalSecret's own wave: the external-secrets operator
// materializes the Secret asynchronously, independent of Argo's apply ordering,
// so a workload that mounts it at an earlier wave is not actually dangling. It
// carries Provenance "produced:externalsecret:<name>" — mirroring the existing
// "rendered:helm:<app>" convention for non-manifest resources in w.Resources.
//
// A synthesized Secret is suppressed when a real Secret with the same
// (namespace, name) is already committed, so this never shadows a manifest or
// manufactures an R7 key collision.
func synthesizeExternalSecretTargets(w *types.World) {
	seen := map[[2]string]bool{}
	for i := range w.Resources {
		if w.Resources[i].Kind == "Secret" {
			seen[[2]string{w.Resources[i].Namespace, w.Resources[i].Name}] = true
		}
	}

	var synth []types.ResourceInfo
	for i := range w.Resources {
		r := w.Resources[i]
		if !isExternalSecret(r) {
			continue
		}
		target := externalSecretTargetName(r)
		if target == "" {
			continue
		}
		key := [2]string{r.Namespace, target}
		if seen[key] {
			continue
		}
		seen[key] = true

		synth = append(synth, types.ResourceInfo{
			APIVersion: "v1",
			Kind:       "Secret",
			Name:       target,
			Namespace:  r.Namespace,
			Source:     r.Source,
			Provenance: "produced:externalsecret:" + r.Name,
			OwningApp:  r.OwningApp,
		})
	}
	w.Resources = append(w.Resources, synth...)
}

// isExternalSecret reports reports whether r is an external-secrets.io ExternalSecret.
func isExternalSecret(r types.ResourceInfo) bool {
	return r.Kind == "ExternalSecret" &&
		strings.HasPrefix(r.APIVersion, "external-secrets.io/")
}

// externalSecretTargetName returns the name of the Secret the ExternalSecret
// produces: spec.target.name when set, otherwise the ExternalSecret's own name
// (external-secrets' documented default).
func externalSecretTargetName(r types.ResourceInfo) string {
	if r.Raw != nil {
		if spec := getMap(r.Raw, "spec"); spec != nil {
			if target := getMap(spec, "target"); target != nil {
				if name, ok := target["name"].(string); ok && name != "" {
					return name
				}
			}
		}
	}
	return r.Name
}
