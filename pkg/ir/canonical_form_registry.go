package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// CanonicalFormRegistry returns the static catalog of Crossplane provider
// forProvider fields whose value the provider canonicalizes on read-back, so
// that setting a non-canonical literal produces a permanent upjet diff. Each
// row is the static (Tier-1) and dynamic (Tier-3) seed for category M
// (Convergence / steady-state): the invariant is that the field must either
// hold a canonical-form value (a fixed point of the provider's read-back) or
// be excluded from the external Update via managementPolicies.
//
// Why this is a distinct class from LateInitRegistry:
//
//   - Late-init (R21) is Argo-vs-Crossplane: the provider writes an observed
//     value back into spec.forProvider, and Argo reverts it. Remedy: Argo
//     ignoreDifferences.
//   - Canonical-form (R31/R32) is upjet-vs-cloud: the desired value the
//     composition wrote can never equal the cloud's normalized read-back, so
//     upjet calls Update on every reconcile and the status write conflicts
//     with the poll loop. Argo is not in the loop; ignoreDifferences does
//     nothing. Remedy: write the canonical value, or set managementPolicies
//     to omit Update.
//
// Discovery method mirrors LateInitRegistry: each row is extracted from a
// fg-manifold GitLab MR that fixed a reconcile storm. The Reason field cites
// the MR. Append-only; keep citations anchored to MR numbers.
//
// Detectors (see CanonicalFormMapping.Detector):
//
//   - "arn-requires-revision": the segment after the last "/" (or the whole
//     scalar when there is no "/") must contain a ":" — i.e. a versioned
//     family:revision form. A bare family name or an unversioned ARN fails.
func CanonicalFormRegistry() []types.CanonicalFormMapping {
	return []types.CanonicalFormMapping{
		// ── ecs.aws.upbound.io / Service ───────────────────────────────────
		// !2232: preview ECS Service set forProvider.taskDefinition to the bare
		// family name (no revision). AWS normalises the read-back to
		// family:revision, so upjet saw a permanent diff and fired an endless
		// async UpdateService whose status write 409-conflicted with the
		// 1-minute poll loop — ~5.7 reconciles/sec at rest, pinning all 100
		// workers. Fix resolved the versioned ARN from the observed
		// TaskDefinition so forProvider matched the read-back.
		{
			Group:     "ecs.aws.upbound.io",
			Kind:      "Service",
			FieldPath: "spec.forProvider.taskDefinition",
			Detector:  "arn-requires-revision",
			Canonical: "family:revision (or .../task-definition/family:revision), resolved from the observed TaskDefinition status.atProvider.arn",
			Reason:    "ECS Service taskDefinition bare family name → permanent upjet diff → reconcile storm; MR !2232",
		},
	}
}
