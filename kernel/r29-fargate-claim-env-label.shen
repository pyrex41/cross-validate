\* r29-fargate-claim-env-label.shen — Rule D3:
   XPC.E.fargate-claim-env-label

   Every Crossplane claim of a policed kind (FargateApp / FargateWorker /
   FargateService by default) must carry the environment label
   (app.facilitygrid.io/environment by default) with a value in the allowed
   enum (prod / preview / ops by default).

   Background: the environment label drives blast-radius reasoning, monitoring
   escalation, and account scoping. A claim with a MISSING label is invisible
   to that tooling; a claim with an INVALID value (typo, or an env that does
   not exist) silently misroutes. This is a forward-looking rule: claims live
   in Helm values files, so coverage depends on the scan scope including
   deploy/facilitygrid/{prod,preview}/ (the values parse as claim docs) or on
   Helm rendering being enabled.

   The Go bridge (buildFargateEnvLabelViolations) pre-filters to claim kinds
   and classifies each violation as `env-missing` or `env-invalid`; this rule
   maps each to a judgment — the R15/R16 precomputed-violation pattern.

   Fact shape (from pkg/checker/bridge.go fargateEnvLabelViolationToObj):
     [fargate-env-label-violation Kind Name Namespace ReasonSym Value Src]
   where ReasonSym is `env-missing` or `env-invalid`.

   NOTE on `cn`: Shen's `cn` takes exactly 2 string arguments; longer
   concatenations are nested (cn s1 (cn s2 s3)) chains. Discriminators are
   lowercase-dashed symbols (never booleans), matching R22/R23 convention. *\


(define r29-emit-missing
  Kind Name Src ->
    (make-error "XPC.E.fargate-claim-env-label"
      Src
      (cn Kind (cn "/" (cn Name
        " is missing the app.facilitygrid.io/environment label")))
      (cn "Crossplane claim " (cn Kind (cn "/" (cn Name
        " carries no app.facilitygrid.io/environment label. The label drives blast-radius reasoning, monitoring escalation, and account scoping; without it the claim is invisible to that tooling."))))
      "Add label app.facilitygrid.io/environment with a value in {prod, preview, ops}."
      []))


(define r29-emit-invalid
  Kind Name Value Src ->
    (make-error "XPC.E.fargate-claim-env-label"
      Src
      (cn Kind (cn "/" (cn Name
        (cn " has environment label value '" (cn Value
          "' which is not an allowed environment")))))
      (cn "app.facilitygrid.io/environment is '" (cn Value
        "' but the allowed values are {prod, preview, ops}. A typo or non-existent env silently misroutes monitoring and account scoping."))
      "Set app.facilitygrid.io/environment to one of {prod, preview, ops}, or extend env-label.allowed-values in xpc.yaml."
      []))


\* r29-check-row — dispatch one violation fact by reason symbol. *\
(define r29-check-row
  [fargate-env-label-violation Kind Name Ns env-missing Value Src] ->
    [(r29-emit-missing Kind Name Src)]
  [fargate-env-label-violation Kind Name Ns env-invalid Value Src] ->
    [(r29-emit-invalid Kind Name Value Src)]
  _ -> [])


\* check-r29 — top-level D3 check. Violations: precomputed claim-label
   violations from the Go bridge. *\
(define check-r29
  Violations ->
    (flatten (map (/. Row (r29-check-row Row)) Violations)))
