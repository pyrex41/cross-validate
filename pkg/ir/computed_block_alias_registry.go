package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// ComputedBlockAliasRegistry returns the static catalog of Crossplane provider
// actions that the provider ALWAYS reads back as a full computed sub-block, so
// that writing the convenient scalar-alias form (instead of the canonical block)
// produces a permanent upjet diff. Each row is the category-M (Convergence /
// steady-state) seed for the Tier-2 template heuristic R34
// (XPC.M.computed-block-alias).
//
// Why this is distinct from CanonicalFormRegistry (R31):
//
//   - R31 is a non-canonical SCALAR: the leaf value (e.g. a bare ECS family
//     name) can never equal the provider's normalized read-back, so the
//     detector inspects the scalar RHS.
//   - R34 is a missing computed BLOCK: the action is legal as a scalar alias
//     (`targetGroupArn`) OR as a sub-block (`forward{}`), but the provider only
//     ever reads back the sub-block + computed scalars (`order: 1`). The alias
//     form therefore perpetually diffs even though every value in it is
//     "correct". The signal is the PRESENCE of a sibling block, not a scalar
//     value — so it needs its own whole-block scan, not the leaf-regex dispatch.
//
// Both end the same way: desired != observed forever → upjet re-issues the
// external Update every reconcile → the async status write 409-conflicts with
// the poll loop → reconcile storm. Append-only; anchor each Reason to the MR
// that fixed the storm.
func ComputedBlockAliasRegistry() []types.ComputedBlockAliasMapping {
	return []types.ComputedBlockAliasMapping{
		// ── elbv2.aws.upbound.io / LBListenerRule ──────────────────────────
		// !2336: preview LBListenerRule `forward` actions used the simple
		// targetGroupArnSelector form. AWS always computes the full
		// action.forward{ stickiness{}, targetGroup[]{} } block + order:1, so
		// forProvider.action never matched the read-back → upjet fired UpdateRule
		// every reconcile and the async callback 409-conflicted on the status
		// write (the unfixed upjet RetryOnConflict bug) → permanent storm on
		// provider-aws-elbv2 (pod restarted 8x). Fix: emit
		// forward.targetGroup[].arn{,Ref,Selector} + weight + explicit order:,
		// leaving stickiness unset (Optional+Computed — adding a disabled
		// stickiness with a non-zero duration re-introduces a NEW perpetual diff).
		{
			Group:             "elbv2.aws.upbound.io",
			Kind:              "LBListenerRule",
			ActionType:        "forward",
			AliasFieldPattern: `(?m)^[ \t]*(targetGroupArn(?:Ref|Selector)?):`,
			CanonicalBlockKey: "forward",
			Reason:            "LBListenerRule forward action uses the simple targetGroupArn form; AWS always computes a full action.forward{} block + order:1, so forProvider perpetually diffs → upjet async-update storm on provider-aws-elbv2; MR !2336",
		},
		// ── elbv2.aws.upbound.io / LBListener ──────────────────────────────
		// The defaultAction of an LBListener has the identical forward-vs-computed
		// shape (aws_lb_listener.default_action). Same alias keys, same canonical
		// block, same storm. Seeded so the gate also catches it on the alias form.
		{
			Group:             "elbv2.aws.upbound.io",
			Kind:              "LBListener",
			ActionType:        "forward",
			AliasFieldPattern: `(?m)^[ \t]*(targetGroupArn(?:Ref|Selector)?):`,
			CanonicalBlockKey: "forward",
			Reason:            "LBListener defaultAction forward uses the simple targetGroupArn form; AWS always computes a full action.forward{} block + order:1, so forProvider perpetually diffs → upjet async-update storm on provider-aws-elbv2; MR !2336",
		},
	}
}
