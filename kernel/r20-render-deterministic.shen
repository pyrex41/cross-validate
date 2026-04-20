\* r20-render-deterministic.shen — Rule R20: XPC.H.render-deterministic

   Walks the (determinism-results ...) section produced by the Go bridge.
   Each entry is:

     (determinism-result AppName RendererKind Status DiffSummary Src)

   where Status is the symbol determ-match or determ-mismatch. As with R18
   we use lowercase-dashed discriminator symbols because uppercase
   identifiers are Shen variables and Shen's true/false are already
   special.

   R20 fires at *warning* severity whenever Status=determ-mismatch — the
   base plan explicitly documents that fg-manifold uses `randAlphaNum`
   legitimately in a handful of charts, so we want these offenders
   enumerated but not blocking. *\


\* r20-fix-hint — constant remediation string. Defined as a nullary function
   so callers can treat it as a value (Shen needs a pattern clause with a
   head-matches body; we use the wildcard pattern _). *\
(define r20-fix-hint
  _ -> "A second render produced different bytes. Check the chart or overlay for randAlphaNum, date-stamps, or other non-deterministic helpers. If the non-determinism is intentional, document it in an ADR.")


\* r20-check-result — emit one warning per mismatched result, nothing on a
   match. The pattern-match discriminators are plain symbols so Shen's
   treatment of literal true/false does not interfere. *\
(define r20-check-result
  [determinism-result AppName RendererKind determ-match DiffSummary Src] -> []
  [determinism-result AppName RendererKind determ-mismatch DiffSummary Src] ->
    [(make-warning "XPC.H.render-deterministic"
       Src
       (cn AppName (cn ": " (cn RendererKind " render is non-deterministic")))
       DiffSummary
       (r20-fix-hint 0)
       [])]
  _ -> [])


\* check-r20 — top-level R20 check. Maps over the determinism-results
   section and flattens the resulting per-entry lists. *\
(define check-r20
  Results -> (flatten (map (/. R (r20-check-result R)) Results)))
