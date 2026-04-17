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
  [rbac-binding-fact BKind BName BNs _ _ _ RoleKind RoleName BSrc] Deleted OK ON Src ->
    (if (or (key-in? BKind BName BNs Deleted)
            (key-in? RoleKind RoleName "" Deleted))
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
  [rbac-binding-fact _ _ _ "ServiceAccount" SAName SANs _ _ _] SAName SANs -> true
  _ _ _ -> false)


\* Top-level R14 check *\
(define check-r14
  {(list (list A)) --> (list (list A)) --> (list (list A)) --> (list judgment)}
  Trajectory SARefs Bindings ->
    (flatten (map (/. S (check-r14-step S SARefs Bindings)) Trajectory)))
