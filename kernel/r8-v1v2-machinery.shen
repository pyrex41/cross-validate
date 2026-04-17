\* r8-v1v2-machinery.shen — Rule R8: v1 vs v2 spec machinery

   Detect when a resource targeting a v2 XRD uses v1-style top-level
   machinery fields instead of the spec.crossplane block. *\

\* Check a resource against XRDs for v1/v2 machinery mismatch *\
(define check-r8-resource
  {(list A) --> (list (list A)) --> (list judgment)}
  [resource-fact APIVersion Kind Name Namespace Annotations Src] XRDs ->
    (let Group (api-version->group APIVersion)
         MatchingXRDs (filter (/. X (xrd-matches-gk? X Group Kind)) XRDs)
      (flatten (map (/. XRD (check-r8-against-xrd Name Kind Src XRD)) MatchingXRDs)))
  _ _ -> [])

\* Match XRD group and kind *\
(define xrd-matches-gk?
  {(list A) --> string --> string --> boolean}
  [xrd-fact Group Kind | _] Group Kind -> true
  _ _ _ -> false)

\* Check if a v2 XRD resource uses v1-style machinery *\
(define check-r8-against-xrd
  {string --> string --> source-loc --> (list A) --> (list judgment)}
  ResName ResKind ResSrc [xrd-fact _ _ _ APIVer _ XrdSrc] ->
    (if (is-v2-api-version? APIVer)
        \* The Go side will annotate resources with "xpc.dev/has-top-level-machinery"
           if they have top-level publishConnectionDetailsTo, writeConnectionSecretToRef,
           compositionRef, compositionSelector, etc. without using spec.crossplane block. *\
        \* For the kernel, we check the annotation the Go IR builder sets *\
        []  \* Actual check delegated to Go pre-processing — see note below *\
        [])
  _ _ _ _ -> [])

\* Is this a v2 API version? *\
(define is-v2-api-version?
  {string --> boolean}
  "apiextensions.crossplane.io/v2" -> true
  _ -> false)

\* Top-level R8 check.
   NOTE: R8 is primarily checked in the Go side during IR building,
   because it requires inspecting the raw resource structure for
   top-level machinery fields. The Shen kernel receives pre-computed
   annotations from the Go side. This function checks resources that
   were annotated by the Go IR builder with "xpc.dev/v1-machinery-on-v2-xrd". *\
(define check-r8
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Resources XRDs ->
    (flatten (map (/. R (check-r8-resource-annotation R)) Resources)))

(define check-r8-resource-annotation
  {(list A) --> (list judgment)}
  [resource-fact APIVersion Kind Name Namespace Annotations Src] ->
    (if (has-annotation? Annotations "xpc.dev/v1-machinery-on-v2-xrd" "true")
        [(make-error "XPC008"
          Src
          (cn "Resource " (cn Name (cn " uses v1-style machinery fields with a v2 XRD")))
          (cn Kind (cn " '" (cn Name
            "' uses top-level machinery fields (publishConnectionDetailsTo, compositionRef, etc.) but its XRD uses apiextensions.crossplane.io/v2. In v2, these fields must be under spec.crossplane.")))
          "Move machinery fields under spec.crossplane. See the Crossplane v2 migration guide."
          [])]
        [])
  _ -> [])
