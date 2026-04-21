\* r6-wave-ordering.shen — Rule R6: Argo sync-wave ordering constraints

   Crossplane has implicit ordering requirements:
   R6a: XRD must be established before any XR of its kind.
        wave(XRD) < wave(XR)
   R6b: Function must be healthy before Composition using it.
        wave(Function) < wave(Composition)
   R6c: Provider must be healthy before its MRDs are usable.
        wave(Provider) < wave(MR)
   R6d: Composition must exist before XR of its referenced type.
        wave(Composition) <= wave(XR) *\

\* Per-fact OwningApp filters. Each fact carries OwningApp in its trailing
   position (see prelude). Filtering by AppName before the cartesian map
   keeps R6a/b/d from blaming one app's XRDs/Compositions/Functions against
   every other app's sync waves — the XPC006 analogue of R15's cartesian
   fix. Unowned facts (OwningApp = "") are ignored by per-app filtering;
   they're conceptually shared/global and don't belong to any one app's
   sync-wave ordering. *\
(define xrd-owned-by?
  AppName [xrd-fact _ _ _ _ _ _ OwningApp] -> (= AppName OwningApp)
  _ _ -> false)

(define composition-owned-by?
  AppName [composition-fact _ _ _ _ _ OwningApp] -> (= AppName OwningApp)
  _ _ -> false)

(define function-owned-by?
  AppName [function-fact _ _ _ _ OwningApp] -> (= AppName OwningApp)
  _ _ -> false)


\* Check wave ordering for an Argo Application *\
(define check-r6-app
  {(list A) --> (list (list A)) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  [argo-app-fact AppName TrackingMode SyncWaves AppSrc] Compositions XRDs Functions ->
    (let OwnedComps (filter (/. C (composition-owned-by? AppName C)) Compositions)
         OwnedXRDs  (filter (/. X (xrd-owned-by? AppName X)) XRDs)
         OwnedFns   (filter (/. F (function-owned-by? AppName F)) Functions)
      (append
        (check-r6a-xrd-before-xr SyncWaves OwnedXRDs AppSrc)
        (append
          (check-r6b-fn-before-composition SyncWaves OwnedComps OwnedFns AppSrc)
          (check-r6d-composition-before-xr SyncWaves OwnedComps OwnedXRDs AppSrc))))
  _ _ _ _ -> [])

\* R6a: XRD wave < XR wave *\
(define check-r6a-xrd-before-xr
  {(list (list A)) --> (list (list A)) --> source-loc --> (list judgment)}
  SyncWaves XRDs AppSrc ->
    (flatten (map (/. XRD (check-r6a-for-xrd XRD SyncWaves AppSrc)) XRDs)))

(define check-r6a-for-xrd
  {(list A) --> (list (list A)) --> source-loc --> (list judgment)}
  [xrd-fact Group Kind _ _ _ XrdSrc _] SyncWaves AppSrc ->
    (let XrdWave (find-wave "CompositeResourceDefinition" Kind SyncWaves)
         \* Find all XRs that match this XRD's kind *\
         XrEntries (filter (/. SW (sync-wave-kind-matches? SW Kind)) SyncWaves)
      (flatten (map (/. XrEntry
                      (let XrWave (get-sync-wave XrEntry)
                           XrName (get-sync-name XrEntry)
                        (if (< XrdWave XrWave)
                            []
                            [(make-error "XPC006"
                              AppSrc
                              (cn "XRD " (cn Kind (cn " (wave " (cn (str XrdWave)
                                (cn ") must have a lower sync-wave than XR " (cn XrName
                                  (cn " (wave " (cn (str XrWave) ")"))))))))
                              (cn "CompositeResourceDefinition " (cn Kind
                                (cn " must be Established before any XR of this kind can be applied. "
                                    "The XRD sync-wave must be strictly less than the XR sync-wave.")))
                              (cn "Set sync-wave on the XRD to a value less than " (cn (str XrWave) "."))
                              [XrdSrc])])))
                    XrEntries)))
  _ _ _ -> [])

\* R6b: Function wave < Composition wave *\
(define check-r6b-fn-before-composition
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> source-loc --> (list judgment)}
  SyncWaves Compositions Functions AppSrc ->
    (flatten (map (/. Comp (check-r6b-for-composition Comp SyncWaves Functions AppSrc))
                  Compositions)))

(define check-r6b-for-composition
  {(list A) --> (list (list A)) --> (list (list A)) --> source-loc --> (list judgment)}
  [composition-fact CompName _ _ Pipeline CompSrc _] SyncWaves Functions AppSrc ->
    (let CompWave (find-wave "Composition" CompName SyncWaves)
         FnRefs (extract-fn-refs Pipeline)
      (flatten (map (/. FnRef
                      (let FnWave (find-wave "Function" FnRef SyncWaves)
                        (if (< FnWave CompWave)
                            []
                            [(make-error "XPC006"
                              AppSrc
                              (cn "Function " (cn FnRef (cn " (wave " (cn (str FnWave)
                                (cn ") must have a lower sync-wave than Composition " (cn CompName
                                  (cn " (wave " (cn (str CompWave) ")"))))))))
                              (cn "Function " (cn FnRef
                                (cn " must be Healthy before Composition " (cn CompName
                                  " can use it. The Function sync-wave must be strictly less than the Composition sync-wave."))))
                              (cn "Set sync-wave on Function " (cn FnRef (cn " to a value less than " (cn (str CompWave) "."))))
                              [CompSrc])])))
                    FnRefs)))
  _ _ _ _ -> [])

\* R6d: Composition wave <= XR wave *\
(define check-r6d-composition-before-xr
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> source-loc --> (list judgment)}
  SyncWaves Compositions XRDs AppSrc ->
    (flatten (map (/. Comp (check-r6d-for-composition Comp SyncWaves AppSrc))
                  Compositions)))

(define check-r6d-for-composition
  {(list A) --> (list (list A)) --> source-loc --> (list judgment)}
  [composition-fact CompName [gvk Group Version Kind] _ _ CompSrc _] SyncWaves AppSrc ->
    (let CompWave (find-wave "Composition" CompName SyncWaves)
         XrEntries (filter (/. SW (sync-wave-kind-matches? SW Kind)) SyncWaves)
      (flatten (map (/. XrEntry
                      (let XrWave (get-sync-wave XrEntry)
                           XrName (get-sync-name XrEntry)
                        (if (<= CompWave XrWave)
                            []
                            [(make-error "XPC006"
                              AppSrc
                              (cn "Composition " (cn CompName (cn " (wave " (cn (str CompWave)
                                (cn ") must not have a higher sync-wave than XR " (cn XrName
                                  (cn " (wave " (cn (str XrWave) ")"))))))))
                              (cn "Composition " (cn CompName
                                (cn " must exist before XR " (cn XrName
                                  " of its referenced type can be applied."))))
                              (cn "Set sync-wave on Composition " (cn CompName
                                (cn " to a value <= " (cn (str XrWave) "."))))
                              [CompSrc])])))
                    XrEntries)))
  _ _ _ -> [])

\* Helper: find the wave for a given kind/name in sync waves *\
(define find-wave
  {string --> string --> (list (list A)) --> number}
  _ _ [] -> 0
  Kind Name [[Kind Name Wave] | _] -> Wave
  Kind Name [_ | Rest] -> (find-wave Kind Name Rest))

\* Helper: does this sync wave entry match a kind? *\
(define sync-wave-kind-matches?
  {(list A) --> string --> boolean}
  [Kind _ _] Kind -> true
  _ _ -> false)

\* Helper: get the wave from a sync wave entry *\
(define get-sync-wave
  {(list A) --> number}
  [_ _ Wave] -> Wave
  _ -> 0)

\* Helper: get the name from a sync wave entry *\
(define get-sync-name
  {(list A) --> string}
  [_ Name _] -> Name
  _ -> "")

\* Helper: extract function refs from pipeline steps *\
(define extract-fn-refs
  {(list (list A)) --> (list string)}
  [] -> []
  [[_ FnRef | _] | Rest] -> [FnRef | (extract-fn-refs Rest)]
  [_ | Rest] -> (extract-fn-refs Rest))

\* Top-level R6 check *\
(define check-r6
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  ArgoApps Compositions XRDs Functions ->
    (flatten (map (/. App (check-r6-app App Compositions XRDs Functions)) ArgoApps)))
