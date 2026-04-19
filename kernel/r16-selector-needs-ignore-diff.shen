\* r16-selector-needs-ignore-diff.shen — Rule R16: selector-needs-ignore-diff (XPC.E.selector-needs-ignore-diff)

   Emit XPC.E.selector-needs-ignore-diff for every SelectorUsage that is not
   covered by any ignoreDifferences entry in the world.

   Background: when a Crossplane managed resource has a *Selector field set,
   Crossplane resolves it at runtime and writes the concrete value into a sibling
   field (the ResolvedPath). Argo CD sees this late-init write as unwanted drift
   and will fight Crossplane forever unless the ResolvedPath is suppressed via
   an ignoreDifferences entry on the owning Application.

   Coverage check (first-pass / forgiving):
     An ignore-diff entry covers a selector usage when at least one of:
       - The entry's JSONPointer string-contains the ResolvedPath, OR
       - The entry's JQPath string-contains the ResolvedPath.
     This is intentionally broad: a single ignoreDifferences[].jsonPointers entry
     covering a path prefix matches all sub-paths. Tighter per-app join is
     deferred to a follow-up pass.

   Error code: XPC.E.selector-needs-ignore-diff
   Fix hint:   add an ignoreDifferences entry with the JSONPointer form of
               the ResolvedPath to the owning Application. *\


\* r16-string-contains? — true if Haystack contains Needle as a substring.
   Shen doesn't have a built-in contains — use cn-scan to walk character by
   character. For efficiency we use the string-index trick via (pos Needle Haystack). *\
(define r16-string-contains?
  Needle Haystack ->
    (let Nlen (length (explode Needle))
         Hlen (length (explode Haystack))
      (r16-scan? (explode Needle) (explode Haystack) Nlen Hlen)))

(define r16-scan?
  _ _ Nlen Hlen -> false where (> Nlen Hlen)
  Needle Haystack Nlen Hlen ->
    (if (r16-prefix? Needle Haystack)
        true
        (r16-scan? Needle (tl Haystack) Nlen (- Hlen 1))))

(define r16-prefix?
  [] _ -> true
  _ [] -> false
  [C | Rest1] [C | Rest2] -> (r16-prefix? Rest1 Rest2)
  _ _ -> false)


\* r16-entry-covers? — true when an ignore-diff-entry fact covers ResolvedPath. *\
(define r16-entry-covers?
  ResolvedPath [ignore-diff-entry _ _ _ JSONPointer JQPath] ->
    (or (and (not (= JSONPointer "")) (r16-string-contains? ResolvedPath JSONPointer))
        (and (not (= JQPath ""))      (r16-string-contains? ResolvedPath JQPath)))
  _ _ -> false)


\* r16-covered? — true when at least one entry in IgnoreDiffEntries covers ResolvedPath. *\
(define r16-covered?
  ResolvedPath [] -> false
  ResolvedPath [Entry | Rest] ->
    (if (r16-entry-covers? ResolvedPath Entry)
        true
        (r16-covered? ResolvedPath Rest)))


\* r16-check-usage — check one SelectorUsage against all IgnoreDiffEntries.
   Returns [] (no error) or a one-element list containing the judgment. *\
(define r16-check-usage
  [selector-usage-fact Group Kind Name Namespace SelectorPath ResolvedPath Src]
    IgnoreDiffEntries ->
      (if (r16-covered? ResolvedPath IgnoreDiffEntries)
          []
          [(make-error "XPC.E.selector-needs-ignore-diff"
              Src
              (cn Kind (cn "/" (cn Name (cn ": selector " (cn SelectorPath (cn " resolves to " ResolvedPath))))))
              (cn "The field " (cn SelectorPath
                (cn " on " (cn Kind
                  (cn " '" (cn Name
                    (cn "' (group: " (cn Group
                      (cn ") is a Crossplane selector. Crossplane will late-init "
                        (cn ResolvedPath
                          " after resolution, but no ignoreDifferences entry in any Application covers this resolved path. Argo CD will continuously fight Crossplane.")))))))))))
              (cn "Add an ignoreDifferences entry to the owning Application with jsonPointers: [\"/spec/forProvider/"
                (cn (r16-resolved-leaf ResolvedPath) "\"] or the full JSON Pointer form of the resolved path."))
              [])])
  _ _ -> [])


\* r16-resolved-leaf — extract the last segment of a dotted path for the fix hint. *\
(define r16-resolved-leaf
  Path ->
    (r16-last-segment (explode Path) ""))

(define r16-last-segment
  [] Acc -> Acc
  ["." | Rest] _ -> (r16-last-segment Rest "")
  [C | Rest] Acc -> (r16-last-segment Rest (cn Acc C)))


\* check-r16 — top-level R16 check.
   SelectorUsages: list of selector-usage-fact tuples.
   IgnoreDiffEntries: list of ignore-diff-entry tuples. *\
(define check-r16
  SelectorUsages IgnoreDiffEntries ->
    (flatten (map (/. Usage
                    (r16-check-usage Usage IgnoreDiffEntries))
                  SelectorUsages)))
