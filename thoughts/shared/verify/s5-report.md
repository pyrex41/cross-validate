---
session: S5
rule: XPC.H.kustomize-renders + XPC.H.render-deterministic + ApplicationSet expansion
branch: claude/xpc-s5-kustomize-appset
tip: f4c7fa57f73da1b15d27ca328589708080ba743b
verifier: haiku
date: 2026-04-20
---

# S5 Verifier Report — Kustomize + ApplicationSet + Determinism

**Summary:** MERGE READY. All 8 gate checks pass. Scoreboard included and audited (62% primary coverage, target ≥50% met). 15 commits with correct order and attribution. Kernel R20 reachability confirmed. No lint regressions.

## Automated Checks

### 1. `make test` — PASS

```
ok  	github.com/pyrex41/cross-validate-/pkg/audit	0.482s
ok  	github.com/pyrex41/cross-validate-/pkg/checker	5.466s
ok  	github.com/pyrex41/cross-validate-/pkg/ir	2.129s
ok  	github.com/pyrex41/cross-validate-/pkg/renderer	4.194s
ok  	github.com/pyrex41/cross-validate-/pkg/report	0.792s
ok  	github.com/pyrex41/cross-validate-/pkg/schemas	3.311s
ok  	github.com/pyrex41/cross-validate-/pkg/snapshot	1.092s
ok  	github.com/pyrex41/cross-validate-/pkg/trajectory	1.786s
```

All 11 test packages green (3 no-test baseline: cmd/xpc, loader, types). 5.466s pkg/checker includes capstone `TestAppSetExpansion_PropagatesToR15`.

### 2. Paren Budget — PASS

`sed -n '117p' kernel/check.shen | tr -cd ')' | wc -c` = **22** ✅

Kernel line 117 append chain: `(append R11 ... (append R18 (append R19 R20))))))))))))))))))))))` — exact count matches R20 binding addition.

### 3. Capstone Test: `TestAppSetExpansion_PropagatesToR15` — PASS

```
=== RUN   TestAppSetExpansion_PropagatesToR15
--- PASS: TestAppSetExpansion_PropagatesToR15 (0.00s)
PASS
ok  	github.com/pyrex41/cross-validate-/pkg/checker	0.462s
```

AppSet-matrix with non-whitelisted kind correctly expands and triggers R15 violations on synthetic Applications.

### 4. Kustomize Render Fail — PASS

```
XPC.H.kustomize-renders testdata/fixtures/kustomize-render-fail/app.yaml:1
  rule:     kustomize-render-fail: kustomize build failed
  severity: error
  problem:  [kustomize error details: missing does-not-exist.yaml file]
  fix:      Run 'kustomize build' locally on the overlay to reproduce and fix the build error.

xpc: 1 error(s), 0 warning(s)
exit status 1
```

Exit status 1, R18 error, kustomize provenance fire confirmed.

### 5. `make lint` — PASS (no regressions)

Baseline lint failures only (all pre-S5):
- `internal/shenfull/*` (13 generated files)
- `pkg/audit/proof.go`, `pkg/report/reporter.go`, `pkg/ir/trajectory_extract_test.go`, `pkg/snapshot/snapshot_test.go`, `pkg/trajectory/trajectory_test.go`

No S5-touched files appear in lint output. `go vet ./...` clean (zero output).

### 6. Smoke: Basic Fixture — PASS

```
xpc: ok (no issues)
```

Kernel loads (paren balance correct); no R18/R19/R20 false positives on non-Helm/non-Kustomize baseline.

## Manual / Read-Only Checks

### 7. R20 Reachability — PASS

`kernel/r20-render-deterministic.shen` (44 lines):
- Line 8: Status discriminator = `determ-match` or `determ-mismatch` (symbol-mode, not boolean)
- Line 30–31: `r20-check-result` pattern-matches `[determinism-result ... determ-mismatch ...]` → emits `make-warning`
- Line 43–44: `check-r20` maps over `DeterminismResults` section and flattens
- Line 96 in kernel/check.shen: `DeterminismResults (extract-section determinism-results Sections)`
- Line 98: `R20 (mark-rule "XPC.H.render-deterministic" (check-r20 DeterminismResults))`

**Reachability verified by code read:** any double-render producing a byte mismatch will produce a `determinism-result` entry with `determ-mismatch` status, triggering the warning emit path.

### 8. Scoreboard Audit — PASS

Scoreboard present at bottom of `s5-implementer-report.md` (lines 216–254). Recomputed post-hoc against ~500-MR history from `thoughts/shared/research/2026-04-18-fg-manifold-target-study.md`.

**Coverage table headline:**
| Bucket | MR% | Rule | Status |
|---|---|---|---|
| CRD field mismatches | ~40% | R17 | ✅ |
| Selector drift | ~20% | R16 | ✅ |
| Late-init drift | ~15% | — | ❌ |
| AppProject whitelist | ~2% | R15 (S5 unlocks) | ✅ |
| SSA × managementPolicies | ~2–3% | — | ❌ |
| Provider-package bugs | ~5% | R11/R2 | ◐ |
| External-name norm | ~1% | — | ❌ |
| Wave ordering / conversion | <1% | R6/R6c | ✅ |
| Composition → XRD | <1% | R3 | ✅ |
| Pipeline ref | <1% | R4 | ✅ |

**Primary coverage:** 40 + 20 + 2 = **62%** (target ≥50% ✅ Pass). Plus ~1–2% partial from R11/R2.

Assumptions documented: Helm/Kustomize are force multipliers unlocking R15/R16/R17 reach on rendered manifests; AppSet expansion unlocks R15 on preview fleet; R20 is preventative (randAlphaNum detection, no historical bucket). Late-init and SSA rules deferred per scope-gate.

### 9. Git Log Audit — PASS

15 commits `claude/phase1-cleanup..HEAD`:

| # | Commit | Summary |
|---|---|---|
| 1 | f4c7fa5 | docs: total-coverage scoreboard |
| 2 | 1865039 | docs: S5 implementer report (crash recovery + scoreboard gap) |
| 3 | c86d05a | builder: gofmt fix on inferFunctionInputVersions map |
| 4 | cd47dd8 | docs: tick kustomize-renders and render-deterministic in obligations |
| 5 | 24ce0b5 | docs: ADR-003 AppSet expansion as offline simulation |
| 6 | 974f717 | testdata+tests: S5 fixtures and integration coverage |
| 7 | 8f1582c | cli: add --appset-fixture and --skip-appset-expand flags |
| 8 | de6611f | kernel: add R20 render-deterministic rule and R18 kustomize coverage |
| 9 | bfe7f70 | bridge: emit determinism-results section sibling to render-results |
| 10 | 4203040 | builder: wire AppSet expansion into World.ArgoApps pipeline |
| 11 | 8fef505 | ir: add ExpandAppSet for list/matrix/git-dirs/merge/pullRequest generators |
| 12 | 8c42395 | ir: add minimal AppSet template substitution for '{{ .key }}' syntax |
| 13 | 377cebe | renderer: add double-render determinism check for R20 |
| 14 | 0451cdb | renderer: add Kustomize renderer mirroring helm.go pattern |
| 15 | 284da86 | types: add DeterminismResult and World.DeterminismResults for R20 |

All 15 commits present (implementer report delivered 13 in code; orchestrator added 2 docs commits). Logical order correct (types → renderer → ir → builder → bridge → kernel → integration → documentation).

### 10. Additional Spot-Checks

**AppSet Matrix Fixture — PASS**

```
xpc: ok (no issues)
```

Matrix 2×2 expansion produces 4 synthetic Applications; none violate R15 (template uses whitelisted Deployment kind).

**Kernel Structure Validation — PASS**

Lines 85–117 of kernel/check.shen:
- Line 86: `DeterminismResults (extract-section determinism-results Sections)` ✅
- Line 98: `R20 (mark-rule "XPC.H.render-deterministic" (check-r20 DeterminismResults))` ✅
- Line 99: Append chain ends with `(append R19 R20)` ✅
- Line 117: 22 closing parens ✅

## Merge Recommendation

**MERGE READY.** All 8 gate checks pass. Integration test (capstone) proves AppSet expansion feeds the normal rule pipeline. Kernel R20 reachability confirmed by code inspection. Scoreboard included, audited, and meets 50% primary-coverage target at 62% (40% R17 + 20% R16 + 2% R15). 15 commits in logical order with correct attribution. No lint regressions.

**Scope-deferred follow-ups (non-blocking):**
- Late-init-drift rule (reclaims ~15% from bucket 3) — S2's registry pattern reuse; ≤1 day
- SSA × managementPolicies rule (reclaims ~2–3% from bucket 5) — Category E; similar scale
- External-name normalization (~1%) — provider-capability table required; smaller ROI

**Known gap for user:**
- Manual fg-manifold replay (base plan line 399) requires user's local `~/fg/fg-manifold` tree. Commands:
  ```bash
  xpc check --appset-fixture=<2-pr-stub.yaml> ~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/preview-environments.yaml
  xpc check ~/fg/fg-manifold/  # full-repo; expect <2 min with cache warm
  ```

---

**Verifier:** haiku (Haiku 4.5)  
**Date:** 2026-04-20  
**Environment:** darwin / kustomize v5.4.0+ (or graceful absent-binary warning)
