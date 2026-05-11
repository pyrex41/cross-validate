\* r14-no-rbac-regression.shen — XPC014 no-rbac-regression

   For each step in the trajectory, for each Pod-bearing owner in the
   step's State that pins a ServiceAccount, look at later steps. If any
   later step Deletes an RBACBinding whose subject is that SA, or
   Deletes a Role/ClusterRole referenced by that binding, emit XPC014.

   In this phase the simulator's Delta.Deleted only includes hook-
   delete-policy-marked resources, so R14 fires when a binding or role
   is annotated for hook deletion while a pod that depends on it still
   exists in a later wave.

   The rule is intentionally conservative in its pair matching — it
   does not reason about which rules inside a Role the Pod actually
   needed. Any deletion of a Binding or Role that a live SA is still
   attached to is enough to emit XPC014. *\


(define check-r14-step
  {(list A) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  [step AppName Wave Delta _] SARefs Bindings ->
    (let Deleted (delta-deleted-keys Delta)
      (flatten (map (/. SA (check-r14-sa-against-deleted SA Deleted Bindings))
                    SARefs)))
  _ _ _ -> [])


(define check-r14-sa-against-deleted
  {(list A) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  [sa-ref-fact OK ON ONs SAName SANs Src] Deleted Bindings ->
    (let MyBindings (filter (/. B (binding-uses-sa? B SAName SANs)) Bindings)
      (flatten (map (/. B (check-r14-binding B Deleted OK ON Src)) MyBindings)))
  _ _ _ -> [])


(define check-r14-binding
  {(list A) --> (list (list A)) --> string --> string --> source-loc --> (list judgment)}
  [rbac-binding-fact BKind BName BNs _ _ _ RoleKind RoleName RoleNs BSrc] Deleted OK ON Src ->
    (if (or (key-in? BKind BName BNs Deleted)
            (key-in? RoleKind RoleName RoleNs Deleted))
        [(make-error "XPC014"
          Src
          (cn "RBAC regression: " (cn BKind (cn "/" (cn BName
            (cn " or its Role is removed while " (cn OK (cn "/" (cn ON " still needs it"))))))))
          (cn "Pod-bearing resource " (cn OK (cn "/" (cn ON
            (cn " pins a ServiceAccount whose permissions flow through binding " (cn BName
              ". Deleting the binding or role mid-sync leaves the workload without the access it declared."))))))
          (cn "Order the deletion of " (cn BName
            " to run after the dependent workload is torn down, or keep the binding in place."))
          [BSrc])]
        [])
  _ _ _ _ _ -> [])


(define binding-uses-sa?
  {(list A) --> string --> string --> boolean}
  [rbac-binding-fact _ _ _ "ServiceAccount" SAName SANs _ _ _ _] SAName SANs -> true
  _ _ _ -> false)


\* Top-level R14 check *\
(define check-r14
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  Trajectory SARefs Bindings ->
    (flatten (map (/. S (check-r14-step S SARefs Bindings)) Trajectory)))


\* ===== Cross-step variant =====
   For each step, for each Pod-bearing owner in that step's State that
   pins a ServiceAccount, require at least one RBAC binding for that SA
   whose binding AND role are both present in this step's State. If
   none survives in the step's State, emit XPC014.

   Same "target absent from State" framing as R12-cross. *\

(define check-r14-cross-step
  {(list A) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  [step AppName Wave Delta StateSec] SARefs Bindings ->
    (let State (state-keys StateSec)
      (flatten (map (/. SA (check-r14-cross-sa SA State Bindings)) SARefs)))
  _ _ _ -> [])


(define check-r14-cross-sa
  {(list A) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  [sa-ref-fact OK ON ONs SAName SANs Src] State Bindings ->
    (if (key-in? OK ON ONs State)
        (let MyBindings (filter (/. B (binding-uses-sa? B SAName SANs)) Bindings)
          (if (any-binding-live? MyBindings State)
              []
              (r14-cross-emit-all MyBindings OK ON Src)))
        [])
  _ _ _ -> [])


\* any-binding-live? — does any binding in MyBindings have both its
   binding key and its role key present in State? *\
(define any-binding-live?
  {(list (list A)) --> (list (list A)) --> boolean}
  [] _ -> false
  [B | Rest] State -> (if (binding-live? B State)
                          true
                          (any-binding-live? Rest State)))


\* binding-live? — tightened in P5.e: a binding is live only if BOTH
   the binding itself AND the Role it references are present in the
   step's State. The rbac-binding-fact now carries the Role's
   namespace (RoleNs) per the bridge schema, resolved by the IR
   extractor per Kubernetes semantics:
     - ClusterRoleBinding → RoleRef must target a ClusterRole (cluster-
       scoped), RoleNs = "".
     - RoleBinding + RoleRef.kind=Role → RoleNs = binding's own ns.
     - RoleBinding + RoleRef.kind=ClusterRole → RoleNs = "" (cluster-
       scoped ClusterRole).
   Lookup by (RoleKind, RoleName, RoleNs) is now accurate for both
   namespaced Roles and cluster-scoped (Cluster)Roles. *\
(define binding-live?
  {(list A) --> (list (list A)) --> boolean}
  [rbac-binding-fact BKind BName BNs _ _ _ RoleKind RoleName RoleNs _] State ->
    (and (key-in? BKind BName BNs State)
         (key-in? RoleKind RoleName RoleNs State))
  _ _ -> false)


\* r14-cross-emit-all — emit one XPC014 per binding in MyBindings that
   is not live in State. If MyBindings is empty, emit nothing: that is
   a coverage gap R14 does not speak to. *\
(define r14-cross-emit-all
  {(list (list A)) --> string --> string --> source-loc --> (list judgment)}
  [] _ _ _ -> []
  [B | Rest] OK ON Src -> (append (check-r14-cross-binding B OK ON Src)
                                  (r14-cross-emit-all Rest OK ON Src)))


(define check-r14-cross-binding
  {(list A) --> string --> string --> source-loc --> (list judgment)}
  [rbac-binding-fact BKind BName BNs _ _ _ RoleKind RoleName _ BSrc] OK ON Src ->
    [(make-error "XPC014"
      Src
      (cn "RBAC regression: " (cn BKind (cn "/" (cn BName
        (cn " or its Role is absent from state while " (cn OK (cn "/" (cn ON " still needs it"))))))))
      (cn "Pod-bearing resource " (cn OK (cn "/" (cn ON
        (cn " pins a ServiceAccount whose permissions flow through binding " (cn BName
          ". The binding or its role is not present in the trajectory state at this step, so the workload loses the access it declared."))))))
      (cn "Order the deletion of " (cn BName
        " to run after the dependent workload is torn down, or keep the binding in place."))
      [BSrc])]
  _ _ _ _ -> [])


\* r14-cross-violation-to-judgment — Go precomputes the expensive
   trajectory/state/binding joins and sends only missing-live-binding facts. *\
(define r14-cross-violation-to-judgment
  [r14-violation BKind BName _ _ _ _ OK ON Src BSrc] ->
    (make-error "XPC014"
      Src
      (cn "RBAC regression: " (cn BKind (cn "/" (cn BName
        (cn " or its Role is absent from state while " (cn OK (cn "/" (cn ON " still needs it"))))))))
      (cn "Pod-bearing resource " (cn OK (cn "/" (cn ON
        (cn " pins a ServiceAccount whose permissions flow through binding " (cn BName
          ". The binding or its role is not present in the trajectory state at this step, so the workload loses the access it declared."))))))
      (cn "Order the deletion of " (cn BName
        " to run after the dependent workload is torn down, or keep the binding in place."))
      [BSrc])
  _ -> [])

\* Top-level R14 cross-step check *\
(define check-r14-cross
  {(list (list A)) --> (list judgment)}
  Violations -> (map (/. V (r14-cross-violation-to-judgment V)) Violations))
