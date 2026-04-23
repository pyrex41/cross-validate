\* r23-crossplane-state-needs-orphan.shen — Rule R23:
   XPC.S.crossplane-state-needs-orphan

   Emit XPC.S.crossplane-state-needs-orphan for every Crossplane managed
   resource whose (Group, Kind) is in the state-bearing allowlist and whose
   spec.deletionPolicy is anything other than "Orphan".

   Background: Crossplane managed resources default to deletionPolicy: Delete.
   When the CR is removed, the underlying AWS/SQL/KMS object is destroyed —
   a real `DROP DATABASE` / `DeleteCluster` / `DeleteKey` call. For kinds that
   hold "real" state (Aurora, DocDB, MySQL, KMS, S3, VPC), this is almost
   never what the author wants; the CR lifecycle should be decoupled from the
   external object's lifecycle via `deletionPolicy: Orphan`. This is the
   static-floor analog of fg-manifold's `crossplane-state-require-orphan`
   ValidatingAdmissionPolicy — xpc enforces the same invariant in CI, across
   all envs, before the VAP has a chance to see the resource.

   Fact shape (from pkg/checker/bridge.go cpDeletionPolicyToObj):
     [cp-deletion-policy-fact Group Kind Name Namespace DeletionPolicy BypassSym Src]
   where BypassSym is `bypass-yes` or `bypass-no`. The Go side pre-filters to
   kinds in the state-bearing registry, so every fact reaching this rule is
   already in-scope — the Shen side only decides on policy/bypass/name.

   Bypass:
     - annotation `xpc.io/allow-delete: "true"` (primary) OR
       `policy.facilitygrid.io/allow-delete: "true"` (alias);
       the Go extractor collapses both to the single `bypass-yes` symbol.
     - resource name string-contains "alb-logs" (name carve-out) — ALB access
       log buckets are separately managed and intentionally destroyable.

   NOTE on `cn`: Shen's `cn` takes exactly 2 string arguments. Every `cn`
   call here is strictly 2-argument; longer concatenations are written as
   nested `(cn s1 (cn s2 s3))` chains.

   NOTE on pattern shape: the two check-row branches bind every positional
   slot to a named variable (even when unused in the body). The `_` wildcard
   in a slot that is named in the sibling branch triggered an empty-message
   panic in shen-go at load time — defensive uniformity avoids it. *\


(define r23-alb-logs?
  Name -> (string-contains? Name "alb-logs"))


(define r23-orphan?
  Policy -> (= Policy "Orphan"))


\* r23-policy-phrase — human-readable phrasing for the diagnostic detail.
   Empty DeletionPolicy is the worst case (default Delete); call it out
   explicitly so the reader isn't left wondering why it fired on a resource
   with no deletionPolicy at all. Use if-test rather than a literal-""
   pattern — shen-go pattern matching on empty string crashes load. *\
(define r23-policy-phrase
  Policy ->
    (if (= Policy "")
        "spec.deletionPolicy is absent (Crossplane default is Delete)"
        (cn "spec.deletionPolicy is " (cn Policy " (not Orphan)"))))


\* r23-emit — build the judgment for one in-scope, non-Orphan, non-bypassed,
   non-carve-out resource. Source points at the resource manifest; that's
   where the author fixes it. *\
(define r23-emit
  Group Kind Name Policy Src ->
    (make-error "XPC.S.crossplane-state-needs-orphan"
      Src
      (cn Kind (cn "/" (cn Name
        " is a state-bearing Crossplane managed resource without deletionPolicy: Orphan")))
      (cn (r23-policy-phrase Policy)
        (cn ". Group " (cn Group
          (cn ", Kind " (cn Kind
            (cn " is in the state-bearing allowlist (Aurora, DocDB, MySQL, KMS, S3, VPC). "
              "Default Crossplane deletion will run a real destructive call against the external object. This is the INC-6 failure mode."))))))
      (cn "Set spec.deletionPolicy: Orphan on this resource. "
        (cn "If destruction is genuinely intended (e.g. throwaway test), "
          "add annotation xpc.io/allow-delete=true OR policy.facilitygrid.io/allow-delete=true to bypass."))
      []))


\* r23-check-row — dispatch one cp-deletion-policy-fact. Returns [] or a
   singleton judgment list. Gating order: bypass annotation first (cheapest
   silent path), then Orphan, then alb-logs carve-out, then emit. *\
(define r23-check-row
  [cp-deletion-policy-fact Group Kind Name Ns Policy bypass-yes Src] -> []
  [cp-deletion-policy-fact Group Kind Name Ns Policy bypass-no Src] ->
    (if (r23-orphan? Policy)
        []
        (if (r23-alb-logs? Name)
            []
            [(r23-emit Group Kind Name Policy Src)]))
  _ -> [])


\* check-r23 — top-level R23 check.
   Facts: list of cp-deletion-policy-fact tuples, one per in-scope resource. *\
(define check-r23
  Facts -> (flatten (map (/. Row (r23-check-row Row)) Facts)))
