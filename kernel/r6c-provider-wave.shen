\* r6c-provider-wave.shen — Rule R6c: Provider wave < MR wave

   Crossplane Providers must become Healthy before any of their managed
   resources (MRs) can reconcile. An MR is a resource whose Kind is
   served by a CRD (not an XRD) in the world.

   For each Argo Application, for each Provider sync-wave entry, and
   each MR sync-wave entry: assert wave(Provider) < wave(MR). The pair
   matching is conservative — we consider every Provider in the same
   Application eligible to serve every MR in that Application. Precise
   Provider→CRD mapping is a follow-up ticket. *\


(define check-r6c-app
  {(list A) --> (list (list A)) --> (list judgment)}
  [argo-app-fact AppName TrackingMode SyncWaves AppSrc] CRDs ->
    (let ProviderEntries (filter (/. SW (sync-wave-kind-matches? SW "Provider")) SyncWaves)
         MREntries (filter (/. SW (sync-wave-is-mr? SW CRDs)) SyncWaves)
      (flatten (map (/. P (check-r6c-for-provider P MREntries AppSrc)) ProviderEntries)))
  _ _ -> [])


(define check-r6c-for-provider
  {(list A) --> (list (list A)) --> source-loc --> (list judgment)}
  [_ ProvName ProvWave] MRs AppSrc ->
    (flatten (map (/. MR (check-r6c-pair ProvName ProvWave MR AppSrc)) MRs))
  _ _ _ -> [])


(define check-r6c-pair
  {string --> number --> (list A) --> source-loc --> (list judgment)}
  ProvName ProvWave [MRKind MRName MRWave] AppSrc ->
    (if (< ProvWave MRWave)
        []
        [(make-error "XPC006"
          AppSrc
          (cn "Provider " (cn ProvName (cn " (wave " (cn (str ProvWave)
            (cn ") must have a lower sync-wave than managed resource " (cn MRKind
              (cn "/" (cn MRName (cn " (wave " (cn (str MRWave) ")"))))))))))
          (cn "R6c: Provider " (cn ProvName
            (cn " must be Healthy before any of its managed resources can reconcile. "
                "The Provider sync-wave must be strictly less than the MR sync-wave.")))
          (cn "Set sync-wave on Provider " (cn ProvName (cn " to a value less than " (cn (str MRWave) "."))))
          [])])
  _ _ _ _ -> [])


\* sync-wave-is-mr? — true when the SyncWave's Kind matches a CRD (not XRD). *\
(define sync-wave-is-mr?
  {(list A) --> (list (list A)) --> boolean}
  [Kind _ _] CRDs ->
    (if (or (= Kind "CompositeResourceDefinition")
            (or (= Kind "Composition")
                (or (= Kind "Function")
                    (= Kind "Provider"))))
        false
        (kind-has-crd? Kind CRDs))
  _ _ -> false)


(define kind-has-crd?
  {string --> (list (list A)) --> boolean}
  _ [] -> false
  Kind [[crd-fact _ Kind | _] | _] -> true
  Kind [_ | Rest] -> (kind-has-crd? Kind Rest))


\* Top-level R6c check *\
(define check-r6c
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  ArgoApps CRDs ->
    (flatten (map (/. App (check-r6c-app App CRDs)) ArgoApps)))
