\* r16-selector-needs-ignore-diff.shen — Rule R16: XPC.E.selector-needs-ignore-diff

   Emit XPC.E.selector-needs-ignore-diff for every SelectorUsage that is not
   covered by any ignoreDifferences entry in the world.

   Background: when a Crossplane managed resource has a *Selector field set,
   Crossplane resolves it at runtime and writes the concrete value into a sibling
   field (the ResolvedPath). Argo CD sees this late-init write as unwanted drift
   and will fight Crossplane forever unless the ResolvedPath is suppressed via
   an ignoreDifferences entry on the owning Application.

   Coverage check (first-pass / forgiving): an ignore-diff entry covers a
   selector usage when at least one of:
     - the entry's JSONPointer string-contains the leaf segment of ResolvedPath
     - the entry's JQPath string-contains the leaf segment of ResolvedPath

   The "leaf segment" is the last dot-delimited component of the dotted path,
   e.g. for "spec.forProvider.vpcZoneIdentifier" the leaf is "vpcZoneIdentifier".
   This matches both /spec/forProvider/vpcZoneIdentifier (JSON Pointer) and
   .spec.forProvider.vpcZoneIdentifier (JQ) style entries.

   Tighter per-app joins and full path matching are deferred to a follow-up. *\


\* r16-leaf-of — return the last dotted segment of a dotted path.
   e.g. "spec.forProvider.vpcZoneIdentifier" -> "vpcZoneIdentifier" *\
(define r16-leaf-of
  Path -> (r16-last-seg (explode Path) []))

(define r16-last-seg
  [] Acc -> (xpc-implode (xpc-reverse Acc))
  ["." | Rest] _ -> (r16-last-seg Rest [])
  [C | Rest] Acc -> (r16-last-seg Rest [C | Acc]))


\* r16-string-list-member? — true when the string S appears in List. *\
(define r16-string-list-member?
  _ [] -> false
  S [X | Rest] -> (if (= S X) true (r16-string-list-member? S Rest)))


\* r16-scope-matches? — true when an ignoreDifferences entry's group/kind
   apply to the resource. ArgoCD treats "*" as wildcard; we also treat the
   empty string as wildcard so legacy fixtures and entries that omit
   group/kind retain pre-scoping behaviour. *\
(define r16-scope-matches?
  EntryG EntryK ResG ResK ->
    (and (or (= EntryG "*") (= EntryG "") (= EntryG ResG))
         (or (= EntryK "*") (= EntryK "") (= EntryK ResK))))


\* r16-entry-covers? — true when an ignore-diff-entry covers ResolvedPath
   for a resource of (ResG, ResK).
   Coverage reasons (logical OR), gated by scope:
     - the entry's managedFieldsManagers includes "crossplane"
       (canonical Crossplane-on-Argo pattern: every Crossplane-written
       field is exempt, regardless of path)
     - JSONPointer string-contains the leaf of ResolvedPath
     - JQPath string-contains the leaf of ResolvedPath *\
(define r16-entry-covers?
  Leaf ResG ResK [ignore-diff-entry _ EntryG EntryK JSONPointer JQPath MFM] ->
    (if (r16-scope-matches? EntryG EntryK ResG ResK)
        (or (r16-string-list-member? "crossplane" MFM)
            (or (and (not (= JSONPointer "")) (string-contains? JSONPointer Leaf))
                (and (not (= JQPath ""))      (string-contains? JQPath Leaf))))
        false)
  _ _ _ _ -> false)


\* r16-covered? — true when at least one entry in IgnoreDiffEntries covers
   (ResG, ResK, Leaf). *\
(define r16-covered?
  Leaf ResG ResK [] -> false
  Leaf ResG ResK [Entry | Rest] ->
    (if (r16-entry-covers? Leaf ResG ResK Entry)
        true
        (r16-covered? Leaf ResG ResK Rest)))


\* r16-check-usage — check one SelectorUsage against all IgnoreDiffEntries.
   Returns [] (no error) or a singleton list containing the judgment. *\
(define r16-check-usage
  [selector-usage-fact Group Kind Name Namespace SelectorPath ResolvedPath Src]
    IgnoreDiffEntries ->
      (let Leaf (r16-leaf-of ResolvedPath)
        (if (r16-covered? Leaf Group Kind IgnoreDiffEntries)
            []
            [(make-error "XPC.E.selector-needs-ignore-diff"
                Src
                (cn Kind (cn "/" (cn Name (cn ": selector " (cn SelectorPath (cn " resolves to " ResolvedPath))))))
                (cn "The field " (cn SelectorPath
                  (cn " on " (cn Kind
                    (cn " (group: " (cn Group
                      (cn ") is a Crossplane selector that resolves via late-init. Crossplane writes "
                        (cn ResolvedPath
                          " after resolution. No ignoreDifferences entry covers this path. Argo CD will fight Crossplane."))))))))
                (cn "Add ignoreDifferences to the owning Application: group: "
                  (cn Group (cn ", kind: " (cn Kind (cn ", jsonPointers containing: " Leaf)))))
                [])]))
  _ _ -> [])


\* check-r16 — top-level R16 check.
   SelectorUsages: list of selector-usage-fact tuples.
   IgnoreDiffEntries: list of ignore-diff-entry tuples. *\
(define check-r16
  SelectorUsages IgnoreDiffEntries ->
    (flatten (map (/. Usage
                    (r16-check-usage Usage IgnoreDiffEntries))
                  SelectorUsages)))
