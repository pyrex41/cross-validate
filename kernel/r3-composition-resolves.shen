\* r3-composition-resolves.shen — Rule R3: Composition type references resolve

   R3a: compositeTypeRef must resolve to an existing XRD.
   R3b: The referenced version must be referenceable on that XRD. *\

\* Check a single Composition's compositeTypeRef against all XRDs *\
(define check-r3-composition
  {(list A) --> (list (list A)) --> (list judgment)}
  [composition-fact Name [gvk Group Version Kind] Mode Pipeline Src _] XRDs ->
    (let Matching (filter (/. X (xrd-matches-gk? X Group Kind)) XRDs)
      (if (= Matching [])
          \* R3a: no XRD found *\
          [(make-error "XPC003"
            Src
            (cn "Composition " (cn Name (cn " references unknown XRD " (cn Group (cn "/" (cn Kind ""))))))
            (cn "compositeTypeRef references " (cn Group (cn "/" (cn Version (cn "/" (cn Kind
              " but no CompositeResourceDefinition for this group/kind was found."))))))
            "Ensure the XRD is defined and included in the checked manifests."
            [])]
          \* R3b: check version is referenceable *\
          (flatten (map (/. X (check-r3b-version Name Group Version Kind Src X)) Matching))))
  _ _ -> [])

\* Does this XRD match the given group and kind? *\
(define xrd-matches-gk?
  {(list A) --> string --> string --> boolean}
  [xrd-fact Group Kind | _] Group Kind -> true
  _ _ _ -> false)

\* R3b: check that the version used by the Composition is referenceable on the XRD *\
(define check-r3b-version
  {string --> string --> string --> string --> source-loc --> (list A) --> (list judgment)}
  CompName Group Version Kind CompSrc [xrd-fact _ _ _ _ Versions XrdSrc _] ->
    (let RefVersions (filter (/. V (version-referenceable? V)) Versions)
         HasVersion (member-version? Version RefVersions)
      (if HasVersion
          []
          [(make-error "XPC003"
            CompSrc
            (cn "Composition " (cn CompName (cn " uses version " (cn Version
              (cn " which is not referenceable on XRD " (cn Group (cn "/" (cn Kind ""))))))))
            (cn "The Composition references " (cn Group (cn "/" (cn Version (cn "/" (cn Kind
              (cn " but this version is not marked referenceable on the XRD. "
                  "Only referenceable versions can be used by Compositions.")))))))
            (cn "Use a referenceable version, or set referenceable: true on version " (cn Version " in the XRD."))
            [XrdSrc])]))
  _ _ _ _ _ _ -> [])

\* Check if a version name appears in a versions list *\
(define member-version?
  {string --> (list (list A)) --> boolean}
  _ [] -> false
  V [[V | _] | _] -> true
  V [_ | Rest] -> (member-version? V Rest))

\* Top-level R3 check *\
(define check-r3
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Compositions XRDs ->
    (flatten (map (/. C (check-r3-composition C XRDs)) Compositions)))
