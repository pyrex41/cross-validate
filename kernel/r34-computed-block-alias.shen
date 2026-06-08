\* r34-computed-block-alias.shen — Rule R34: XPC.M.computed-block-alias

   Category M (Convergence / steady-state), Tier-2 (heuristic). Emit
   XPC.M.computed-block-alias for every go-templating Composition that writes a
   provider-computed-block action in the simple scalar-alias form instead of the
   canonical sub-block.

   The seed case is an elbv2 LBListenerRule (or LBListener) forward action
   written with targetGroupArn (or targetGroupArnRef/targetGroupArnSelector)
   instead of the canonical forward target-group block plus explicit order. AWS
   ALWAYS reads back the full action.forward block (stickiness + targetGroup) plus
   order 1, so the alias form leaves forProvider permanently unequal to the
   read-back → upjet re-issues UpdateRule every reconcile and the status write 409s
   with the poll loop → reconcile storm on provider-aws-elbv2. This is the
   missing-computed-BLOCK sibling of R31 (non-canonical SCALAR): desired never
   equals observed.

   The Go bridge precomputes one r34-violation per (composition, kind, alias
   field). The kernel renders the judgment at warn severity — a template-text
   scan of an unrendered block cannot be as certain as a concrete resource. The
   remediation text carries the stickiness trap: omit stickiness (Optional+
   Computed), since adding a disabled stickiness with a non-zero duration
   re-introduces a NEW perpetual diff. *\


(define r34-violation-to-judgment
  [r34-violation Composition Group Kind ActionType AliasField CanonicalBlock Reason Src] ->
    (make-warning "XPC.M.computed-block-alias"
      Src
      (cn "Composition " (cn Composition
        (cn ": " (cn Kind (cn " " (cn ActionType
          (cn " action uses the " (cn AliasField " alias instead of the canonical block"))))))))
      (cn Kind (cn " " (cn ActionType
        (cn " action in composition " (cn Composition
          (cn " sets " (cn AliasField
            (cn " but has no " (cn CanonicalBlock
              (cn " block. AWS always computes a full action." (cn CanonicalBlock
                " block plus order 1 for a forward action, so forProvider perpetually diffs against the read-back, driving an upjet async-update storm on provider-aws-elbv2 (the unfixed RetryOnConflict status-409 bug amplifies it). If the alias and the canonical block live on different resources of the same template this is a false positive.")))))))))))
      (cn "Emit the canonical forward target group via arn, arnRef, or arnSelector plus weight plus an explicit order, and leave stickiness unset (Optional+Computed; adding a disabled stickiness with a non-zero duration re-introduces a NEW perpetual diff). "
        Reason)
      [])
  _ -> [])


\* check-r34 — top-level R34 check. Go pre-filters to alias-form actions. *\
(define check-r34
  Violations -> (map (/. V (r34-violation-to-judgment V)) Violations))
