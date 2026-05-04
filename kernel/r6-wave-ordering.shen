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


\* Check wave ordering for an Argo Application.

   R6a and R6d compare against XRs that live in this App's own SyncWaves, so
   they remain per-App. R6b is evaluated globally (see check-r6b-global) to
   honour Option B scoping: same-App Function/Composition pairs are deployed
   atomically by Argo, so flagging them produces ~30 false positives on
   fg-manifold. R6b only fires on cross-App pairs where AppSet ordering
   matters. *\
(define check-r6-app
  {(list A) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  [argo-app-fact AppName TrackingMode SyncWaves AppSrc] Compositions XRDs ->
    (let OwnedComps (filter (/. C (composition-owned-by? AppName C)) Compositions)
         OwnedXRDs  (filter (/. X (xrd-owned-by? AppName X)) XRDs)
      (append
        (check-r6a-xrd-before-xr SyncWaves OwnedXRDs AppSrc)
        (check-r6d-composition-before-xr SyncWaves OwnedComps OwnedXRDs AppSrc)))
  _ _ _ -> [])

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

\* R6b: Function wave < Composition wave (cross-App only).

   Option B scoping: skip Function/Composition pairs that share the same
   OwningApp. Within one Argo Application, Argo applies same-wave resources
   atomically and Crossplane reconciles eventually, so default-0 vs default-0
   pairs are not real ordering hazards. Cross-App pairs still need ordering
   because each AppSet syncs as its own transaction.

   For the surviving cross-App pairs, the Composition's wave is looked up in
   its own App's SyncWaves and the Function's wave in its own App's
   SyncWaves. *\
(define check-r6b-global
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  ArgoApps Compositions Functions ->
    (flatten (map (/. Comp (check-r6b-for-composition Comp ArgoApps Functions))
                  Compositions)))

(define check-r6b-for-composition
  {(list A) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  [composition-fact CompName _ _ Pipeline CompSrc CompApp] ArgoApps Functions ->
    (let CompWaves (app-sync-waves CompApp ArgoApps)
         CompSrcLoc (app-source CompApp ArgoApps)
         CompWave (find-wave "Composition" CompName CompWaves)
         FnRefs (extract-fn-refs Pipeline)
      (flatten (map (/. FnRef
                      (check-r6b-pair CompName CompApp CompWave CompSrc CompSrcLoc
                                      FnRef ArgoApps Functions))
                    FnRefs)))
  _ _ _ -> [])

(define check-r6b-pair
  {string --> string --> number --> source-loc --> source-loc --> string --> (list (list A)) --> (list (list A)) --> (list judgment)}
  CompName CompApp CompWave CompSrc CompSrcLoc FnRef ArgoApps Functions ->
    (let FnApp (function-app FnRef Functions)
         FnWaves (app-sync-waves FnApp ArgoApps)
         FnWave (find-wave "Function" FnRef FnWaves)
      (if (= FnApp CompApp)
          []
          (if (= FnApp "")
              []
              (if (< FnWave CompWave)
                  []
                  [(check-r6b-emit CompName CompApp CompWave CompSrc CompSrcLoc
                                   FnRef FnApp FnWave)])))))

(define check-r6b-emit
  {string --> string --> number --> source-loc --> source-loc --> string --> string --> number --> judgment}
  CompName CompApp CompWave CompSrc CompSrcLoc FnRef FnApp FnWave ->
    (make-error "XPC006"
      CompSrcLoc
      (r6b-msg FnRef FnApp FnWave CompName CompApp CompWave)
      (r6b-detail FnRef CompName)
      (r6b-fix FnRef CompWave)
      [CompSrc]))

(define r6b-msg
  {string --> string --> number --> string --> string --> number --> string}
  FnRef FnApp FnWave CompName CompApp CompWave ->
    (cn "Function "
      (cn FnRef
        (cn " in App "
          (cn FnApp
            (cn " (wave "
              (cn (str FnWave)
                (cn ") must have a lower sync-wave than Composition "
                  (cn CompName
                    (cn " in App "
                      (cn CompApp
                        (cn " (wave "
                          (cn (str CompWave) ")")))))))))))))

(define r6b-detail
  {string --> string --> string}
  FnRef CompName ->
    (cn "Function "
      (cn FnRef
        (cn " must be Healthy before Composition "
          (cn CompName
            " can use it. The Function sync-wave must be strictly less than the Composition sync-wave.")))))

(define r6b-fix
  {string --> number --> string}
  FnRef CompWave ->
    (cn "Set sync-wave on Function "
      (cn FnRef
        (cn " to a value less than "
          (cn (str CompWave) ".")))))

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

\* Top-level R6 check.
   R6a/R6d run per-Argo-Application against same-App XRDs/Compositions and
   that App's SyncWaves. R6b runs globally and only fires on cross-App
   Function/Composition pairs (Option B). When all of Compositions, XRDs,
   and Functions are empty, every result is [] so we skip the dispatch. *\
(define check-r6
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  _ [] [] [] -> []
  ArgoApps Compositions XRDs Functions ->
    (append
      (flatten (map (/. App (check-r6-app App Compositions XRDs)) ArgoApps))
      (check-r6b-global ArgoApps Compositions Functions)))

\* Helper: look up an Argo Application's SyncWaves by Name. Returns [] when
   the App is not found (or AppName is empty), which makes find-wave default
   to 0 — the same behaviour as the unannotated case. *\
(define app-sync-waves
  {string --> (list (list A)) --> (list (list A))}
  _ [] -> []
  AppName [[argo-app-fact AppName _ SyncWaves _] | _] -> SyncWaves
  AppName [_ | Rest] -> (app-sync-waves AppName Rest))

\* Helper: look up an Argo Application's source-loc by Name. Returns a
   placeholder source-loc when not found — used as the diagnostic anchor for
   cross-App R6b emissions, which logically belong to the Composition's App. *\
(define app-source
  {string --> (list (list A)) --> source-loc}
  _ [] -> [source "" 0]
  AppName [[argo-app-fact AppName _ _ AppSrc] | _] -> AppSrc
  AppName [_ | Rest] -> (app-source AppName Rest))

\* Helper: look up a Function's OwningApp by Name from the Functions fact
   list. Returns "" when not found, which check-r6b-pair treats as
   unowned/cross-App-irrelevant. *\
(define function-app
  {string --> (list (list A)) --> string}
  _ [] -> ""
  FnName [[function-fact FnName _ _ _ OwningApp] | _] -> OwningApp
  FnName [_ | Rest] -> (function-app FnName Rest))
