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
