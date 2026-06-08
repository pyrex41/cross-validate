package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// OrphanSGRefRegistry returns the static catalog of Crossplane managed "rule"
// resources (a rule attached to one resource that references a second) whose
// cross-scope reference can dangle on teardown and wedge re-creation. Each row is
// the seed for category S (Safety / state-preservation) rule R36
// (XPC.S.orphaned-sgref).
//
// The seed case is ec2 SecurityGroupRule (fg-manifold commit d144aa739b, the
// preview SG-orphan wedge): the preview fargateapp composition emits, for each
// per-env SecurityGroup (deletionPolicy: Delete — short-lived), an EGRESS
// SecurityGroupRule that is ATTACHED to the SHARED, long-lived
// `fg-preview-alb-sg` (via `securityGroupIdSelector`) but REFERENCES the per-env
// SG (via `sourceSecurityGroupIdSelector`). On teardown the per-env SG's Delete
// runs, but the egress rule on the shared ALB SG is NOT torn down with it (it is
// a separate managed resource whose attach target survives) — so the reference
// dangles. AWS then refuses `DeleteSecurityGroup` on the per-env SG with
// `DependencyViolation` even at zero ENIs, the SG MR sits stuck Terminating, and
// a re-create of the same PR fails `InvalidGroup.Duplicate` → the whole web-app
// preview recycle wedges. The fix was a teardown-side reaper that revokes the
// dangling rule then deletes the SG.
//
// The static signal, per OrphanSGRefMapping, is the ASYMMETRY: a rule whose
// AttachField points one way and whose RefField points the other, where the two
// targets resolve to different lifecycles (a long-lived/shared SG vs a per-env
// SG). See pkg/ir/orphan_sgref_check.go for exactly how the heuristic infers the
// asymmetry from the template, and the precision limits.
//
// Append-only; anchor each Reason to the fixing commit/MR.
func OrphanSGRefRegistry() []types.OrphanSGRefMapping {
	return []types.OrphanSGRefMapping{
		// ── ec2.aws.upbound.io / SecurityGroupRule ─────────────────────────
		// d144aa739b: preview per-env SG (deletionPolicy: Delete) is pinned by a
		// dangling egress rule that lives on the SHARED fg-preview-alb-sg but
		// references the per-env SG → DeleteSecurityGroup DependencyViolation at 0
		// ENIs → SG MR stuck Terminating 16d → recreate InvalidGroup.Duplicate.
		{
			Group:              "ec2.aws.upbound.io",
			Kind:               "SecurityGroupRule",
			RefGroup:           "ec2.aws.upbound.io",
			RefKind:            "SecurityGroup",
			AttachFieldPattern: `(?m)^[ \t]*(securityGroupId(?:Ref|Selector)?):`,
			RefFieldPattern:    `(?m)^[ \t]*((?:source|referenced)SecurityGroupId(?:Ref|Selector)?):`,
			Reason:             "SecurityGroupRule attached to a long-lived/shared SG but referencing a short-lived per-env SG: on teardown the referenced SG is deleted while the rule is not, so the reference dangles → DeleteSecurityGroup DependencyViolation at 0 ENIs → SG stuck Terminating → recreate InvalidGroup.Duplicate; commit d144aa739b",
		},
	}
}
