\* r12-no-dangling-mount.shen — XPC012 no-dangling-mount

   A ConfigMap/Secret is "dangling" at a step if it appears in the
   step's Deleted set but some Pod-bearing owner that mounts it is
   still present in the step's State and the mount is not marked
   optional. *\


(define check-r12-step
  {(list A) --> (list (list A)) --> (list judgment)}
  [step AppName Wave Delta StateSec] MountRefs ->
    (let Deleted (delta-deleted-keys Delta)
         State  (state-keys StateSec)
      (flatten (map (/. M (check-r12-mount M Deleted State)) MountRefs)))
  _ _ -> [])


(define check-r12-mount
  {(list A) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  [mount-ref-fact OK ON ONs TK TN TNs MK Optional Src] Deleted State ->
    (if Optional
        []
        (if (and (key-in? TK TN TNs Deleted)
                 (key-in? OK ON ONs State))
            [(make-error "XPC012"
              Src
              (cn TK (cn " " (cn TN (cn " is pruned mid-sync but still mounted by "
                                       (cn OK (cn "/" ON))))))
              (cn "Non-optional " (cn MK (cn " mount of " (cn TK (cn " " (cn TN
                (cn " by " (cn OK (cn "/" (cn ON
                  (cn " would read from a ConfigMap/Secret that has already been pruned. "
                      "The mount is declared non-optional so the Pod will fail to start.")))))))))))
              (cn "Either delay pruning " (cn TK (cn " " (cn TN
                " until the dependent workload is gone, or mark the mount optional."))))
              [])]
            []))
  _ _ _ -> [])


\* Top-level R12 check *\
(define check-r12
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Trajectory MountRefs ->
    (flatten (map (/. S (check-r12-step S MountRefs)) Trajectory)))


\* ===== Cross-step variant =====
   For each step in the trajectory, for each Pod-bearing owner in that
   step's State, for each mount-ref whose owner is in this step's
   State, confirm the mount target is ALSO in this step's State (or
   the mount is optional). If the target is absent and the mount is
   not optional -> emit XPC012.

   "Target absent from State" is the unifying frame: it handles the
   same-wave case (target hook-deleted at end-of-wave so it is absent
   from the wave's final State) AND the cross-wave case (target
   hook-deleted at an earlier wave, Pod survives into a later wave
   whose State no longer contains the target). *\

(define check-r12-cross-step
  {(list A) --> (list (list A)) --> (list judgment)}
  [step AppName Wave Delta StateSec] MountRefs ->
    (let State (state-keys StateSec)
      (flatten (map (/. M (check-r12-cross-mount M State)) MountRefs)))
  _ _ -> [])


(define check-r12-cross-mount
  {(list A) --> (list (list A)) --> (list judgment)}
  [mount-ref-fact OK ON ONs TK TN TNs MK Optional Src] State ->
    (if Optional
        []
        (if (and (key-in? OK ON ONs State)
                 (not (key-in? TK TN TNs State)))
            [(make-error "XPC012"
              Src
              (cn TK (cn " " (cn TN (cn " is absent from the trajectory state but still mounted by "
                                       (cn OK (cn "/" ON))))))
              (cn "Non-optional " (cn MK (cn " mount of " (cn TK (cn " " (cn TN
                (cn " by " (cn OK (cn "/" (cn ON
                  (cn " would read from a ConfigMap/Secret that has already been pruned "
                      "or was never applied. The mount is declared non-optional so the Pod will fail to start.")))))))))))
              (cn "Either delay pruning " (cn TK (cn " " (cn TN
                " until the dependent workload is gone, or mark the mount optional."))))
              [])]
            []))
  _ _ -> [])


\* Top-level R12 cross-step check *\
(define check-r12-cross
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Trajectory MountRefs ->
    (flatten (map (/. S (check-r12-cross-step S MountRefs)) Trajectory)))
