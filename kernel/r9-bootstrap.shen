\* r9-bootstrap.shen — Rule R9: required resources bootstrappable

   Compositions with pipeline steps that require external resources
   must ensure those resources are available on first reconcile. *\

\* Check a Composition's pipeline for required resource availability *\
(define check-r9-composition
  {(list A) --> (list (list A)) --> (list judgment)}
  [composition-fact CompName CTR _ Pipeline _ CompSrc] Resources ->
    (flatten (map (/. Step (check-r9-step CompName CompSrc Step Pipeline Resources))
                  Pipeline))
  _ _ -> [])

\* Check a single pipeline step for required resources.
   Steps may declare required resources via the input payload.
   The Go IR builder extracts these and annotates the step. *\
(define check-r9-step
  {string --> source-loc --> (list A) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  CompName CompSrc [StepName FnRef InputAV InputKind] Pipeline Resources ->
    \* Required resource checking is done via annotations set by the Go IR builder.
       The kernel checks for "xpc.dev/required-resource-missing" annotations. *\
    []
  _ _ _ _ _ -> [])

\* Check resources annotated with bootstrap gap information *\
(define check-r9-annotated
  {(list (list A)) --> (list judgment)}
  [] -> []
  [[resource-fact APIVersion Kind Name Namespace Annotations Src] | Rest] ->
    (let BootstrapIssues (check-r9-annotations Name Kind Annotations Src)
      (append BootstrapIssues (check-r9-annotated Rest)))
  [_ | Rest] -> (check-r9-annotated Rest))

(define check-r9-annotations
  {string --> string --> (list (list string)) --> source-loc --> (list judgment)}
  Name Kind Annotations Src ->
    (if (and (has-annotation? Annotations "xpc.dev/required-resource-missing" "true")
             (not (has-annotation? Annotations "xpc.dev/accept-bootstrap-gap" "true")))
        [(make-error "XPC009"
          Src
          (cn "required resource not bootstrappable for " (cn Name ""))
          (cn Kind (cn " \"" (cn Name
            "\" references a required resource that may not exist on first reconcile. The Composition pipeline depends on a resource that isn't produced by an earlier step or known to exist at bootstrap time.")))
          (cn "Ensure the required resource is produced by an earlier pipeline step, "
              "or add annotation xpc.dev/accept-bootstrap-gap: \"true\" to acknowledge.")
          [])]
        []))

\* Top-level R9 check *\
(define check-r9
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Compositions Resources ->
    (append
      (flatten (map (/. C (check-r9-composition C Resources)) Compositions))
      (check-r9-annotated Resources)))
