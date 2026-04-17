\* r5-patch-typecheck.shen — Rule R5: patch type checking

   Walks the (resolved-patches ...) section produced by the Go bridge.
   Each entry is (resolved-patch CompName Src FromPath ToPath FromType ToType)
   where FromType has already had user-supplied convert transforms applied.
   Emits XPC005 when FromType is not assignable to ToType. *\

\* Type assignability — mirrors pkg/schemas.TypeAssignable in Go. *\
(define type-assignable?
  {string --> string --> boolean}
  X X -> true
  "unknown" _ -> true
  _ "unknown" -> true
  "integer" "number" -> true
  _ _ -> false)

\* Check one resolved patch. *\
(define check-r5-patch
  {(list A) --> (list judgment)}
  [resolved-patch CompName Src FromPath ToPath FromType ToType] ->
    (if (type-assignable? FromType ToType)
        []
        [(make-error "XPC005"
          Src
          (cn "patch type mismatch in Composition " CompName)
          (cn "Field " (cn FromPath (cn " has type " (cn FromType
            (cn " but target field " (cn ToPath (cn " has type " (cn ToType
              ". These types are not compatible without an explicit transform."))))))))
          (cn "Add a transform (e.g., convert: { toType: " (cn ToType " }) to the patch."))
          [])])
  _ -> [])

\* Top-level R5 check. *\
(define check-r5
  {(list (list A)) --> (list judgment)}
  Patches -> (flatten (map (/. P (check-r5-patch P)) Patches)))
