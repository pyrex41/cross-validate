\* r22-ssa-managementpolicies-safety.shen — Rule family R22:
   XPC.E.ssa-managementpolicies-{observe,partial,nondefault}

   Flag managed resources whose owning Argo Application enables
   syncPolicy.syncOptions.ServerSideApply AND whose spec has a
   managementPolicies combination that conflicts with SSA.

   Three sub-codes fire under progressively broader modes:
     -observe    managementPolicies is ONLY "Observe" — clearest bug.
                 Fires under every mode.
     -partial    managementPolicies includes write ops (Create/LateInitialize/
                 Delete) but OMITS "Update". Fires at mode >= partial.
     -nondefault any non-default managementPolicies. Fires at mode = any.

   Mode is read from the `ssa-mp-mode` section emitted by the bridge.
   An empty section defaults to `observe` — the narrowest setting.

   Fact shape (from pkg/checker/bridge.go ssaMPConflictToObj):
     [ssa-mp-conflict-fact AppName SsaSym Policies Group Kind Name Ns Src]
   where SsaSym is the lowercase-dashed symbol `ssa-yes` or `ssa-no`
   (NEVER a Shen boolean — uppercase would collide with Shen's
   pattern-variable convention), and Policies is a list of the raw
   managementPolicies strings as declared on the resource.

   The Go extractor already drops rows whose owning app has SSA=false,
   so in practice SsaSym here is always `ssa-yes` — the pattern match
   still gates on it for belt-and-braces safety.

   NOTE on `cn`: Shen's `cn` takes exactly 2 string arguments. Calling
   it with 3+ args results in partial application of a string (which is
   not a function) causing a runtime panic. Every `cn` call here is
   strictly 2-argument; longer concatenations are written as nested
   `(cn s1 (cn s2 s3))` chains. *\


\* r22-member? — plain list membership. Returns true when S equals any
   element of Xs. *\
(define r22-member?
  _ [] -> false
  S [S | _] -> true
  S [_ | Rest] -> (r22-member? S Rest))


\* r22-all-observe? — true when every policy in Ps is the literal
   "Observe". The empty list also qualifies (explicit empty = "do
   nothing", equivalent to Observe-only for the SSA-conflict question). *\
(define r22-all-observe?
  [] -> true
  ["Observe" | Rest] -> (r22-all-observe? Rest)
  _ -> false)


\* r22-has-write-op? — true when the policy list contains any write op
   (Create, LateInitialize, Delete). Update is a write too but we
   specifically gate -partial on its *absence*, so it is listed
   separately in r22-has-update?. *\
(define r22-has-write-op?
  Ps -> (if (r22-member? "Create" Ps)
            true
            (if (r22-member? "LateInitialize" Ps)
                true
                (r22-member? "Delete" Ps))))


(define r22-has-update?
  Ps -> (r22-member? "Update" Ps))


\* r22-is-default? — true when Ps contains all four core policies,
   treating that as the Crossplane default. LateInitialize is
   implementation-specific so we don't require it here. *\
(define r22-is-default?
  Ps -> (if (r22-member? "Observe" Ps)
            (if (r22-member? "Create" Ps)
                (if (r22-member? "Update" Ps)
                    (r22-member? "Delete" Ps)
                    false)
                false)
            false))


\* Mode-gating helpers. The mode symbols are observe / partial / any
   (lowercase, as emitted by the bridge). We compare with `=` to avoid
   any ambiguity about symbol vs. variable in patterns. *\
(define r22-mode-at-least-partial?
  Mode -> (if (= Mode partial)
              true
              (= Mode any)))


(define r22-mode-at-least-any?
  Mode -> (= Mode any))


\* r22-mode-of — unwrap the one-element mode list. Empty list means no
   mode fact was emitted (test harness edge case) — default to observe. *\
(define r22-mode-of
  [] -> observe
  [Mode | _] -> Mode)


\* Emission paths. Each cn call is strictly 2 arguments; longer strings
   are built as nested (cn s1 (cn s2 s3)) chains.
   All three carry source-loc from the MR, not the Application — that's
   where the author has to edit managementPolicies. *\
(define r22-emit-observe
  AppName Kind Name Src ->
    (make-error "XPC.E.ssa-managementpolicies-observe"
      Src
      (cn Kind (cn "/" (cn Name (cn " on Application " (cn AppName
        " uses managementPolicies=[Observe] but owning Application has ServerSideApply=true")))))
      (cn "Crossplane will NOT write this resource's fields (managementPolicies is Observe-only) "
        (cn "but Argo CD's ServerSideApply will continue to reconcile the spec against the git-declared "
          "manifest. The two tools are giving contradictory instructions for the same fields."))
      (cn "Either drop ServerSideApply from spec.syncPolicy.syncOptions on Application "
        (cn AppName ", or widen managementPolicies to include the write ops (Create/Update/Delete) you want Argo to apply."))
      []))


(define r22-emit-partial
  AppName Kind Name Src ->
    (make-error "XPC.E.ssa-managementpolicies-partial"
      Src
      (cn Kind (cn "/" (cn Name (cn " on Application " (cn AppName
        " has managementPolicies that include writes but omit Update, with ServerSideApply=true")))))
      (cn "managementPolicies lists write operations (Create/LateInitialize/Delete) but not Update. "
        (cn "Crossplane will create and delete, but won't reconcile field drift. "
          "Argo CD's ServerSideApply will still push Update events, violating the narrowed policy."))
      (cn "Add Update to managementPolicies on this resource, or drop ServerSideApply from "
        (cn AppName "'s spec.syncPolicy.syncOptions."))
      []))


(define r22-emit-nondefault
  AppName Kind Name Src ->
    (make-error "XPC.E.ssa-managementpolicies-nondefault"
      Src
      (cn Kind (cn "/" (cn Name (cn " on Application " (cn AppName
        " has a non-default managementPolicies with ServerSideApply=true")))))
      (cn "Any narrowing of managementPolicies from the Crossplane default interacts with "
        (cn "Argo CD's ServerSideApply in ways that are hard to reason about locally. "
          "This broad diagnostic catches residual skew after -observe and -partial have been audited."))
      (cn "Review whether ServerSideApply is truly needed on Application " (cn AppName
        " given the narrowed managementPolicies, or restore the default managementPolicies."))
      []))


\* r22-check-row — dispatch one conflict fact. Returns a list of
   judgments (0, 1, or 2 entries — the same row may qualify for both
   -partial and -nondefault under mode=any).
   For the observe case, short-circuit: if all policies are Observe the
   -partial/-nondefault paths cannot apply.
   For the non-observe case, walk incrementally: start with [] and cons
   each judgment that qualifies. *\
(define r22-check-row
  [ssa-mp-conflict-fact AppName ssa-yes Policies Group Kind Name Ns Src] ModeSym ->
    (if (r22-all-observe? Policies)
        [(r22-emit-observe AppName Kind Name Src)]
        (let Acc0 []
          (let Acc1
                (if (r22-mode-at-least-partial? ModeSym)
                    (if (r22-has-write-op? Policies)
                        (if (r22-has-update? Policies)
                            Acc0
                            [(r22-emit-partial AppName Kind Name Src) | Acc0])
                        Acc0)
                    Acc0)
            (if (r22-mode-at-least-any? ModeSym)
                (if (r22-is-default? Policies)
                    Acc1
                    [(r22-emit-nondefault AppName Kind Name Src) | Acc1])
                Acc1))))
  _ _ -> [])


\* check-r22 — top-level R22 check.
   SSAMPConflicts: list of ssa-mp-conflict-fact tuples.
   ModeList: one-element list from `(extract-section ssa-mp-mode …)`. *\
(define check-r22
  SSAMPConflicts ModeList ->
    (let ModeSym (r22-mode-of ModeList)
      (r22-check-all SSAMPConflicts ModeSym [])))


\* r22-check-all — explicit tail-recursive accumulator replacing
   map+flatten to avoid any shen-go lambda capture quirks. *\
(define r22-check-all
  [] _ Acc -> (xpc-reverse Acc)
  [Row | Rest] ModeSym Acc ->
    (let RowJudgments (r22-check-row Row ModeSym)
      (r22-check-all Rest ModeSym (r22-prepend-rev RowJudgments Acc))))


\* r22-prepend-rev — prepend Xs (in reverse order) onto Acc.
   Net effect: the rows from Xs are appended to the eventual output
   since r22-check-all reverses the accumulator at the end. *\
(define r22-prepend-rev
  [] Acc -> Acc
  [X | Rest] Acc -> (r22-prepend-rev Rest [X | Acc]))


\* r22-filter-by-code — pick only the judgments whose code matches.
   Used to partition R22's mixed output before `mark-rule` so the
   per-code satisfied markers come out right. *\
(define r22-filter-by-code
  _ [] -> []
  Code [[judgment Code Sev Src Msg Detail Fix Related] | Rest] ->
    [[judgment Code Sev Src Msg Detail Fix Related] | (r22-filter-by-code Code Rest)]
  Code [_ | Rest] -> (r22-filter-by-code Code Rest))


\* mark-r22-rules — apply mark-rule to each of the three R22 sub-codes
   using the shared R22All judgments, then flatten into a single list.
   Keeping this helper outside check.shen avoids growing check.shen's
   let-binding count, which the shen-go runtime appears to cap. *\
(define mark-r22-rules
  R22All ->
    (let Observe  (mark-rule "XPC.E.ssa-managementpolicies-observe"
                    (r22-filter-by-code "XPC.E.ssa-managementpolicies-observe" R22All))
         Partial  (mark-rule "XPC.E.ssa-managementpolicies-partial"
                    (r22-filter-by-code "XPC.E.ssa-managementpolicies-partial" R22All))
         NonDef   (mark-rule "XPC.E.ssa-managementpolicies-nondefault"
                    (r22-filter-by-code "XPC.E.ssa-managementpolicies-nondefault" R22All))
      (append Observe (append Partial NonDef))))
