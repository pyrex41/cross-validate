\* r24-appset-finalizer-without-preserve.shen — Rule R24:
   XPC.E.appset-finalizer-without-preserve

   Emit XPC.E.appset-finalizer-without-preserve for every ApplicationSet whose
   template carries the ArgoCD cascading finalizer
     resources-finalizer.argocd.argoproj.io
   without the AppSet-level syncPolicy.preserveResourcesOnDeletion: true
   counterweight.

   Background: an ApplicationSet's spec.template.metadata.finalizers list is
   baked into every generated Application. ArgoCD interprets this finalizer as
   a "when this Application is deleted, cascade-delete every resource it owns"
   instruction. If the AppSet itself is also missing preserveResourcesOnDeletion,
   then removing an AppSet (or a single generated Application, if a generator
   stops emitting that parameter set) triggers cascading DELETE calls. When the
   owned resources are state-bearing Crossplane MRs defaulting to
   deletionPolicy: Delete, the cascade runs real `DROP DATABASE` /
   `DeleteCluster` calls. This is the root cause of fg-synapse INC-6.

   Fact shape (from pkg/checker/bridge.go appSetFinalizerToObj):
     [appset-finalizer-fact Name Finalizers PreserveSym Src]
   where Finalizers is a list of strings and PreserveSym is the lowercase-dashed
   symbol `preserve-yes` or `preserve-no` (NEVER a Shen boolean — uppercase
   would collide with Shen's pattern-variable convention).

   NOTE on `cn`: Shen's `cn` takes exactly 2 string arguments. Every `cn` call
   here is strictly 2-argument; longer concatenations are written as nested
   `(cn s1 (cn s2 s3))` chains. *\


\* r24-member? — plain list membership (specialized for strings). Returns
   true when S equals any element of Xs. *\
(define r24-member?
  _ [] -> false
  S [S | _] -> true
  S [_ | Rest] -> (r24-member? S Rest))


\* r24-has-cascade-finalizer? — true when Finalizers contains the ArgoCD
   cascading finalizer string. The alternative spelling
   `resources-finalizer.argocd.argoproj.io/foreground` also qualifies — it is
   the foreground-cascade variant with the same destructive effect. *\
(define r24-has-cascade-finalizer?
  Finalizers ->
    (if (r24-member? "resources-finalizer.argocd.argoproj.io" Finalizers)
        true
        (r24-member? "resources-finalizer.argocd.argoproj.io/foreground" Finalizers)))


\* r24-emit — build the XPC.E.appset-finalizer-without-preserve judgment for a
   single AppSet. Source points at the AppSet manifest — that is where the
   author has to edit either the finalizers list or the syncPolicy block. *\
(define r24-emit
  Name Src ->
    (make-error "XPC.E.appset-finalizer-without-preserve"
      Src
      (cn "ApplicationSet " (cn Name
        " bakes the ArgoCD cascading finalizer into its template without preserveResourcesOnDeletion"))
      (cn "spec.template.metadata.finalizers includes `resources-finalizer.argocd.argoproj.io`, "
        (cn "so every generated Application will cascade-delete its owned resources on deletion. "
          (cn "spec.syncPolicy.preserveResourcesOnDeletion is not set to true on this AppSet, "
            "so AppSet-level removal or a generator that stops producing a parameter set will trigger the cascade. This is the fg-synapse INC-6 failure mode.")))
      (cn "Set spec.syncPolicy.preserveResourcesOnDeletion: true on the ApplicationSet, "
        "OR drop the resources-finalizer.argocd.argoproj.io entry from spec.template.metadata.finalizers.")
      [])
  _ _ -> [])


\* r24-check-row — emit zero or one judgment for one appset-finalizer-fact.
   Fires only when the cascade finalizer is present AND preserve-on-deletion
   is off. Any malformed row is silently skipped (defensive, mirrors r22). *\
(define r24-check-row
  [appset-finalizer-fact Name Finalizers preserve-no Src] ->
    (if (r24-has-cascade-finalizer? Finalizers)
        [(r24-emit Name Src)]
        [])
  [appset-finalizer-fact _ _ preserve-yes _] -> []
  _ -> [])


\* check-r24 — top-level R24 check.
   AppSetFacts: list of appset-finalizer-fact tuples. *\
(define check-r24
  AppSetFacts ->
    (flatten (map (/. Row (r24-check-row Row)) AppSetFacts)))
