\* r13-no-immutable-change.shen — XPC013 no-immutable-change (framework only)

   Emits a diagnostic when a resource in a step's Updated set has a
   field path listed in the immutable-fields registry. In the current
   phase the trajectory simulator does not detect updates (every
   Delta.Updated is empty), so this rule is framework-only and always
   returns no judgments on real input. A follow-up ticket will
   populate Delta.Updated and this rule will fire without changes. *\


(define check-r13-step
  {(list A) --> (list (list A)) --> (list judgment)}
  [step AppName Wave Delta _] ImmutableFields ->
    (let Updated (delta-updated-keys Delta)
      (flatten (map (/. K (check-r13-key K ImmutableFields)) Updated)))
  _ _ -> [])


(define delta-updated-keys
  {(list A) --> (list (list A))}
  [delta _ [updated | Keys] _] -> Keys
  _ -> [])


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
