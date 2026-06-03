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
(load "r14-no-rbac-regression.shen")
(load "r15-appproject-whitelist.shen")
(load "r16-selector-needs-ignore-diff.shen")
(load "r17-resource-field-valid.shen")
(load "r18-helm-renders.shen")
(load "r19-values-well-typed.shen")
(load "r20-render-deterministic.shen")
(load "r21-late-init-needs-ignore-diff.shen")
(load "r22-ssa-managementpolicies-safety.shen")
(load "r23-crossplane-state-needs-orphan.shen")
(load "r24-appset-finalizer-without-preserve.shen")
(load "r25-prod-appset-autosync.shen")
(load "r28-providerconfig-resolves.shen")
(load "r29-fargate-claim-env-label.shen")
(load "r30-externalsecret-store.shen")
(load "r31-forprovider-canonical-form.shen")
(load "r32-observed-desired-fixed-point.shen")

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

\* rule-allowed? — true when Allowlist is empty (= run everything) or
   when Code appears in Allowlist. Empty-allowlist-means-all is what
   keeps default behavior identical for callers that don't pass a focus
   preset. The empty-list short-circuit must be on the OUTER call only;
   the recursive helper below distinguishes "no allowlist set" from
   "exhausted search without a hit". *\
(define rule-allowed?
  {string --> (list string) --> boolean}
  _ [] -> true
  Code Allowlist -> (rule-member? Code Allowlist))

(define rule-member?
  {string --> (list string) --> boolean}
  _ [] -> false
  Code [Code | _] -> true
  Code [_ | Rest] -> (rule-member? Code Rest))

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
         LateInitMappings (extract-section late-init-mappings Sections)
         LateInitUsages (extract-section late-init-usages Sections)
         IgnoreDiffEntries (extract-section ignore-diff-entries Sections)
         ResourceFieldFacts (extract-section resource-field-facts Sections)
         RenderResults (extract-section render-results Sections)
         DeterminismResults (extract-section determinism-results Sections)
         SSAMPConflicts (extract-section ssa-mp-conflicts Sections)
         SSAMPMode     (extract-section ssa-mp-mode Sections)
         CPDeletionPolicyFacts (extract-section crossplane-deletion-policy-facts Sections)
         AppSetFinalizerFacts (extract-section appset-finalizer-facts Sections)
         AppSetAutosyncFacts (extract-section appset-autosync-facts Sections)
         ProdPatterns (extract-section prod-patterns Sections)
         R23Carveouts (extract-section crossplane-state-needs-orphan-carveouts Sections)
         Allowlist    (extract-section rule-allowlist Sections)
         R12Violations (extract-section r12-violations Sections)
         R14Violations (extract-section r14-violations Sections)
         R15Violations (extract-section r15-violations Sections)
         R16Violations (extract-section r16-violations Sections)
         R21Violations (extract-section r21-violations Sections)
         R28Violations (extract-section providerconfig-ref-violations Sections)
         R29Violations (extract-section fargate-env-label-violations Sections)
         R30Violations (extract-section eso-store-violations Sections)
         R31Violations (extract-section canonical-form-violations Sections)
         R32Violations (extract-section fixed-point-violations Sections)
         Trajectory   (extract-section trajectory Sections)

         \* Run all rules — each result is passed through mark-rule so that
            rules which ran without violations emit a satisfied marker
            judgment. The bridge filters those before returning Diagnostics
            but counts them for the audit-proof RunSummary.
            Each dispatch is gated by rule-allowed? so a non-empty allowlist
            (e.g. --focus=inc6-floor) skips the entire check-rN call rather
            than running it and dropping the output afterwards. *\
         R1 (if (rule-allowed? "XPC001" Allowlist) (mark-rule "XPC001" (check-r1 CRDs XRDs)) [])
         R2 (if (rule-allowed? "XPC002" Allowlist) (mark-rule "XPC002" (check-r2 Resources CRDs)) [])
         R3 (if (rule-allowed? "XPC003" Allowlist) (mark-rule "XPC003" (check-r3 Compositions XRDs)) [])
         R4 (if (rule-allowed? "XPC004" Allowlist) (mark-rule "XPC004" (check-r4 Compositions Functions)) [])
         R5 (if (rule-allowed? "XPC005" Allowlist) (mark-rule "XPC005" (check-r5 ResolvedPatches)) [])
         R6 (if (rule-allowed? "XPC006" Allowlist) (mark-rule "XPC006" (check-r6 ArgoApps Compositions XRDs Functions)) [])
         R6c (if (rule-allowed? "XPC006" Allowlist) (mark-rule "XPC006" (check-r6c ArgoApps CRDs)) [])
         R7 (if (rule-allowed? "XPC007" Allowlist) (mark-rule "XPC007" (check-r7 ArgoApps Compositions)) [])
         R8 (if (rule-allowed? "XPC008" Allowlist) (mark-rule "XPC008" (check-r8 Resources XRDs)) [])
         R9 (if (rule-allowed? "XPC009" Allowlist) (mark-rule "XPC009" (check-r9 Compositions Resources)) [])
         R10 (if (rule-allowed? "XPC010" Allowlist) (mark-rule "XPC010" (check-r10 ResolvedPatches)) [])
         R11 (if (rule-allowed? "XPC011" Allowlist) (mark-rule "XPC011" (check-r11 Resources Compositions Providers CRDs)) [])
         R12 (if (rule-allowed? "XPC012" Allowlist) (mark-rule "XPC012" (check-r12-cross R12Violations)) [])
         R14 (if (rule-allowed? "XPC014" Allowlist) (mark-rule "XPC014" (check-r14-cross R14Violations)) [])
         R15 (if (rule-allowed? "XPC.D.kind-whitelisted" Allowlist) (mark-rule "XPC.D.kind-whitelisted" (check-r15 R15Violations)) [])
         R16 (if (rule-allowed? "XPC.E.selector-needs-ignore-diff" Allowlist) (mark-rule "XPC.E.selector-needs-ignore-diff" (check-r16 R16Violations)) [])
         R17 (if (rule-allowed? "XPC.A.resource-field-valid" Allowlist) (mark-rule "XPC.A.resource-field-valid" (check-r17 ResourceFieldFacts)) [])
         R18 (if (rule-allowed? "XPC.H.helm-renders" Allowlist) (mark-rule "XPC.H.helm-renders" (check-r18 RenderResults)) [])
         R19 (if (rule-allowed? "XPC.H.values-well-typed" Allowlist) (mark-rule "XPC.H.values-well-typed" (check-r19 RenderResults)) [])
         R20 (if (rule-allowed? "XPC.H.render-deterministic" Allowlist) (mark-rule "XPC.H.render-deterministic" (check-r20 DeterminismResults)) [])
         R21 (if (rule-allowed? "XPC.E.late-init-needs-ignore-diff" Allowlist) (mark-rule "XPC.E.late-init-needs-ignore-diff" (check-r21 R21Violations)) [])
         R22 (if (or (rule-allowed? "XPC.E.ssa-managementpolicies-observe" Allowlist)
                     (or (rule-allowed? "XPC.E.ssa-managementpolicies-partial" Allowlist)
                         (rule-allowed? "XPC.E.ssa-managementpolicies-nondefault" Allowlist)))
                  (mark-r22-rules (check-r22 SSAMPConflicts SSAMPMode))
                  [])
         R23 (if (rule-allowed? "XPC.S.crossplane-state-needs-orphan" Allowlist) (mark-rule "XPC.S.crossplane-state-needs-orphan" (check-r23 CPDeletionPolicyFacts R23Carveouts)) [])
         R24 (if (rule-allowed? "XPC.E.appset-finalizer-without-preserve" Allowlist) (mark-rule "XPC.E.appset-finalizer-without-preserve" (check-r24 AppSetFinalizerFacts)) [])
         R25 (if (rule-allowed? "XPC.E.prod-appset-autosync" Allowlist) (mark-rule "XPC.E.prod-appset-autosync" (check-r25 AppSetAutosyncFacts ProdPatterns)) [])
         R28 (if (rule-allowed? "XPC.B.providerconfig-resolves" Allowlist) (mark-rule "XPC.B.providerconfig-resolves" (check-r28 R28Violations)) [])
         R29 (if (rule-allowed? "XPC.E.fargate-claim-env-label" Allowlist) (mark-rule "XPC.E.fargate-claim-env-label" (check-r29 R29Violations)) [])
         R30 (if (rule-allowed? "XPC.K.externalsecret-store" Allowlist) (mark-rule "XPC.K.externalsecret-store" (check-r30 R30Violations)) [])
         R31 (if (rule-allowed? "XPC.M.forprovider-canonical-form" Allowlist) (mark-rule "XPC.M.forprovider-canonical-form" (check-r31 R31Violations)) [])
         R32 (if (rule-allowed? "XPC.M.observed-desired-fixed-point" Allowlist) (mark-rule "XPC.M.observed-desired-fixed-point" (check-r32 R32Violations)) [])

      (append R1 (append R2 (append R3 (append R4 (append R5
        (append R6 (append R6c (append R7 (append R8 (append R9 (append R10
          (append R11 (append R12 (append R14 (append R15 (append R16 (append R17 (append R18 (append R19 (append R20 (append R21 (append R22 (append R23 (append R24 (append R25 (append R28 (append R29 (append R30 (append R31 R32)))))))))))))))))))))))))))))))

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
