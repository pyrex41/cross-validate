\* r18-helm-renders.shen — Rule R18: XPC.H.helm-renders

   Walks the (render-results ...) section produced by the Go bridge.
   Each entry is:

     (render-result AppName ChartPath Success ErrorKind Error
                    (values-issues (values-issue Path Msg) ...) Src)

   where Success is the symbol true or false, and ErrorKind is one of
   helm-absent / helm-template-failed / helm-timeout / other / none.
   All discriminator symbols are lowercase-dashed so Shen's pattern-match
   (which treats uppercase identifiers as variables) binds correctly.

   R18 only inspects the render outcome. It fires:
     - severity warning when Success=false and ErrorKind=helm-absent
       (users on a machine without helm still get a signal without a
       hard failure).
     - severity error  when Success=false and ErrorKind is anything else
       (real template/timeout failures are hard blockers).
     - nothing when Success=true. *\


\* r18-error-label — human-readable kind label. Covers helm + kustomize. *\
(define r18-error-label
  helm-absent            -> "helm binary absent"
  helm-template-failed   -> "helm template failed"
  helm-timeout           -> "helm template timed out"
  kustomize-absent       -> "kustomize binary absent"
  kustomize-build-failed -> "kustomize build failed"
  kustomize-timeout      -> "kustomize build timed out"
  other                  -> "render failed"
  none                   -> "render failed"
  K                      -> "render failed")


\* r18-fix-hint — remediation hint keyed off the error kind. *\
(define r18-fix-hint
  helm-absent            -> "Install helm or pass --helm-bin=<path> so xpc can render this Application's Helm sources."
  helm-template-failed   -> "Run 'helm template' locally on the chart to reproduce and fix the template error."
  helm-timeout           -> "The chart takes >30s to render. Simplify the chart or raise the timeout."
  kustomize-absent       -> "Install kustomize or put it on PATH so xpc can render this Application's Kustomize sources."
  kustomize-build-failed -> "Run 'kustomize build' locally on the overlay to reproduce and fix the build error."
  kustomize-timeout      -> "The overlay takes >30s to render. Simplify the overlay tree or raise the timeout."
  other                  -> "Inspect the render error and fix the chart or values."
  none                   -> "Inspect the render error and fix the chart or values."
  K                      -> "Inspect the render error and fix the chart or values.")


\* r18-is-kustomize-kind? — does this ErrorKind identify a Kustomize
   failure? Shen has no string-level introspection on symbols, so we pattern
   match the three known kinds directly. *\
(define r18-is-kustomize-kind?
  kustomize-absent       -> true
  kustomize-build-failed -> true
  kustomize-timeout      -> true
  _                      -> false)


\* r18-code-for-kind — pick the diagnostic code: helm-renders vs
   kustomize-renders. Using distinct codes lets the obligation ledger
   tick both generators independently. *\
(define r18-code-for-kind
  K -> (if (r18-is-kustomize-kind? K)
           "XPC.H.kustomize-renders"
           "XPC.H.helm-renders"))


\* r18-check-result — emit one judgment per failed render-result.
   The third arg is a lowercase-dashed discriminator symbol:
   render-ok or render-failed (Shen's literal true/false booleans would
   be interpreted specially, so we use plain symbols). *\
(define r18-check-result
  [render-result AppName ChartPath render-ok    ErrorKind Error Issues Src] -> []
  [render-result AppName ChartPath render-failed helm-absent Error Issues Src] ->
    [(make-warning "XPC.H.helm-renders"
       Src
       (cn AppName (cn ": " (r18-error-label helm-absent)))
       (cn ChartPath (cn ": " Error))
       (r18-fix-hint helm-absent)
       [])]
  [render-result AppName ChartPath render-failed kustomize-absent Error Issues Src] ->
    [(make-warning "XPC.H.kustomize-renders"
       Src
       (cn AppName (cn ": " (r18-error-label kustomize-absent)))
       (cn ChartPath (cn ": " Error))
       (r18-fix-hint kustomize-absent)
       [])]
  [render-result AppName ChartPath render-failed ErrorKind Error Issues Src] ->
    [(make-error (r18-code-for-kind ErrorKind)
       Src
       (cn AppName (cn ": " (r18-error-label ErrorKind)))
       (cn ChartPath (cn ": " Error))
       (r18-fix-hint ErrorKind)
       [])]
  _ -> [])


\* check-r18 — top-level R18 check. *\
(define check-r18
  Results -> (flatten (map (/. R (r18-check-result R)) Results)))
