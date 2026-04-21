\* r21-late-init-needs-ignore-diff.shen — Rule R21: XPC.E.late-init-needs-ignore-diff

   Emit XPC.E.late-init-needs-ignore-diff for every LateInitUsage that is
   not covered by any ignoreDifferences entry in the world.

   Background: upjet-generated Crossplane providers (AWS, GCP, Azure) mirror
   observed cloud state into spec.forProvider.* via the LateInitialize
   management policy. ArgoCD sees the provider's write as drift from the
   git-declared manifest and shows the Application OutOfSync forever unless
   the ApplicationSet declares an ignoreDifferences entry covering the
   field, OR the Composition uses managementPolicies to omit LateInitialize,
   OR per-resource `omitLateInitialize` is set.

   Coverage check mirrors R16 (first-pass / forgiving): an ignore-diff entry
   covers a late-init usage when at least one of:
     - the entry's JSONPointer string-contains the leaf segment of FieldPath
     - the entry's JQPath string-contains the leaf segment of FieldPath

   Tighter per-app joins and full path matching are deferred to a follow-up,
   just like R16. *\


\* r21-leaf-of — return the last dotted segment of a dotted path.
   e.g. "spec.forProvider.idleTimeout" -> "idleTimeout" *\
(define r21-leaf-of
  Path -> (r21-last-seg (explode Path) []))

(define r21-last-seg
  [] Acc -> (xpc-implode (xpc-reverse Acc))
  ["." | Rest] _ -> (r21-last-seg Rest [])
  [C | Rest] Acc -> (r21-last-seg Rest [C | Acc]))


\* r21-entry-covers? — true when an ignore-diff-entry covers FieldPath. *\
(define r21-entry-covers?
  Leaf [ignore-diff-entry _ _ _ JSONPointer JQPath] ->
    (or (and (not (= JSONPointer "")) (string-contains? JSONPointer Leaf))
        (and (not (= JQPath ""))      (string-contains? JQPath Leaf)))
  _ _ -> false)


\* r21-covered? — true when at least one entry in IgnoreDiffEntries covers the leaf. *\
(define r21-covered?
  Leaf [] -> false
  Leaf [Entry | Rest] ->
    (if (r21-entry-covers? Leaf Entry)
        true
        (r21-covered? Leaf Rest)))


\* r21-check-usage — check one LateInitUsage against all IgnoreDiffEntries.
   Returns [] (no error) or a singleton list containing the judgment. *\
(define r21-check-usage
  [late-init-usage-fact Group Kind Name Namespace FieldPath Src]
    IgnoreDiffEntries ->
      (let Leaf (r21-leaf-of FieldPath)
        (if (r21-covered? Leaf IgnoreDiffEntries)
            []
            [(make-error "XPC.E.late-init-needs-ignore-diff"
                Src
                (cn Kind (cn "/" (cn Name (cn ": late-init field " FieldPath))))
                (cn "The field " (cn FieldPath
                  (cn " on " (cn Kind
                    (cn " (group: " (cn Group
                      (cn ") is late-initialized by the Crossplane provider from observed cloud state. "
                        "No ignoreDifferences entry covers this path. Argo CD will fight the provider.")))))))
                (cn "Either add ignoreDifferences on the owning Application covering "
                  (cn Leaf
                    ", OR set managementPolicies to omit LateInitialize, OR use omitLateInitialize on the resource."))
                [])]))
  _ _ -> [])


\* check-r21 — top-level R21 check.
   LateInitUsages: list of late-init-usage-fact tuples.
   IgnoreDiffEntries: list of ignore-diff-entry tuples. *\
(define check-r21
  LateInitUsages IgnoreDiffEntries ->
    (flatten (map (/. Usage
                    (r21-check-usage Usage IgnoreDiffEntries))
                  LateInitUsages)))
