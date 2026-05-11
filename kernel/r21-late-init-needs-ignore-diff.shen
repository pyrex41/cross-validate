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


\* r21-string-list-member? — true when the string S appears in List. *\
(define r21-string-list-member?
  _ [] -> false
  S [X | Rest] -> (if (= S X) true (r21-string-list-member? S Rest)))


\* r21-scope-matches? — entry group/kind apply to (ResG, ResK). "*" and ""
   both act as wildcards (preserves pre-scoping behaviour for entries that
   omit the scope filter). *\
(define r21-scope-matches?
  EntryG EntryK ResG ResK ->
    (and (or (= EntryG "*") (= EntryG "") (= EntryG ResG))
         (or (= EntryK "*") (= EntryK "") (= EntryK ResK))))


\* r21-entry-covers? — true when an ignore-diff-entry covers FieldPath for
   a resource of (ResG, ResK). Late-init paths are Crossplane-written, so
   the canonical Crossplane-on-Argo wildcard
   (`group: "*", kind: "*", managedFieldsManagers: [crossplane]`) covers
   every late-init field. *\
(define r21-entry-covers?
  Leaf ResG ResK [ignore-diff-entry _ EntryG EntryK JSONPointer JQPath MFM] ->
    (if (r21-scope-matches? EntryG EntryK ResG ResK)
        (or (r21-string-list-member? "crossplane" MFM)
            (or (and (not (= JSONPointer "")) (string-contains? JSONPointer Leaf))
                (and (not (= JQPath ""))      (string-contains? JQPath Leaf))))
        false)
  _ _ _ _ -> false)


\* r21-covered? — true when at least one entry in IgnoreDiffEntries covers
   (ResG, ResK, Leaf). *\
(define r21-covered?
  Leaf ResG ResK [] -> false
  Leaf ResG ResK [Entry | Rest] ->
    (if (r21-entry-covers? Leaf ResG ResK Entry)
        true
        (r21-covered? Leaf ResG ResK Rest)))


\* r21-check-usage — check one LateInitUsage against all IgnoreDiffEntries.
   Returns [] (no error) or a singleton list containing the judgment. *\
(define r21-check-usage
  [late-init-usage-fact Group Kind Name Namespace FieldPath Src]
    IgnoreDiffEntries ->
      (let Leaf (r21-leaf-of FieldPath)
        (if (r21-covered? Leaf Group Kind IgnoreDiffEntries)
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


\* r21-violation-to-judgment — Go precomputes ignoreDifferences coverage and
   emits only late-init usages that are not covered. *\
(define r21-violation-to-judgment
  [r21-violation Group Kind Name _ FieldPath Leaf Src] ->
    (make-error "XPC.E.late-init-needs-ignore-diff"
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
      [])
  _ -> [])

\* check-r21 — top-level R21 check. *\
(define check-r21
  Violations -> (map (/. V (r21-violation-to-judgment V)) Violations))
