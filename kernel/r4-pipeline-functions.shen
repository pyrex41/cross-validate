\* r4-pipeline-functions.shen — Rule R4: pipeline functions resolve

   R4a: Every functionRef in a pipeline must resolve to an installed Function.
   R4b: The input apiVersion must match the function's accepted input versions. *\

\* Check a single Composition's pipeline against installed functions *\
(define check-r4-composition
  {(list A) --> (list (list A)) --> (list judgment)}
  [composition-fact Name CTR Pipeline-mode Pipeline Src _] Functions ->
    (if (= Pipeline-mode "Pipeline")
        (flatten (map (/. Step (check-r4-step Name Src Step Functions)) Pipeline))
        [])
  _ _ -> [])

\* Check a single pipeline step *\
(define check-r4-step
  {string --> source-loc --> (list A) --> (list (list A)) --> (list judgment)}
  CompName CompSrc [StepName FnRef InputAV InputKind] Functions ->
    (let MatchingFns (filter (/. F (function-name-matches? F FnRef)) Functions)
      (if (= MatchingFns [])
          \* R4a: function not found *\
          [(make-error "XPC004"
            CompSrc
            (cn "Composition " (cn CompName (cn " step " (cn StepName
              (cn " references unknown function " (cn FnRef ""))))))
            (cn "Pipeline step '" (cn StepName (cn "' references function '" (cn FnRef
              "' but no Function resource with this name was found."))))
            (cn "Ensure Function '" (cn FnRef "' is defined and included in the checked manifests."))
            [])]
          \* R4b: check input version compatibility *\
          (if (= InputAV "")
              [] \* no input, skip version check *\
              (flatten (map (/. F (check-r4b-input-version
                                    CompName StepName FnRef InputAV CompSrc F))
                            MatchingFns)))))
  _ _ _ _ -> [])

\* Does the function name match? *\
(define function-name-matches?
  {(list A) --> string --> boolean}
  [function-fact Name | _] Name -> true
  _ _ -> false)

\* R4b: check input version compatibility *\
(define check-r4b-input-version
  {string --> string --> string --> string --> source-loc --> (list A) --> (list judgment)}
  CompName StepName FnRef InputAV CompSrc [function-fact _ _ InputVersions FnSrc _] ->
    (if (= InputVersions [])
        [] \* unknown function input versions, skip *\
        (if (member InputAV InputVersions)
            []
            [(make-error "XPC004"
              CompSrc
              (cn "Composition " (cn CompName (cn " step " (cn StepName
                (cn " input version mismatch for function " (cn FnRef ""))))))
              (cn "Pipeline step '" (cn StepName (cn "' passes input at " (cn InputAV
                (cn " but function '" (cn FnRef (cn "' accepts: "
                  (format-versions InputVersions))))))))
              (cn "Change the input apiVersion to one accepted by the function: "
                  (format-versions InputVersions))
              [FnSrc])]))
  _ _ _ _ _ _ -> [])

\* Format a list of version strings *\
(define format-versions
  {(list string) --> string}
  [] -> "(none)"
  [V] -> V
  [V | Rest] -> (cn V (cn ", " (format-versions Rest))))

\* Top-level R4 check *\
(define check-r4
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Compositions Functions ->
    (flatten (map (/. C (check-r4-composition C Functions)) Compositions)))
