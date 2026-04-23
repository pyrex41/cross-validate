package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// StateBearingKindsRegistry returns the hand-curated allowlist of Crossplane
// managed-resource kinds that carry "real" external state — kinds whose
// underlying AWS/SQL/KMS object survives beyond the CR itself and must not be
// destroyed by a cascading delete. Any resource of one of these kinds must
// declare spec.deletionPolicy: Orphan.
//
// The source-of-truth is fg-manifold's crossplane-state-require-orphan
// ValidatingAdmissionPolicy (see deploy/facilitygrid/ops/applications/
// admission-policies/aws/us-east-2/facilitygrid-ops/manifests/
// crossplane-state-require-orphan.yaml). This mirrors its matchConstraints
// resourceRules block verbatim. Keep the two in sync: fg-manifold enforces at
// runtime, xpc enforces in CI.
//
// Rationale: fg-synapse INC-6 (2026-04-22 postmortem) — a cascaded
// `kubectl delete application` pushed DELETE through ~70 managed resources
// because their default deletionPolicy is Delete. Setting Orphan decouples
// the CR lifecycle from the external object's lifecycle.
//
// Expand by appending to this slice — one entry per (Group, Kind). When
// adding, keep groups grouped and kinds within a group sorted alphabetically.
func StateBearingKindsRegistry() []types.ArgoGroupKind {
	return []types.ArgoGroupKind{
		// Aurora RDS — Cluster holds the DB; ClusterInstance is a node.
		{Group: "rds.aws.upbound.io", Kind: "Cluster"},
		{Group: "rds.aws.upbound.io", Kind: "ClusterInstance"},

		// DocDB — same Cluster/ClusterInstance split as RDS.
		{Group: "docdb.aws.upbound.io", Kind: "Cluster"},
		{Group: "docdb.aws.upbound.io", Kind: "ClusterInstance"},

		// SQL provider — database-level objects whose deletion drops user data.
		{Group: "mysql.sql.crossplane.io", Kind: "Database"},
		{Group: "mysql.sql.crossplane.io", Kind: "User"},
		{Group: "mysql.sql.crossplane.io", Kind: "Grant"},

		// KMS — key deletion is effectively irreversible (7–30 day window).
		{Group: "kms.aws.upbound.io", Kind: "Key"},

		// S3 — Bucket contents are external state; deletion deletes objects.
		{Group: "s3.aws.upbound.io", Kind: "Bucket"},

		// VPC — network identity; many downstream resources keep its id.
		{Group: "ec2.aws.upbound.io", Kind: "VPC"},
	}
}
