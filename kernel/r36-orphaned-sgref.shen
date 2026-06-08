\* r36-orphaned-sgref.shen — Rule R36: XPC.S.orphaned-sgref

   Category S (Safety / state-preservation), Tier-2 (heuristic). Emit
   XPC.S.orphaned-sgref for every go-templating Composition that emits a rule
   resource (e.g. ec2 SecurityGroupRule) whose peer reference dangles on
   teardown: the rule is ATTACHED to a long-lived/shared resource but REFERENCES a
   short-lived, composition-scoped resource. When the composition is torn down the
   short-lived resource is deleted while the rule on the long-lived resource is
   not, so the reference dangles and pins the short-lived resource.

   The seed case is the preview SG-orphan wedge (fg-manifold commit d144aa739b):
   an egress SecurityGroupRule attached (securityGroupIdSelector) to the shared
   fg-preview-alb-sg but referencing (sourceSecurityGroupIdSelector) the per-env
   SG. On teardown DeleteSecurityGroup on the per-env SG fails DependencyViolation
   EVEN AT ZERO ENIs, the SG MR sits stuck Terminating, and a recreate fails
   InvalidGroup.Duplicate → the whole web-app preview recycle wedges. The fix was
   a teardown-side reaper.

   The Go bridge precomputes one r36-violation per offending rule block. The
   kernel renders the judgment at WARN severity — a template-text scan that infers
   the asymmetric lifecycle (the referenced SG is composition-scoped, so
   short-lived; the attach SG is foreign, so long-lived) cannot be as certain as
   observing two concrete resources with their deletionPolicies. *\


(define r36-violation-to-judgment
  [r36-violation Composition Group Kind RuleName AttachField RefField Reason Src] ->
    (make-warning "XPC.S.orphaned-sgref"
      Src
      (cn "Composition " (cn Composition (cn ": " (cn Kind (cn " " (cn RuleName " references a short-lived peer SG while attached to a long-lived/shared SG — dangles on teardown"))))))
      (cn Kind (cn " " (cn RuleName (cn " in composition " (cn Composition (cn " is attached via " (cn AttachField (cn " to a security group NOT created by this composition (long-lived/shared) but references a security group it DOES create (short-lived, per-env) via " (cn RefField ". On teardown the referenced SG is deleted while this rule on the shared SG survives, so the reference dangles and pins the deleted SG: DeleteSecurityGroup fails DependencyViolation even at zero ENIs, the SG sits stuck Terminating, and a recreate fails InvalidGroup.Duplicate. If the two SGs actually share a lifecycle this is a false positive.")))))))))
      (cn "Tear the cross-scope rule down WITH the short-lived SG (own it in the same scope / make its deletion revoke the rule first), or reap the dangling rule on teardown. To accept the risk add xpc.io/allow-orphan-sgref: true on the rule. " Reason)
      [])
  _ -> [])


\* check-r36 — top-level R36 check. Go pre-filters to dangling cross-scope rule
   blocks. *\
(define check-r36
  Violations -> (map (/. V (r36-violation-to-judgment V)) Violations))
