\* r17-resource-field-valid.shen — Rule R17: XPC.A.resource-field-valid

   Walks the (resource-field-facts ...) section produced by the Go bridge.
   Each entry is:

     (resource-field-fact APIVersion Kind Namespace Name Path Violation Message Src)

   where Violation is one of the symbols UnknownField, WrongType,
   MissingRequired, InvalidEnum.

   Emits one XPC.A.resource-field-valid judgment per fact. All heavy lifting
   (schema walking, type checks, enum checks) happens in Go —
   pkg/schemas.ValidateManifest — so this rule is a pure fact→judgment mapper
   mirroring the R5 shape. *\


\* r17-violation-label — human-readable label for a violation symbol.
   Violation symbols are lowercase, dashed; uppercase names would be
   interpreted as variables under Shen's pattern-match convention. *\
(define r17-violation-label
  unknown-field    -> "unknown field"
  wrong-type       -> "wrong type"
  missing-required -> "missing required field"
  invalid-enum     -> "invalid enum value"
  V                -> "schema violation")


\* r17-fix-hint — short remediation hint keyed off the violation kind. *\
(define r17-fix-hint
  unknown-field    -> "Remove the field, or update the CRD schema to declare it."
  wrong-type       -> "Correct the value's type to match the schema."
  missing-required -> "Add the required field to the manifest."
  invalid-enum     -> "Pick one of the schema's allowed enum values."
  V                -> "Fix the manifest to satisfy the CRD schema.")


\* r17-check-fact — emit one judgment per resource-field-fact. *\
(define r17-check-fact
  [resource-field-fact APIVersion Kind Namespace Name Path Violation Message Src] ->
    [(make-error "XPC.A.resource-field-valid"
       Src
       (cn Kind (cn "/" (cn Name (cn ": " (cn (r17-violation-label Violation) (cn " at " Path))))))
       Message
       (r17-fix-hint Violation)
       [])]
  _ -> [])


\* check-r17 — top-level R17 check. *\
(define check-r17
  Facts -> (flatten (map (/. F (r17-check-fact F)) Facts)))
