\* r11-api-deprecation.shen — Rule R11: API deprecation calendar (XPC011)

   Emit XPC011 for:
     (a) resources using a deprecated apiVersion
     (b) compositions whose compositeTypeRef group/version is deprecated
     (c) providers pinned to a package version older than a known floor
     (d) CRDs with versions that are no longer served

   Mirrors pkg/obligation/deprecation/api_calendar.go (now deleted). The
   hard-coded deprecation calendar there becomes a pair of Shen lookup
   functions below. *\


\* ===== Deprecation calendar =====
   deprecated-api? returns [Flag Message]: Flag is true when the
   apiVersion is deprecated. Message is the companion explanation.
   Both list values default to ["false" ""] for unknown versions. *\

(define deprecated-api?
  {string --> boolean}
  "apiextensions.crossplane.io/v1alpha1" -> true
  "pkg.crossplane.io/v1alpha1"            -> true
  "s3.aws.m.upbound.io/v1alpha1"          -> true
  "ec2.aws.m.upbound.io/v1alpha1"         -> true
  "rds.aws.m.upbound.io/v1alpha1"         -> true
  _ -> false)

(define deprecation-message
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


\* ===== Provider version floor table =====
   Each entry: [PackagePattern VersionFloor Message].
   If a provider's Package contains PackagePattern and its extracted
   version is strictly before VersionFloor, emit a warning. *\

(define provider-deprecations
  {(list (list string))}
  -> [["xpkg.crossplane.io/upbound/provider-aws" "v0.40.0"
       "provider-aws versions before v0.40.0 have known conversion webhook issues"]
      ["xpkg.crossplane.io/upbound/provider-family-aws" "v1.0.0"
       "provider-family-aws versions before v1.0.0 are pre-GA and may have breaking changes"]])


\* ===== Resource-level check ===== *\

(define check-r11-resource
  {(list A) --> (list judgment)}
  [resource-fact APIVersion Kind Name Namespace Annotations Src _] ->
    (if (deprecated-api? APIVersion)
        [(make-warning "XPC011"
          Src
          (cn "deprecated API version " (cn APIVersion (cn " for " (cn Kind (cn "/" Name)))))
          (deprecation-message APIVersion)
          "Update to the recommended API version."
          [])]
        [])
  _ -> [])


\* ===== Composition-level check (compositeTypeRef) ===== *\

(define check-r11-composition
  {(list A) --> (list judgment)}
  [composition-fact CompName [gvk Group Version Kind] Mode Pipeline Src] ->
    (let APIVer (cn Group (cn "/" Version))
      (if (deprecated-api? APIVer)
          [(make-warning "XPC011"
            Src
            (cn "Composition " (cn CompName (cn " references deprecated API version " APIVer)))
            (deprecation-message APIVer)
            "Update the compositeTypeRef to use a supported version."
            [])]
          []))
  _ -> [])


\* ===== Provider version comparison =====
   Extract a version suffix from "pkg...:vX.Y.Z" and compare with a
   floor. The format must be v<digits>.<digits>.<digits>. If either
   value does not parse, no judgment is emitted. *\

(define extract-provider-version
  {string --> string}
  Pkg -> (let Parts (split-string Pkg ":")
           (if (>= (length Parts) 2)
               (hd (xpc-reverse Parts))
               "")))

(define parse-digits-h
  {(list string) --> number --> number}
  [] Acc -> Acc
  [C | Rest] Acc -> (let N (string-digit C)
                      (if (= N -1)
                          Acc
                          (parse-digits-h Rest (+ (* Acc 10) N)))))

(define string-digit
  {string --> number}
  "0" -> 0  "1" -> 1  "2" -> 2  "3" -> 3  "4" -> 4
  "5" -> 5  "6" -> 6  "7" -> 7  "8" -> 8  "9" -> 9
  _ -> -1)

\* Return (list Major Minor Patch) for a "vX.Y.Z" string; empty if unparsable. *\
(define parse-semver
  {string --> (list number)}
  V -> (let Chars (explode V)
            Stripped (if (and (not (= Chars [])) (= (hd Chars) "v"))
                         (tl Chars)
                         Chars)
            Str (xpc-implode Stripped)
            Parts (split-string Str ".")
         (if (= (length Parts) 3)
             [(parse-digits-h (explode (hd Parts)) 0)
              (parse-digits-h (explode (hd (tl Parts))) 0)
              (parse-digits-h (explode (hd (tl (tl Parts)))) 0)]
             [])))

(define version-before?
  {string --> string --> boolean}
  A B -> (let PA (parse-semver A)
              PB (parse-semver B)
           (if (or (= PA []) (= PB []))
               false
               (version-before-h PA PB))))

(define version-before-h
  {(list number) --> (list number) --> boolean}
  [AM AN AP] [BM BN BP] ->
    (if (< AM BM) true
        (if (> AM BM) false
            (if (< AN BN) true
                (if (> AN BN) false
                    (< AP BP)))))
  _ _ -> false)


\* Walk provider-deprecation table against one provider. *\
(define check-r11-provider-entries
  {string --> string --> source-loc --> (list (list string)) --> (list judgment)}
  _ _ _ [] -> []
  ProvName Package Src [[Pattern Floor Msg] | Rest] ->
    (let PackageContains (string-contains? Package Pattern)
         ProvVer (extract-provider-version Package)
         Older (if (and PackageContains (not (= ProvVer "")))
                   (version-before? ProvVer Floor)
                   false)
      (if Older
          [(make-warning "XPC011"
            Src
            (cn "Provider " (cn ProvName (cn " at " (cn ProvVer " may have known issues"))))
            Msg
            (cn "Upgrade to version " (cn Floor " or later."))
            [])
           | (check-r11-provider-entries ProvName Package Src Rest)]
          (check-r11-provider-entries ProvName Package Src Rest))))

(define check-r11-provider
  {(list A) --> (list judgment)}
  [provider-fact ProvName Package Src] ->
    (check-r11-provider-entries ProvName Package Src (provider-deprecations))
  _ -> [])


\* ===== CRD unserved-version check ===== *\

(define check-r11-crd-versions
  {string --> string --> source-loc --> (list (list A)) --> (list judgment)}
  _ _ _ [] -> []
  Group Kind Src [[VName Served Storage SchemaRef] | Rest] ->
    (if Served
        (check-r11-crd-versions Group Kind Src Rest)
        [(make-warning "XPC011"
          Src
          (cn "CRD " (cn Group (cn "." (cn Kind (cn " version " (cn VName " is no longer served"))))))
          (cn "Version " (cn VName (cn " of CRD " (cn Group (cn "." (cn Kind
            ". Any resources still using this version will fail on next API server restart. This version will likely be removed in a future release."))))))
          (cn "Migrate resources from " (cn VName " to a served version."))
          []) | (check-r11-crd-versions Group Kind Src Rest)]))

(define check-r11-crd
  {(list A) --> (list judgment)}
  [crd-fact Group Kind Scope Versions Conversion Src] ->
    (check-r11-crd-versions Group Kind Src Versions)
  _ -> [])


\* ===== Top-level R11 check ===== *\

(define check-r11
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  Resources Compositions Providers CRDs ->
    (append
      (flatten (map (/. R (check-r11-resource R)) Resources))
      (append
        (flatten (map (/. C (check-r11-composition C)) Compositions))
        (append
          (flatten (map (/. P (check-r11-provider P)) Providers))
          (flatten (map (/. C (check-r11-crd C)) CRDs))))))
