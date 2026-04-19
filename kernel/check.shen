\* check.shen — top-level entry point for the xpc type checker kernel

   Reads the IR (as Shen data structures) from stdin, runs all rules,
   and writes judgments to stdout as s-expressions.

   Usage: the Go binary serializes the World to Shen-readable s-expressions,
   pipes them in, and reads the judgment list back.

   Protocol:
     stdin:  a single s-expression (world CRDs XRDs Compositions Functions
             Providers Configurations Resources ArgoApps Schemas)
     stdout: a list of judgment s-expressions
             (judgments [(judgment Code Severity Source Message Detail Fix Related) ...]) *\

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
(load "r11-api-deprecation.shen")
(load "r6c-provider-wave.shen")
(load "r12-no-dangling-mount.shen")
(load "r13-no-immutable-change.shen")
(load "r14-no-rbac-regression.shen")
(load "r15-appproject-whitelist.shen")
(load "r16-selector-needs-ignore-diff.shen")

\* ===== IR reading ===== *\

\* The world is expected as:
   (world
     (crds ...)
     (xrds ...)
     (compositions ...)
     (functions ...)
     (providers ...)
     (configurations ...)
     (resources ...)
     (argo-apps ...)
     (schemas ...)) *\

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
         ArgoAppProjLinks (extract-section argo-app-proj-links Sections)
         ArgoAppProjects (extract-section argo-appprojects Sections)
         Schemas      (extract-section schemas Sections)
         ResolvedPatches (extract-section resolved-patches Sections)
         MountRefs    (extract-section mount-refs Sections)
         SARefs       (extract-section sa-refs Sections)
         RBACBindings (extract-section rbac-bindings Sections)
         RBACRules    (extract-section rbac-rules Sections)
         ImmutableFields (extract-section immutable-fields Sections)
         SelectorMappings (extract-section selector-mappings Sections)
         SelectorUsages (extract-section selector-usages Sections)
         IgnoreDiffEntries (extract-section ignore-diff-entries Sections)
         Trajectory   (extract-section trajectory Sections)

         \* Run all rules — each result is passed through mark-rule so that
            rules which ran without violations emit a satisfied marker
            judgment. The bridge filters those before returning Diagnostics
            but counts them for the audit-proof RunSummary. *\
         R1 (mark-rule "XPC001" (check-r1 CRDs XRDs))
         R2 (mark-rule "XPC002" (check-r2 Resources CRDs))
         R3 (mark-rule "XPC003" (check-r3 Compositions XRDs))
         R4 (mark-rule "XPC004" (check-r4 Compositions Functions))
         R5 (mark-rule "XPC005" (check-r5 ResolvedPatches))
         R6 (mark-rule "XPC006" (check-r6 ArgoApps Compositions XRDs Functions))
         R6c (mark-rule "XPC006" (check-r6c ArgoApps CRDs))
         R7 (mark-rule "XPC007" (check-r7 ArgoApps Compositions))
         R8 (mark-rule "XPC008" (check-r8 Resources XRDs))
         R9 (mark-rule "XPC009" (check-r9 Compositions Resources))
         R10 (mark-rule "XPC010" (check-r10 ResolvedPatches))
         R11 (mark-rule "XPC011" (check-r11 Resources Compositions Providers CRDs))
         R12 (mark-rule "XPC012" (check-r12 Trajectory MountRefs))
         R13 (mark-rule "XPC013" (check-r13 Trajectory ImmutableFields))
         R14 (mark-rule "XPC014" (check-r14 Trajectory SARefs RBACBindings))
         R15 (mark-rule "XPC.D.kind-whitelisted" (check-r15 ArgoApps ArgoAppProjLinks ArgoAppProjects Resources CRDs))
         R16 (mark-rule "XPC.E.selector-needs-ignore-diff" (check-r16 SelectorUsages IgnoreDiffEntries))

      (append R1 (append R2 (append R3 (append R4 (append R5
        (append R6 (append R6c (append R7 (append R8 (append R9 (append R10
          (append R11 (append R12 (append R13 (append R14 (append R15 R16)))))))))))))))))))

\* ===== Stdin/stdout protocol ===== *\

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
  _ -> "(judgment '???' error (source '' 0) 'malformed judgment' '' '' ())")

\* Format all judgments *\
(define format-judgments
  {(list judgment) --> string}
  Js -> (cn "(judgments " (cn (format-judgments-h Js) ")")))

(define format-judgments-h
  {(list judgment) --> string}
  [] -> ""
  [J | Rest] -> (cn (format-judgment J) (cn " " (format-judgments-h Rest))))
