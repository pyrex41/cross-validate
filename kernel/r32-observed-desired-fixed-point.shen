\* r32-observed-desired-fixed-point.shen — Rule R32: XPC.M.observed-desired-fixed-point

   Category M (Convergence / steady-state), Tier-3 (dynamic). Emit
   XPC.M.observed-desired-fixed-point for every forProvider leaf on a live
   (status-bearing) managed resource whose value diverges from the matching
   status.atProvider leaf.

   This is the reconcile-storm fingerprint captured from reality rather than
   predicted: desired (spec.forProvider.X) != observed (status.atProvider.X)
   means the provider keeps trying to reconcile a value the cloud will never
   echo back. It only produces rows when status is present — i.e. a
   --from-cluster snapshot merged into the World; on plain disk manifests
   atProvider is absent and R32 is silent.

   Severity is registry-aware (the Go bridge sets the registered- symbol):
     - registered-yes: (Group,Kind,leaf) is in the canonical-form registry, a
       known-non-convergent field. A single snapshot is conclusive → error.
     - registered-no: the high-recall long tail the registry does not know
       about. A single snapshot cannot distinguish a storm from a resource
       mid-update → warn; confirm with a second snapshot taken minutes later.

   The Go bridge drops divergences that cannot storm (managementPolicies
   disables Update) before they reach the kernel. *\


(define r32-message
  Group Kind FieldPath Desired Observed ->
    (cn "The field " (cn FieldPath
      (cn " on " (cn Kind
        (cn " (group: " (cn Group
          (cn ") declares " (cn Desired
            (cn " but the provider observes " (cn Observed
              " — desired never converges to observed. The provider re-issues the external Update on every reconcile (a reconcile storm).")))))))))))

(define r32-fix
  -> "Set forProvider to the provider's canonical read-back form (resolve it from status.atProvider), or set spec.managementPolicies to omit Update. If this is a transient mid-update value, confirm against a second snapshot before acting.")


(define r32-violation-to-judgment
  [r32-violation Group Kind Name _ FieldPath Desired Observed registered-yes Src] ->
    (make-error "XPC.M.observed-desired-fixed-point"
      Src
      (cn Kind (cn "/" (cn Name (cn ": forProvider/atProvider divergence at " FieldPath))))
      (r32-message Group Kind FieldPath Desired Observed)
      (r32-fix)
      [])
  [r32-violation Group Kind Name _ FieldPath Desired Observed registered-no Src] ->
    (make-warning "XPC.M.observed-desired-fixed-point"
      Src
      (cn Kind (cn "/" (cn Name (cn ": forProvider/atProvider divergence at " FieldPath))))
      (r32-message Group Kind FieldPath Desired Observed)
      (r32-fix)
      [])
  _ -> [])


\* check-r32 — top-level R32 check. Go pre-filters to actionable divergences. *\
(define check-r32
  Violations -> (map (/. V (r32-violation-to-judgment V)) Violations))
