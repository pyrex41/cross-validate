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
   For each mount-ref, scan every step in the trajectory. If there
   exists a step where the owner is in State AND the target is absent
   from State (and the mount is not optional), the mount-ref is
   "violating". Emit exactly one XPC012 per distinct
   (owner-kind, owner-name, owner-ns, target-kind, target-name,
   target-ns) tuple, even if multiple steps witness the violation or
   multiple mount-ref-fact entries share the same (owner, target).

   "Target absent from State" is the unifying frame: it handles the
   same-wave case (target hook-deleted at end-of-wave so it is absent
   from the wave's final State) AND the cross-wave case (target
   hook-deleted at an earlier wave, Pod survives into a later wave
   whose State no longer contains the target). *\

\* violates-at-step? — does a specific mount-ref violate at this step
   (owner in State, target absent from State)? *\
(define violates-at-step?
  {string --> string --> string --> string --> string --> string --> (list A) --> boolean}
  OK ON ONs TK TN TNs [step _ _ _ StateSec] ->
    (let State (state-keys StateSec)
      (and (key-in? OK ON ONs State)
           (not (key-in? TK TN TNs State))))
  _ _ _ _ _ _ _ -> false)

\* mount-ref-violates? — does this mount-ref violate at ANY step in
   the trajectory? *\
(define mount-ref-violates?
  {string --> string --> string --> string --> string --> string --> (list (list A)) --> boolean}
  _ _ _ _ _ _ [] -> false
  OK ON ONs TK TN TNs [S | Rest] ->
    (if (violates-at-step? OK ON ONs TK TN TNs S)
        true
        (mount-ref-violates? OK ON ONs TK TN TNs Rest)))

\* dedup-key-seen? — has this (owner, target) key tuple already been
   recorded in the seen list? *\
(define dedup-key-seen?
  {string --> string --> string --> string --> string --> string --> (list (list string)) --> boolean}
  _ _ _ _ _ _ [] -> false
  OK ON ONs TK TN TNs [[OK ON ONs TK TN TNs] | _] -> true
  OK ON ONs TK TN TNs [_ | Rest] -> (dedup-key-seen? OK ON ONs TK TN TNs Rest))

\* r12-cross-emit — build the XPC012 judgment for a violating mount-ref. *\
(define r12-cross-emit
  {string --> string --> string --> string --> string --> string --> source-loc --> judgment}
  OK ON TK TN MK Src ->
    (make-error "XPC012"
      Src
      (cn TK (cn " " (cn TN (cn " is absent from the trajectory state but still mounted by "
                               (cn OK (cn "/" ON))))))
      (cn "Non-optional " (cn MK (cn " mount of " (cn TK (cn " " (cn TN
        (cn " by " (cn OK (cn "/" (cn ON
          (cn " would read from a ConfigMap/Secret that has already been pruned "
              "or was never applied. The mount is declared non-optional so the Pod will fail to start.")))))))))))
      (cn "Either delay pruning " (cn TK (cn " " (cn TN
        " until the dependent workload is gone, or mark the mount optional."))))
      []))

\* check-r12-cross-fold — fold over mount-refs accumulating (Seen, Judgments).
   For each mount-ref, skip if optional, skip if no violation in any step,
   skip if (owner, target) key already in Seen. Otherwise add key to Seen
   and append a judgment. Returns [Seen Judgments]. *\
(define check-r12-cross-fold
  {(list (list A)) --> (list (list A)) --> (list (list string)) --> (list judgment) --> (list (list A))}
  _ [] Seen Acc -> [Seen Acc]
  Trajectory [[mount-ref-fact OK ON ONs TK TN TNs MK Optional Src] | Rest] Seen Acc ->
    (if Optional
        (check-r12-cross-fold Trajectory Rest Seen Acc)
        (if (not (mount-ref-violates? OK ON ONs TK TN TNs Trajectory))
            (check-r12-cross-fold Trajectory Rest Seen Acc)
            (if (dedup-key-seen? OK ON ONs TK TN TNs Seen)
                (check-r12-cross-fold Trajectory Rest Seen Acc)
                (check-r12-cross-fold
                  Trajectory
                  Rest
                  [[OK ON ONs TK TN TNs] | Seen]
                  (append Acc [(r12-cross-emit OK ON TK TN MK Src)])))))
  Trajectory [_ | Rest] Seen Acc ->
    (check-r12-cross-fold Trajectory Rest Seen Acc))


\* Top-level R12 cross-step check — dedups emissions per
   (owner-kind, owner-name, owner-ns, target-kind, target-name, target-ns). *\
(define check-r12-cross
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Trajectory MountRefs ->
    (hd (tl (check-r12-cross-fold Trajectory MountRefs [] []))))
