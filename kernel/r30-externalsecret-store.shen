\* r30-externalsecret-store.shen — Rule D5:
   XPC.K.externalsecret-store

   Emit XPC.K.externalsecret-store for every ExternalSecret whose
   spec.secretStoreRef.name is not in the configured allowlist
   (external-secret-stores.allowed-names in xpc.yaml).

   Background: an ExternalSecret binds to a (Cluster)SecretStore by name. A
   typo'd or wrong store name names a store that does not exist, so the
   external-secrets operator fails to sync with SecretSyncedError — the target
   Secret is never created and every workload mounting it fails. In fg-manifold
   the legitimate stores are aws-secrets-manager-{cluster,preview,prod}; this
   rule catches a reference to anything else.

   Opt-in: the Go bridge (buildESOStoreViolations) returns no facts when the
   allowlist is empty, so the rule is silent until configured. When configured,
   the bridge emits one fact per ExternalSecret whose store name is not allowed;
   this rule maps each to a judgment — the R15/R16 precomputed-violation
   pattern.

   Fact shape (from pkg/checker/bridge.go esoStoreViolationToObj):
     [eso-store-violation Name Namespace StoreName Src]

   NOTE on `cn`: Shen's `cn` takes exactly 2 string arguments; longer
   concatenations are nested (cn s1 (cn s2 s3)) chains. *\


(define r30-emit
  Name Namespace StoreName Src ->
    (make-error "XPC.K.externalsecret-store"
      Src
      (cn "ExternalSecret " (cn Name
        (cn " references secretStoreRef.name '" (cn StoreName
          "' which is not an allowed secret store"))))
      (cn "spec.secretStoreRef.name is '" (cn StoreName
        "' but it is not in the configured allowlist (external-secret-stores.allowed-names). If the store does not exist, external-secrets fails to sync with SecretSyncedError and the target Secret is never created."))
      "Reference an allowed (Cluster)SecretStore, or add this name to external-secret-stores.allowed-names in xpc.yaml if it is legitimate."
      []))


\* r30-check-row — map one violation fact to a singleton judgment list. *\
(define r30-check-row
  [eso-store-violation Name Namespace StoreName Src] ->
    [(r30-emit Name Namespace StoreName Src)]
  _ -> [])


\* check-r30 — top-level D5 check. Violations: precomputed ExternalSecret
   store-name violations from the Go bridge. *\
(define check-r30
  Violations ->
    (flatten (map (/. Row (r30-check-row Row)) Violations)))
