\* r33-duplicate-env-key.shen — Rule R33: XPC.M.duplicate-env-key

   Category M (Convergence / steady-state), Tier-2 (heuristic). Emit
   XPC.M.duplicate-env-key for every go-templating Composition that builds an
   ECS containerDefinitions environment array with the same variable name more
   than once.

   AWS dedupes the env array on registration, so a desired containerDefinitions
   carrying a duplicate name never matches the stored task def (one entry) → a
   permanent diff on the container_definitions field, which is IMMUTABLE → upjet
   refuses to update and hard-fails with ReconcileError ("cannot change the
   value of container_definitions ... requires replacing it"). This is the
   convergence-failure sibling of R31: desired never equals observed.

   The Go bridge precomputes the per-name counts and emits one r33-violation per
   (composition, duplicated env name). The kernel renders the judgment at warn
   severity — a template-text scan cannot tell whether the two entries land in
   the same container (the bug) or different containers of a multi-container
   task (legitimate), so the message says so. *\


(define r33-violation-to-judgment
  [r33-violation Composition EnvName Count Src] ->
    (make-warning "XPC.M.duplicate-env-key"
      Src
      (cn "Composition " (cn Composition (cn ": duplicate env var " EnvName)))
      (cn "Composition " (cn Composition
        (cn " emits the environment variable " (cn EnvName
          (cn " " (cn Count
            (cn " times. AWS dedupes the ECS env array on registration, so the desired containerDefinitions never matches the stored task def — a permanent diff on the immutable container_definitions field, which upjet cannot update (ReconcileError). If the copies are in different containers of a multi-container task this is a false positive."
              "")))))))
      (cn "Emit " (cn EnvName
        " exactly once (e.g. make a single entry's value conditional instead of appending an override, since ECS last-wins does not survive AWS env dedup)."))
      [])
  _ -> [])


\* check-r33 — top-level R33 check. Go pre-filters to duplicated names. *\
(define check-r33
  Violations -> (map (/. V (r33-violation-to-judgment V)) Violations))
