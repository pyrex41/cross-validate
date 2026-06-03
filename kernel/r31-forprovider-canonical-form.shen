\* r31-forprovider-canonical-form.shen — Rule R31: XPC.M.forprovider-canonical-form

   Category M (Convergence / steady-state), Tier-1 (static). Emit
   XPC.M.forprovider-canonical-form for every managed resource that sets a
   registered, provider-canonicalized forProvider field to a non-canonical
   literal value.

   Background: some upjet-generated provider fields are canonicalized by the
   cloud on read-back (e.g. ECS Service spec.forProvider.taskDefinition is
   echoed as family:revision). Writing a non-canonical literal (a bare family
   name) makes desired != observed forever: upjet re-issues the external
   Update on every reconcile and the status write conflicts with the poll
   loop — a self-sustaining reconcile storm (fg-manifold MR !2232).

   This is distinct from R21 (late-init): the fight is upjet-vs-cloud, not
   Argo-vs-Crossplane, so an Argo ignoreDifferences entry does NOT fix it. The
   remedy is a canonical-form value, or a managementPolicies that omits Update.

   The Go bridge precomputes coverage: it emits an r31-violation only for a
   non-canonical value that upjet would actually push (managementPolicies does
   not disable Update) and that no xpc.io/allow-noncanonical annotation
   exempts. The kernel just renders the judgment. *\


(define r31-fix
  Canonical ->
    (cn "Set the field to its canonical form ("
      (cn Canonical
        "), or set spec.managementPolicies to omit Update so the provider stops fighting the cloud.")))

(define r31-violation-to-judgment
  \* origin-resource: a concrete resource value — high confidence → error. *\
  [r31-violation Group Kind Name _ FieldPath Value Canonical Reason origin-resource Src] ->
    (make-error "XPC.M.forprovider-canonical-form"
      Src
      (cn Kind (cn "/" (cn Name (cn ": non-canonical forProvider field " FieldPath))))
      (cn "The field " (cn FieldPath
        (cn " on " (cn Kind
          (cn " (group: " (cn Group
            (cn ") is set to " (cn Value
              (cn ", which the provider canonicalizes on read-back, so desired never equals observed. upjet re-issues the external Update on every reconcile and the status write conflicts with the poll loop — a reconcile storm. " Reason)))))))))
      (r31-fix Canonical)
      [])
  \* origin-template: an unrendered Composition template scan (Tier-2) —
     heuristic → warn. Name is the Composition; Value is the offending RHS. *\
  [r31-violation Group Kind Name _ FieldPath Value Canonical Reason origin-template Src] ->
    (make-warning "XPC.M.forprovider-canonical-form"
      Src
      (cn "Composition " (cn Name (cn ": non-canonical " (cn FieldPath " in template"))))
      (cn "Composition " (cn Name
        (cn " assigns " (cn FieldPath
          (cn " (" (cn Kind
            (cn ") to a hardcoded ARN literal " (cn Value
              (cn ", which the provider canonicalizes on read-back. The rendered resource is not in scope (the composite XR is synthesized at runtime), so this is a static template scan: verify the value resolves the versioned form from the observed resource. " Reason)))))))))
      (r31-fix Canonical)
      [])
  _ -> [])


\* check-r31 — top-level R31 check. Go pre-filters to actionable violations. *\
(define check-r31
  Violations -> (map (/. V (r31-violation-to-judgment V)) Violations))
