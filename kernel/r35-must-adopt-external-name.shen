\* r35-must-adopt-external-name.shen — Rule R35: XPC.I.must-adopt-external-name

   Category I (Provider-capability), Tier-1 (resource walk, definite). Emit
   XPC.I.must-adopt-external-name for every concrete managed resource of a
   registered must-adopt kind (ExternalNameAdoptRegistry) that lacks a non-empty
   crossplane.io/external-name annotation and is not bypassed.

   The seed case is provider-signoz Alert: its provider Create path is broken
   against the custom signoz build (returns an HTML error page → the SDK fails
   with "invalid character '<'"), so the Alert never reconciles unless it ADOPTS
   the existing external object (created out-of-band via the SigNoz API) by
   carrying crossplane.io/external-name: <id>. Observe/update work; only Create is
   broken (fg-manifold commit abd5aa10ed / INC-8).

   The Go bridge precomputes one r35-violation per offending resource. The kernel
   renders the judgment at ERROR severity: the failure is definite and
   registry-confirmed — the provider WILL fail or duplicate Create for this kind —
   so it is not softened to warn. *\


(define r35-violation-to-judgment
  [r35-violation Group Kind Name Namespace Reason Src] ->
    (make-error "XPC.I.must-adopt-external-name"
      Src
      (cn Kind (cn " " (cn Name " must adopt its existing external object via crossplane.io/external-name")))
      (cn Kind (cn " " (cn Name (cn " is a kind whose provider Create path is broken or non-idempotent, but it has no crossplane.io/external-name annotation, so Crossplane will attempt a Create that fails (or duplicates a singleton) and the resource never reconciles cleanly. " Reason))))
      "Adopt the existing external object: add the annotation crossplane.io/external-name: <external-id> so the provider observes/updates instead of creating. If a fresh object really should be created (e.g. the provider Create path is fixed), set xpc.io/allow-missing-external-name: true and retire the registry row."
      [])
  _ -> [])


\* check-r35 — top-level R35 check. Go pre-filters to resources missing the
   external-name annotation. *\
(define check-r35
  Violations -> (map (/. V (r35-violation-to-judgment V)) Violations))
