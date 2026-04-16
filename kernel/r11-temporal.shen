\* r11-temporal.shen — Rule R11: temporal validity

   Every type judgment carries an implicit "valid until" derived from upstream
   metadata: CRD deprecation timelines, provider package versions,
   function deprecation notices. *\

\* Known deprecated API versions.
   Returns the deprecation message, or "" if not deprecated. *\
(define known-deprecation-message
  {string --> string}
  "apiextensions.crossplane.io/v1alpha1" ->
    "apiextensions.crossplane.io/v1alpha1 is deprecated, use v1 or v2"
  "pkg.crossplane.io/v1alpha1" ->
    "pkg.crossplane.io/v1alpha1 is deprecated, use v1 or v1beta1"
  "s3.aws.m.upbound.io/v1alpha1" ->
    "v1alpha1 is deprecated for s3.aws.m.upbound.io, use v1beta1 or v1beta2"
  "ec2.aws.m.upbound.io/v1alpha1" ->
    "v1alpha1 is deprecated for ec2.aws.m.upbound.io, use v1beta1"
  "rds.aws.m.upbound.io/v1alpha1" ->
    "v1alpha1 is deprecated for rds.aws.m.upbound.io, use v1beta1"
  _ -> "")

\* Known provider version deprecations.
   Returns [message min-version] or [] if no deprecation. *\
(define provider-deprecation
  {string --> (list string)}
  Pkg ->
    (if (string-contains? Pkg "xpkg.crossplane.io/upbound/provider-aws")
        ["provider-aws versions before v0.40.0 have known conversion webhook issues" "v0.40.0"]
        (if (string-contains? Pkg "xpkg.crossplane.io/upbound/provider-family-aws")
            ["provider-family-aws versions before v1.0.0 are pre-GA and may have breaking changes" "v1.0.0"]
            [])))

\* Extract version from a package string like "xpkg.../foo:v1.2.3" *\
(define extract-pkg-version
  {string --> string}
  Pkg -> (extract-after-colon (explode Pkg) []))

(define extract-after-colon
  {(list string) --> (list string) --> string}
  [] Acc -> ""
  [":" | Rest] _ -> (implode Rest)
  [_ | Rest] Acc -> (extract-after-colon Rest Acc))

\* Parse a semver string "v1.2.3" into [Major Minor Patch].
   Returns [] on parse failure. *\
(define parse-semver
  {string --> (list number)}
  S -> (let Chars (explode S)
            NoV (if (= (hd Chars) "v") (tl Chars) Chars)
            Parts (split-string (implode NoV) ".")
         (if (= (length Parts) 3)
             (let A (string-to-num (hd Parts))
                  B (string-to-num (hd (tl Parts)))
                  C (string-to-num (hd (tl (tl Parts))))
               [A B C])
             [])))

\* Convert a numeric string to a number (basic: only non-negative integers). *\
(define string-to-num
  {string --> number}
  S -> (string-to-num-h (explode S) 0))

(define string-to-num-h
  {(list string) --> number --> number}
  [] Acc -> Acc
  [D | Rest] Acc -> (let N (digit-value D)
                      (if (>= N 0)
                          (string-to-num-h Rest (+ (* Acc 10) N))
                          Acc))
  _ Acc -> Acc)

(define digit-value
  {string --> number}
  "0" -> 0  "1" -> 1  "2" -> 2  "3" -> 3  "4" -> 4
  "5" -> 5  "6" -> 6  "7" -> 7  "8" -> 8  "9" -> 9
  _ -> -1)

\* Is version A before version B?  (semver comparison) *\
(define version-before?
  {string --> string --> boolean}
  A B -> (let PA (parse-semver A)
              PB (parse-semver B)
           (if (or (= PA []) (= PB []))
               false
               (semver-less? PA PB))))

(define semver-less?
  {(list number) --> (list number) --> boolean}
  [] [] -> false
  [A | _] [B | _] -> true  where (< A B)
  [A | _] [B | _] -> false where (> A B)
  [_ | RA] [_ | RB] -> (semver-less? RA RB)
  _ _ -> false)

\* ================================================================
   R11 checks
   ================================================================ *\

\* R11a: Check resources using deprecated API versions *\
(define check-r11-resource
  {(list A) --> (list judgment)}
  [resource-fact APIVersion Kind Name Namespace Annotations Src] ->
    (let Msg (known-deprecation-message APIVersion)
      (if (= Msg "")
          []
          [(make-warning "XPC011"
            Src
            (cn "deprecated API version " (cn APIVersion (cn " for " (cn Kind (cn "/" (cn Name ""))))))
            Msg
            "Update to the recommended API version."
            [])]))
  _ -> [])

\* R11b: Check compositions referencing deprecated API versions *\
(define check-r11-composition
  {(list A) --> (list judgment)}
  [composition-fact CompName [gvk Group Version Kind] _ _ _ CompSrc] ->
    (let APIVer (cn Group (cn "/" Version))
         Msg (known-deprecation-message APIVer)
      (if (= Msg "")
          []
          [(make-warning "XPC011"
            CompSrc
            (cn "Composition " (cn CompName (cn " references deprecated API version " (cn APIVer ""))))
            Msg
            "Update the compositeTypeRef to use a supported version."
            [])]))
  _ -> [])

\* R11c: Check providers for known deprecated versions *\
(define check-r11-provider
  {(list A) --> (list judgment)}
  [provider-fact ProvName Pkg Src] ->
    (let Dep (provider-deprecation Pkg)
      (if (= Dep [])
          []
          (let Msg (hd Dep)
               MinVer (hd (tl Dep))
               ProvVer (extract-pkg-version Pkg)
            (if (= ProvVer "")
                []
                (if (version-before? ProvVer MinVer)
                    [(make-warning "XPC011"
                      Src
                      (cn "Provider " (cn ProvName (cn " at " (cn ProvVer " may have known issues"))))
                      Msg
                      (cn "Upgrade to version " (cn MinVer " or later."))
                      [])]
                    [])))))
  _ -> [])

\* R11d: CRDs with versions that are no longer served *\
(define check-r11-crd
  {(list A) --> (list judgment)}
  [crd-fact Group Kind _ Versions _ Src] ->
    (let Unserved (filter (/. V (not (version-served? V))) Versions)
      (map (/. V (make-r11-unserved-warning Group Kind V Src)) Unserved))
  _ -> [])

(define make-r11-unserved-warning
  {string --> string --> (list A) --> source-loc --> judgment}
  Group Kind [VName | _] Src ->
    (make-warning "XPC011"
      Src
      (cn "CRD " (cn Group (cn "." (cn Kind (cn " version " (cn VName " is no longer served"))))))
      (cn "Version " (cn VName (cn " of CRD " (cn Group (cn "." (cn Kind
        (cn " is not served. Any resources still using this version will fail on next API server restart. "
            "This version will likely be removed in a future release.")))))))
      (cn "Migrate resources from " (cn VName " to a served version."))
      [])
  _ _ _ _ -> (make-warning "XPC011" [source "" 0] "unknown" "" "" []))

\* Top-level R11 check *\
(define check-r11
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  Resources Compositions Providers CRDs ->
    (append (flatten (map (/. R (check-r11-resource R)) Resources))
      (append (flatten (map (/. C (check-r11-composition C)) Compositions))
        (append (flatten (map (/. P (check-r11-provider P)) Providers))
                (flatten (map (/. C (check-r11-crd C)) CRDs))))))
