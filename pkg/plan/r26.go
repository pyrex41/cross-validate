package plan

import (
	"fmt"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// R26DestructiveDelete scans a ResourceDelta for changes that, when applied,
// would destroy "real" external state.
//
// Two concrete failure shapes:
//
//  1. A state-bearing Crossplane MR (by R23's allowlist) is Removed on the
//     head side, and its base-side `spec.deletionPolicy` is NOT Orphan. The
//     CR's deletion will cascade to the external cloud object (DROP DATABASE,
//     DeleteCluster, DeleteKey). Emit XPC.P.destructive-delete.
//
//  2. An argoproj.io/Application is Removed, and its base manifest carries
//     `resources-finalizer.argocd.argoproj.io` without an AppSet-level
//     preserveResourcesOnDeletion: true (the fg-synapse INC-6 shape).
//     Removing the Application triggers the cascade. Emit XPC.P.cascade-risk.
//
// Bypass: the base-side annotation set is inspected first. An explicit
//
//	xpc.io/allow-delete: "true"                     (primary)
//	policy.facilitygrid.io/allow-delete: "true"     (alias)
//
// silences both failure shapes for that single identity. The bypass has to
// live on the base side because the resource no longer exists on head.
//
// Returned diagnostics are plan-level (severity=error), distinct from the
// per-variant static diagnostics. They live in Plan.Diagnostics and drive the
// "## ⚠ Destructive changes" section of the markdown output.
func R26DestructiveDelete(delta ResourceDelta) []types.Diagnostic {
	stateBearing := buildStateBearingSet()

	var diags []types.Diagnostic
	for _, c := range delta.Removed {
		if hasBypassAnnotation(c.BaseRaw) {
			continue
		}

		gk := c.Identity.APIVersion + "/" + c.Identity.Kind
		// Crossplane's APIVersion is "group/version" — R23's registry keys
		// on bare group + kind. Extract the group and re-key.
		group := groupFromAPIVersion(c.Identity.APIVersion)
		if stateBearing[group+"/"+c.Identity.Kind] {
			policy := readDeletionPolicy(c.BaseRaw)
			if policy == "Orphan" {
				// Author explicitly opted into preservation — removing the
				// CR keeps the external object. Not destructive.
				continue
			}
			diags = append(diags, types.Diagnostic{
				Code:     "XPC.P.destructive-delete",
				Severity: types.SeverityError,
				Message: fmt.Sprintf("%s %s would be removed",
					c.Identity.Kind, qualifiedName(c.Identity)),
				Detail: fmt.Sprintf("%s is state-bearing (Group=%s). Base-side %s. "+
					"Applying this change will run a real destructive call against the external object. "+
					"This is the INC-6 failure shape.",
					c.Identity.Kind, group, deletionPolicyPhrase(policy)),
				Fix: fmt.Sprintf("Either (a) keep the resource on HEAD (revert the removal), "+
					"(b) set spec.deletionPolicy: Orphan on the base side before removing the CR, or "+
					"(c) add annotation xpc.io/allow-delete: \"true\" to the base manifest if the destruction is genuinely intended."),
				Source: c.BaseSource,
			})
			continue
		}

		// Argo Application removal.
		if gk == "argoproj.io/v1alpha1/Application" || gk == "v1alpha1/Application" ||
			(c.Identity.Kind == "Application" && group == "argoproj.io") {
			if hasCascadeFinalizer(c.BaseRaw) && !preserveResourcesOnDeletion(c.BaseRaw) {
				diags = append(diags, types.Diagnostic{
					Code:     "XPC.P.cascade-risk",
					Severity: types.SeverityError,
					Message: fmt.Sprintf("ArgoCD Application %s would be removed with cascading finalizer present",
						qualifiedName(c.Identity)),
					Detail: "Base manifest carries resources-finalizer.argocd.argoproj.io without " +
						"preserveResourcesOnDeletion: true. Removing the Application will cascade DELETE through " +
						"every resource it owns. This is the fg-synapse INC-6 trigger applied at the Application level.",
					Fix: "Either set spec.syncPolicy.preserveResourcesOnDeletion: true on the Application " +
						"before removing it, or drop the resources-finalizer entry from metadata.finalizers, " +
						"or keep the Application on HEAD.",
					Source: c.BaseSource,
				})
			}
		}
	}
	return diags
}

// buildStateBearingSet returns the R23 allowlist as a set keyed by
// "group/Kind". Reuses the registry directly — same source-of-truth as R23.
func buildStateBearingSet() map[string]bool {
	out := make(map[string]bool, 16)
	for _, gk := range ir.StateBearingKindsRegistry() {
		out[gk.Group+"/"+gk.Kind] = true
	}
	return out
}

// hasBypassAnnotation returns true when metadata.annotations carries either
// of the two recognized allow-delete keys set to "true". Mirrors R23's
// extractor so the two rules stay in lockstep.
func hasBypassAnnotation(raw map[string]interface{}) bool {
	if raw == nil {
		return false
	}
	meta, ok := raw["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	ann, ok := meta["annotations"].(map[string]interface{})
	if !ok {
		return false
	}
	if v, ok := ann["xpc.io/allow-delete"].(string); ok && v == "true" {
		return true
	}
	if v, ok := ann["policy.facilitygrid.io/allow-delete"].(string); ok && v == "true" {
		return true
	}
	return false
}

func readDeletionPolicy(raw map[string]interface{}) string {
	if raw == nil {
		return ""
	}
	spec, ok := raw["spec"].(map[string]interface{})
	if !ok {
		return ""
	}
	if s, ok := spec["deletionPolicy"].(string); ok {
		return s
	}
	return ""
}

func deletionPolicyPhrase(policy string) string {
	if policy == "" {
		return "spec.deletionPolicy is absent (Crossplane default is Delete)"
	}
	return "spec.deletionPolicy is " + policy + " (not Orphan)"
}

func hasCascadeFinalizer(raw map[string]interface{}) bool {
	if raw == nil {
		return false
	}
	meta, ok := raw["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	fins, ok := meta["finalizers"].([]interface{})
	if !ok {
		return false
	}
	for _, f := range fins {
		if s, ok := f.(string); ok {
			if s == "resources-finalizer.argocd.argoproj.io" ||
				s == "resources-finalizer.argocd.argoproj.io/foreground" {
				return true
			}
		}
	}
	return false
}

// preserveResourcesOnDeletion reads spec.syncPolicy.preserveResourcesOnDeletion
// from a manifest. Present on Application (per-app) — AppSet scope is
// separate and handled by R24.
func preserveResourcesOnDeletion(raw map[string]interface{}) bool {
	if raw == nil {
		return false
	}
	spec, ok := raw["spec"].(map[string]interface{})
	if !ok {
		return false
	}
	sp, ok := spec["syncPolicy"].(map[string]interface{})
	if !ok {
		return false
	}
	if v, ok := sp["preserveResourcesOnDeletion"].(bool); ok {
		return v
	}
	return false
}

func qualifiedName(id ResourceIdentity) string {
	if id.Namespace != "" {
		return id.Namespace + "/" + id.Name
	}
	return id.Name
}

// groupFromAPIVersion returns the group portion of an APIVersion. For
// "group/version" returns "group"; for bare "v1" returns "". Matches the
// extractor used by bridge.go and trajectory_extract.go.
func groupFromAPIVersion(apiVersion string) string {
	for i, c := range apiVersion {
		if c == '/' {
			return apiVersion[:i]
		}
	}
	return ""
}
