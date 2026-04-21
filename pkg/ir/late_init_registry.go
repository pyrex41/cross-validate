package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// LateInitRegistry returns the static catalog of Crossplane provider fields
// that upjet-generated providers late-initialize from observed cloud state
// (AWS/GCP fills them in after Create; the provider writes them back into
// spec.forProvider; ArgoCD shows perpetual drift unless ignoreDifferences
// suppresses them).
//
// Discovery method: each row was extracted from a fg-manifold GitLab MR that
// fixed perpetual OutOfSync on the kind. The `Reason` field cites the MR
// number. Seed set was harvested 2026-04-21 via:
//
//	glab mr view <n> --output json
//	glab mr diff <n>
//
// for MRs !1048, !1502, !893 (clean late-init fixes on forProvider fields).
//
// Scope decisions:
//
//   - Selector-resolution fields (e.g. `securityGroups` populated from
//     `securityGroupSelector`) are omitted. R16 already covers those.
//   - `initProvider` drift (!1172) is omitted: that is ArgoCD SSA null-
//     serialization, not late-init observed-field write-back. Different class.
//   - `managementPolicies` explicit declarations (!1147, !1247) are omitted:
//     those are policy-ownership fixes, not late-init per se.
//
// Expand this list whenever a new fg-manifold MR declares a forProvider
// late-init fix. Append-only; keep citations anchored to MR numbers so a
// future maintainer can trace why the row exists.
func LateInitRegistry() []types.LateInitMapping {
	return []types.LateInitMapping{
		// ── elbv2.aws.upbound.io / LB ──────────────────────────────────────
		// !1048: ALB perpetual drift — Crossplane late-inits connection and
		// health-check settings from AWS-observed state. Fix declared these
		// fields in the manifest and added an ignoreDifferences entry to
		// the crossplane-platform-aws ApplicationSet.
		{
			Group:      "elbv2.aws.upbound.io",
			Kind:       "LB",
			FieldPath:  "spec.forProvider.clientKeepAlive",
			FixPattern: "ignoreDifferences",
			Reason:     "ALB clientKeepAlive late-init from AWS state; MR !1048",
		},
		{
			Group:      "elbv2.aws.upbound.io",
			Kind:       "LB",
			FieldPath:  "spec.forProvider.idleTimeout",
			FixPattern: "ignoreDifferences",
			Reason:     "ALB idleTimeout late-init from AWS state; MR !1048",
		},
		{
			Group:      "elbv2.aws.upbound.io",
			Kind:       "LB",
			FieldPath:  "spec.forProvider.enableHttp2",
			FixPattern: "ignoreDifferences",
			Reason:     "ALB enableHttp2 late-init from AWS state; MR !1048",
		},
		{
			Group:      "elbv2.aws.upbound.io",
			Kind:       "LB",
			FieldPath:  "spec.forProvider.ipAddressType",
			FixPattern: "ignoreDifferences",
			Reason:     "ALB ipAddressType late-init from AWS state; MR !1048",
		},

		// ── ec2.aws.upbound.io / LaunchTemplate ────────────────────────────
		// !893: Crossplane late-inits name and tags on LaunchTemplate.
		// Previous ignoreDifferences only covered the *Ref/Selector fields
		// (R16 territory); this MR added the pure late-init fields.
		{
			Group:      "ec2.aws.upbound.io",
			Kind:       "LaunchTemplate",
			FieldPath:  "spec.forProvider.name",
			FixPattern: "ignoreDifferences",
			Reason:     "LaunchTemplate name late-init from AWS state; MR !893",
		},
		{
			Group:      "ec2.aws.upbound.io",
			Kind:       "LaunchTemplate",
			FieldPath:  "spec.forProvider.tags",
			FixPattern: "ignoreDifferences",
			Reason:     "LaunchTemplate tags late-init from AWS state; MR !893",
		},

		// ── ecs.aws.upbound.io / Service ───────────────────────────────────
		// !1502: Fargate Service — upjet v1beta2 late-inits the auto-
		// populated AWSServiceRoleForECS ARN back into spec.forProvider.
		// iamRole. Subsequent CreateService calls (composition rotates
		// $svcVersion) then fail because AWS rejects the explicit iamRole
		// on Fargate. Fix: set managementPolicies to omit LateInitialize.
		{
			Group:      "ecs.aws.upbound.io",
			Kind:       "Service",
			FieldPath:  "spec.forProvider.iamRole",
			FixPattern: "omit-late-initialize",
			Reason:     "Fargate Service iamRole late-init breaks rotation; MR !1502",
		},
	}
}
