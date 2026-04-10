\* r2-conversion.shen — Rule R2: conversion cost opt-in

   The motivating-bug rule. Any read/write path that crosses a Webhook
   conversion edge must be explicitly acknowledged with an annotation. *\

\* Check a single resource against all CRDs for webhook conversion *\
(define check-r2-resource
  {(list A) --> (list (list A)) --> (list judgment)}
  [resource-fact APIVersion Kind Name Namespace Annotations Src] CRDs ->
    (let Group (api-version->group APIVersion)
         Version (api-version->version APIVersion)
         MatchingCRDs (filter (/. C (crd-matches-gvk? C Group Kind)) CRDs)
      (flatten (map (/. C (check-r2-against-crd
                            Name Version Annotations Src C)) MatchingCRDs)))
  _ _ -> [])

\* Does this CRD match the given group and kind? *\
(define crd-matches-gvk?
  {(list A) --> string --> string --> boolean}
  [crd-fact Group Kind | _] Group Kind -> true
  _ _ _ -> false)

\* Check one resource against one CRD for webhook conversion *\
(define check-r2-against-crd
  {string --> string --> (list (list string)) --> source-loc --> (list A) --> (list judgment)}
  Name Version Annotations Src [crd-fact Group Kind Scope Versions [Strategy CostClass WebhookSvc] CrdSrc] ->
    (if (and (= CostClass webhook)
             (not (= Version (find-storage-version Versions))))
        (if (has-annotation? Annotations "xpc.dev/accept-conversion-webhook" "true")
            []
            [(make-error "XPC002"
              Src
              (cn "webhook conversion not acknowledged")
              (cn "This resource is written at version " (cn Version
                (cn ", but the storage version of CRD " (cn Group (cn "." (cn Kind
                  (cn " is " (cn (find-storage-version Versions)
                    ". Reading or writing this resource will invoke a conversion webhook on every request, which is a network round-trip and a single point of failure."))))))))
              (cn "Re-author the resource at the storage version " (cn (find-storage-version Versions)
                (cn ". Or add annotation xpc.dev/accept-conversion-webhook: \"true\" to acknowledge."
                    "")))
              [CrdSrc])])
        [])
  _ _ _ _ _ -> [])

\* Find the storage version name from a versions list *\
(define find-storage-version
  {(list (list A)) --> string}
  [] -> ""
  [[Name _ true | _] | _] -> Name
  [_ | Rest] -> (find-storage-version Rest))

\* Check if an annotation exists with a given value *\
(define has-annotation?
  {(list (list string)) --> string --> string --> boolean}
  [] _ _ -> false
  [[Key Val] | _] Key Val -> true
  [_ | Rest] Key Val -> (has-annotation? Rest Key Val))

\* Top-level R2 check *\
(define check-r2
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Resources CRDs ->
    (flatten (map (/. R (check-r2-resource R CRDs)) Resources)))
