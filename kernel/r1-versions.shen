\* r1-versions.shen — Rule R1: served-and-storage version coherence

   R1a: Every version referenced must be served.
   R1b: Exactly one version must be marked storage (or referenceable for XRDs). *\

\* Check a single CRD for version coherence *\
(define check-r1-crd
  {(list A) --> (list judgment)}
  [crd-fact Group Kind Scope Versions Conversion Src] ->
    (append (check-r1a-versions Group Kind Versions Src)
            (check-r1b-storage Group Kind Versions Src))
  _ -> [])

\* R1a: find versions that are not served *\
(define check-r1a-versions
  {string --> string --> (list (list A)) --> source-loc --> (list judgment)}
  Group Kind Versions Src ->
    (let Unserved (filter (/. V (not (version-served? V))) Versions)
      (map (/. V (make-r1a-error Group Kind V Src)) Unserved)))

(define version-served?
  {(list A) --> boolean}
  [_ true | _] -> true
  _ -> false)

(define make-r1a-error
  {string --> string --> (list A) --> source-loc --> judgment}
  Group Kind [Name _ _ _] Src ->
    (make-error "XPC001"
      Src
      (cn "version " (cn Name (cn " of CRD " (cn Group (cn "/" (cn Kind " is not served"))))))
      (cn "CRD " (cn Group (cn "." (cn Kind (cn " declares version " (cn Name " but it is not marked as served. Clients cannot use this version."))))))
      (cn "Set served: true for version " (cn Name " or remove the version entry."))
      [])
  _ _ _ _ -> (make-error "XPC001" [source "" 0] "version check failed" "" "" []))

\* R1b: count storage versions, must be exactly 1 *\
(define check-r1b-storage
  {string --> string --> (list (list A)) --> source-loc --> (list judgment)}
  Group Kind Versions Src ->
    (let StorageCount (count-if (/. V (version-storage? V)) Versions)
      (if (= StorageCount 1)
          []
          [(make-error "XPC001"
            Src
            (cn "CRD " (cn Group (cn "/" (cn Kind (cn " has " (cn (str StorageCount) " storage versions (expected exactly 1)"))))))
            (cn "Every CRD must have exactly one version marked as the storage version. "
                (cn "Found " (cn (str StorageCount) " storage versions.")))
            "Mark exactly one version with storage: true."
            [])])))

(define version-storage?
  {(list A) --> boolean}
  [_ _ true | _] -> true
  _ -> false)

\* Check a single XRD for version coherence *\
(define check-r1-xrd
  {(list A) --> (list judgment)}
  [xrd-fact Group Kind Scope APIVer Versions Src _] ->
    (append (check-r1a-versions Group Kind Versions Src)
            (check-r1b-referenceable Group Kind Versions Src))
  _ -> [])

\* R1b for XRDs: exactly one version must be referenceable *\
(define check-r1b-referenceable
  {string --> string --> (list (list A)) --> source-loc --> (list judgment)}
  Group Kind Versions Src ->
    (let RefCount (count-if (/. V (version-referenceable? V)) Versions)
      (if (= RefCount 0)
          [(make-error "XPC001"
            Src
            (cn "XRD " (cn Group (cn "/" (cn Kind " has no referenceable version"))))
            "Every XRD must have at least one version marked as referenceable for Compositions to reference."
            "Set referenceable: true on the version Compositions should use."
            [])]
          [])))

(define version-referenceable?
  {(list A) --> boolean}
  [_ _ true | _] -> true
  _ -> false)

\* Top-level R1 check: iterate all CRDs and XRDs *\
(define check-r1
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  CRDs XRDs ->
    (append (flatten (map (/. C (check-r1-crd C)) CRDs))
            (flatten (map (/. X (check-r1-xrd X)) XRDs))))
