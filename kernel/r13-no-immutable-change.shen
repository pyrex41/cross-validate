\* =====================================================================
   RETIRED by P4.d (2026-04-23) in favour of R27 / XPC.P.immutable-change
   (plan-mode, variant-axis). Kept as dormant reference; not loaded by
   check.shen. See pkg/plan/r27.go.
===================================================================== *\

\* r13-no-immutable-change.shen — XPC013 no-immutable-change

   Emits a diagnostic when a resource in a step's Updated set has a
   field path listed in the immutable-fields registry. Dormant until the
   trajectory simulator starts populating Delta.Updated. *\


(define check-r13-step
  {(list A) --> (list (list A)) --> (list judgment)}
  [step AppName Wave Delta _] ImmutableFields ->
    (let Updated (delta-updated-keys Delta)
      (flatten (map (/. K (check-r13-key K ImmutableFields)) Updated)))
  _ _ -> [])


(define check-r13-key
  {(list A) --> (list (list A)) --> (list judgment)}
  [resource-key APIVersion Kind Ns Name] ImmutableFields ->
    (flatten (map (/. F (check-r13-field APIVersion Kind Ns Name F)) ImmutableFields))
  _ _ -> [])


(define check-r13-field
  {string --> string --> string --> string --> (list A) --> (list judgment)}
  APIVersion Kind Ns Name [immutable-field-fact Group FKind FieldPath Reason] ->
    (if (and (= Kind FKind)
             (api-version-matches-group? APIVersion Group))
        [(make-error "XPC013"
          [source "" 0]
          (cn "Immutable field " (cn FieldPath (cn " on " (cn Kind (cn "/" Name)))))
          Reason
          (cn "Do not change " (cn FieldPath
            (cn " after create. Delete and recreate the resource to change it.")))
          [])]
        [])
  _ _ _ _ _ -> [])


(define api-version-matches-group?
  {string --> string --> boolean}
  AV Group -> (= (api-version->group AV) Group))


\* Top-level R13 check *\
(define check-r13
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Trajectory ImmutableFields ->
    (flatten (map (/. S (check-r13-step S ImmutableFields)) Trajectory)))
