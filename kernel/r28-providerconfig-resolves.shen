\* r28-providerconfig-resolves.shen — Rule D1:
   XPC.B.providerconfig-resolves

   Emit XPC.B.providerconfig-resolves for every resource whose
   spec.providerConfigRef.name does not resolve to a declared ProviderConfig
   (or ClusterProviderConfig), nor to an allowed-provider-configs entry.

   Background: a Crossplane managed resource references its credentials/account
   binding by name via spec.providerConfigRef.name. A typo (e.g. "ops-accout")
   names nothing — Crossplane cannot resolve the reference, the resource never
   reconciles, and the deploy looks healthy while the object is silently never
   created. In fg-manifold ~1064 resources carry a providerConfigRef across ~11
   distinct names (prod-account, ops-account, aurora-prod-mysql, …); this rule
   catches a misspelling before it reaches the cluster.

   The Go bridge (buildProviderConfigRefViolations) does the set-membership
   join: it computes the declared-name set and emits one
   providerconfig-ref-violation fact per UNRESOLVED reference only. This rule
   maps each precomputed violation to a judgment — the R15/R16 pattern.

   Fact shape (from pkg/checker/bridge.go providerConfigRefViolationToObj):
     [providerconfig-ref-violation Group Kind Name Namespace RefName Src]

   NOTE on `cn`: Shen's `cn` takes exactly 2 string arguments; longer
   concatenations are nested (cn s1 (cn s2 s3)) chains. *\


(define r28-emit
  Group Kind Name RefName Src ->
    (make-error "XPC.B.providerconfig-resolves"
      Src
      (cn Kind (cn "/" (cn Name
        (cn " references providerConfigRef.name '" (cn RefName
          "' which resolves to no declared ProviderConfig")))))
      (cn "spec.providerConfigRef.name is '" (cn RefName
        (cn "' but no ProviderConfig or ClusterProviderConfig with that name is declared in the checked set. "
          "Crossplane cannot resolve the reference, so the resource never reconciles — the deploy appears healthy while the external object is silently never created.")))
      (cn "Fix the providerConfigRef.name to match a declared ProviderConfig. "
        "If the ProviderConfig is created out-of-band (e.g. a separate bootstrap app), add its name to allowed-provider-configs in xpc.yaml.")
      []))


\* r28-check-row — map one violation fact to a singleton judgment list. *\
(define r28-check-row
  [providerconfig-ref-violation Group Kind Name Ns RefName Src] ->
    [(r28-emit Group Kind Name RefName Src)]
  _ -> [])


\* check-r28 — top-level D1 check. Violations: list of precomputed, unresolved
   providerconfig-ref-violation facts from the Go bridge. *\
(define check-r28
  Violations ->
    (flatten (map (/. Row (r28-check-row Row)) Violations)))
