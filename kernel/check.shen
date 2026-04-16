\* check.shen — top-level entry point for the xpc type checker kernel

   The Go binary converts the World to Shen values, calls check-world
   directly via the in-process Shen runtime, and reads the judgment list
   back as Shen values.

   Protocol (in-process):
     input:  a Shen value representing the world
             [world [crds ...] [xrds ...] [compositions ...] [functions ...]
              [providers ...] [configurations ...] [resources ...]
              [argo-apps ...] [schemas ...]]
     output: a list of judgment values
             [[judgment Code Severity Source Message Detail Fix Related] ...] *\

\* Load all rule files *\
(load "prelude.shen")
(load "r1-versions.shen")
(load "r2-conversion.shen")
(load "r3-composition-resolves.shen")
(load "r4-pipeline-functions.shen")
(load "r5-patch-typecheck.shen")
(load "r6-wave-ordering.shen")
(load "r7-owner-refs.shen")
(load "r8-v1v2-machinery.shen")
(load "r9-bootstrap.shen")
(load "r10-secret-taint.shen")
(load "r11-temporal.shen")

\* ===== IR reading ===== *\

\* The world is expected as:
   [world
     [crds ...]
     [xrds ...]
     [compositions ...]
     [functions ...]
     [providers ...]
     [configurations ...]
     [resources ...]
     [argo-apps ...]
     [schemas ...]] *\

(define extract-section
  {symbol --> (list A) --> (list A)}
  _ [] -> []
  Tag [[Tag | Content] | _] -> Content
  Tag [_ | Rest] -> (extract-section Tag Rest))

\* ===== Main entry point ===== *\

(define check-world
  {(list A) --> (list judgment)}
  [world | Sections] ->
    (let CRDs         (extract-section crds Sections)
         XRDs         (extract-section xrds Sections)
         Compositions (extract-section compositions Sections)
         Functions    (extract-section functions Sections)
         Providers    (extract-section providers Sections)
         Configs      (extract-section configurations Sections)
         Resources    (extract-section resources Sections)
         ArgoApps     (extract-section argo-apps Sections)
         Schemas      (extract-section schemas Sections)

         \* Run all rules *\
         R1  (check-r1 CRDs XRDs)
         R2  (check-r2 Resources CRDs)
         R3  (check-r3 Compositions XRDs)
         R4  (check-r4 Compositions Functions)
         R5  (check-r5 Compositions XRDs Schemas)
         R6  (check-r6 ArgoApps Compositions XRDs Functions)
         R7  (check-r7 ArgoApps Compositions)
         R8  (check-r8 Resources XRDs)
         R9  (check-r9 Compositions Resources)
         R10 (check-r10 Compositions)
         R11 (check-r11 Resources Compositions Providers CRDs)

      (append R1 (append R2 (append R3 (append R4 (append R5
        (append R6 (append R7 (append R8 (append R9
          (append R10 R11))))))))))))

\* ===== Stdin/stdout protocol (legacy, kept for compatibility) ===== *\

\* Read the world from stdin, run checks, write judgments to stdout *\
(define run-checker
  {A --> (list judgment)}
  _ -> (let World (read (stinput))
            Judgments (check-world World)
         (do (pr (make-string "~S~%" [judgments | Judgments]) (stoutput))
             Judgments)))

\* Format a judgment as an s-expression string *\
(define format-judgment
  {judgment --> string}
  [judgment Code Sev [source File Line] Msg Detail Fix Related] ->
    (make-string "(judgment ~S ~S (source ~S ~A) ~S ~S ~S ~S)"
      Code Sev File Line Msg Detail Fix Related)
  _ -> "(judgment \"???\" error (source \"\" 0) \"malformed judgment\" \"\" \"\" ())")

\* Format all judgments *\
(define format-judgments
  {(list judgment) --> string}
  Js -> (cn "(judgments " (cn (format-judgments-h Js) ")")))

(define format-judgments-h
  {(list judgment) --> string}
  [] -> ""
  [J | Rest] -> (cn (format-judgment J) (cn " " (format-judgments-h Rest))))
