\* r25-prod-appset-autosync.shen — Rule R25:
   XPC.E.prod-appset-autosync

   Emit XPC.E.prod-appset-autosync for every ApplicationSet whose name matches
   a "prod" pattern AND whose template enables automated sync.

   Background: a production ApplicationSet with automated sync means a
   destructive commit lands in production without a human click. INC-6
   (fg-synapse, 2026-04-22) was triggered by an out-of-band delete, but the
   same cascade would have fired under any commit that reduced the generator
   output, because the prod AppSets had syncPolicy.automated enabled. The
   fg-manifold remediation (!1648 / commit a5f77a3b8) dropped
   `spec.template.spec.syncPolicy.automated` from 5 prod AppSets; this rule
   catches any regression.

   Name patterns (hardcoded for now; kernel config file is a P1 follow-up):
     "-prod"   — catches names like "crossplane-platform-aws-prod"
     "prod-"   — catches names like "prod-services"

   Fact shape (from pkg/checker/bridge.go appSetAutosyncToObj):
     [appset-autosync-fact Name AutoSym Src]
   where AutoSym is `auto-yes` / `auto-no` (lowercase-dashed symbols, not
   Shen booleans — same convention as R22/R23/R24).

   NOTE on `cn`: strictly 2-argument; longer chains are nested `(cn s1 (cn s2 s3))`. *\


\* r25-is-prod-name? — true when Name contains "-prod" or "prod-".
   Prod-name classification is substring-based to avoid over-matching
   Applications like "prodrome-staging". string-contains? from the prelude
   is the same primitive R21 uses for leaf-path matching. *\
(define r25-is-prod-name?
  Name ->
    (if (string-contains? Name "-prod")
        true
        (string-contains? Name "prod-")))


\* r25-emit — build the judgment for one prod AppSet with automated sync. *\
(define r25-emit
  Name Src ->
    (make-error "XPC.E.prod-appset-autosync"
      Src
      (cn "ApplicationSet " (cn Name
        " matches prod name pattern AND enables automated sync on generated Applications"))
      (cn "spec.template.spec.syncPolicy.automated is present, so every generated "
        (cn "Application auto-syncs any git change without human approval. For production "
          "ApplicationSets this is a force multiplier: a destructive commit (e.g. a generator that stops producing a parameter set, or a change that removes a state-bearing resource) lands in prod without a human click. fg-synapse INC-6 was exactly this shape."))
      (cn "Remove spec.template.spec.syncPolicy.automated from the ApplicationSet template, "
        "forcing manual sync for each generated Application. If automated sync is genuinely required for a prod AppSet, rename the AppSet so it does not match the prod pattern, or split into a non-prod-named sibling.")
      [])
  _ _ -> [])


\* r25-check-row — dispatch one appset-autosync-fact. Fires only when both
   gates pass (prod name AND auto-yes). Uniform named bindings in both
   branches — avoids shen-go pattern-compile panic. *\
(define r25-check-row
  [appset-autosync-fact Name auto-yes Src] ->
    (if (r25-is-prod-name? Name)
        [(r25-emit Name Src)]
        [])
  [appset-autosync-fact Name auto-no Src] -> []
  _ -> [])


\* check-r25 — top-level R25 check.
   Facts: list of appset-autosync-fact tuples, one per ApplicationSet. *\
(define check-r25
  Facts ->
    (flatten (map (/. Row (r25-check-row Row)) Facts)))
