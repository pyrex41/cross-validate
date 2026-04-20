\* r19-values-well-typed.shen — Rule R19: XPC.H.values-well-typed

   Walks the (render-results ...) section and, for each RenderResult,
   emits one error judgment per entry in its (values-issues ...) list.
   Values-schema violations are detected in Go via renderer.ValidateValues
   (which reuses schemas.ValidateManifest from S3), so this rule is a
   pure fact→judgment mapper. *\


\* r19-issue-judgment — produce one judgment from a (values-issue Path Msg)
   tuple, carrying the owning app's name and source location for context. *\
(define r19-issue-judgment
  AppName Src [values-issue Path Msg] ->
    (make-error "XPC.H.values-well-typed"
      Src
      (cn AppName (cn ": values." (cn Path " violates values.schema.json")))
      Msg
      "Correct the value to match the chart's values.schema.json, or relax the schema."
      [])
  _ _ _ -> (make-error "XPC.H.values-well-typed"
             [source "" 0]
             "malformed values-issue"
             ""
             ""
             []))


\* r19-issue-list-of — extract the list of (values-issue ...) entries from a
   (values-issues ...) wrapper. Returns [] when the wrapper is malformed. *\
(define r19-issue-list-of
  [values-issues | Rest] -> Rest
  _ -> [])


\* r19-check-result — emit one judgment per values-issue on this result.
   Matches both render-ok and render-failed discriminators: a failed render
   can still produce values-schema violations worth reporting. *\
(define r19-check-result
  [render-result AppName ChartPath SuccessSym ErrorKind Error Issues Src] ->
    (map (/. I (r19-issue-judgment AppName Src I))
         (r19-issue-list-of Issues))
  _ -> [])


\* check-r19 — top-level R19 check. *\
(define check-r19
  Results -> (flatten (map (/. R (r19-check-result R)) Results)))
