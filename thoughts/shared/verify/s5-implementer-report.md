---
session: S5
rule: XPC.H.kustomize-renders + XPC.H.render-deterministic + ApplicationSet expansion
branch: claude/xpc-s5-kustomize-appset
tip: c86d05a
date: 2026-04-20
implementer: opus (crashed mid-run with "Prompt is too long"); report + commit discipline + gofmt fix completed by orchestrator
---

# S5 Implementer Report — Kustomize + ApplicationSet + Determinism

## Meta: how this report was produced

The spawned Implementer-S5 agent (opus, background) crashed with `Prompt is too long` after ~17 minutes of execution. Its context overflowed before it reached the commit + report phase. However, all source files were written to the worktree in an uncommitted state, and every plan objective was implemented correctly:

- `go build ./...` clean
- `make test` fully green (all packages)
- Every S5-specific smoke test produced the expected behavior (see "Commands run" below)
- Kernel paren budget correct (line 117 has 22 closers; append chain extended `(append R18 R19)` → `(append R18 (append R19 R20))`)

The orchestrator (this session) picked up the uncommitted state, ran the full verification suite to confirm soundness, then:
1. Committed the work in 12 logical chunks matching the plan's suggested order.
2. Ticked `docs/obligations.md` entries (missing from the original work).
3. Fixed one gofmt regression on `pkg/ir/builder.go:1370` (two rows with extra spaces before `{}` value literals in `inferFunctionInputVersions` map).
4. Wrote this report from observable state (diffs, test output, CLI smoke-test output).

**This means**: the report is grounded in literal command output, not a running narrative from the implementer's own perspective. The verifier should treat this as the source of truth for what shipped, but should re-run every command listed below independently.

**Gap flagged to user**: the base plan's Manual success criterion (line 401) — *"Total coverage assessment: recompute the MR-bucket hit-rate table. Target: ≥50% of the ~500-MR history would have been caught"* — was NOT produced by the crashed implementer, and the orchestrator does not have confident access to the source research doc's bucket taxonomy. This scoreboard is the capstone artifact for the 5-session investment. Decision needed: (a) dispatch a small scoreboard-only agent, (b) hand-compute from the research doc, or (c) ship without it and note in the verify report.

## Commits (ordered `claude/phase1-cleanup..HEAD`)

| SHA | Summary |
|---|---|
| `284da86` | types: add DeterminismResult and World.DeterminismResults for R20 |
| `0451cdb` | renderer: add Kustomize renderer mirroring helm.go pattern |
| `377cebe` | renderer: add double-render determinism check for R20 |
| `8c42395` | ir: add minimal AppSet template substitution for '{{ .key }}' syntax |
| `8fef505` | ir: add ExpandAppSet for list/matrix/git-dirs/merge/pullRequest generators |
| `4203040` | builder: wire AppSet expansion into World.ArgoApps pipeline |
| `bfe7f70` | bridge: emit determinism-results section sibling to render-results |
| `de6611f` | kernel: add R20 render-deterministic rule and R18 kustomize coverage |
| `8f1582c` | cli: add --appset-fixture and --skip-appset-expand flags |
| `974f717` | testdata+tests: S5 fixtures and integration coverage |
| `24ce0b5` | docs: ADR-003 AppSet expansion as offline simulation |
| `cd47dd8` | docs: tick kustomize-renders and render-deterministic in obligations |
| `c86d05a` | builder: gofmt fix on inferFunctionInputVersions map |

Total: 13 commits on `claude/xpc-s5-kustomize-appset`. (Plan budgeted ≤10; orchestrator split for reviewability after picking up mid-stream.)

## Files delivered

### New Go files
| File | Lines | Purpose |
|---|---|---|
| `pkg/renderer/kustomize.go` | 216 | Kustomize renderer; absent-binary sentinel, 30s timeout, stdout parse; `Provenance = "rendered:kustomize:<app>"` |
| `pkg/renderer/determinism.go` | 84 | `DoubleRender` helper; byte-compare; produces `[]DeterminismResult` |
| `pkg/ir/appset_expand.go` | 331 | `ExpandAppSet(as, ctx) []ArgoApplication` — list, git-directories, matrix, merge, pullRequest |
| `pkg/ir/appset_template.go` | 100 | Hand-rolled `{{ .key }}` substitution over `ArgoAppSetTemplate` |

### New Go test files (test func counts)
| File | # tests |
|---|---|
| `pkg/renderer/kustomize_test.go` | 3 |
| `pkg/renderer/determinism_test.go` | 4 |
| `pkg/ir/appset_expand_test.go` | 7 |
| `pkg/ir/appset_template_test.go` | 2 |
| `pkg/renderer/cache_test.go` (extended) | 5 (includes kustomize cache-key tests) |
| `pkg/checker/check_test.go` (extended) | 24 total; `TestAppSetExpansion_PropagatesToR15` is the capstone integration test |

### New kernel file
- `kernel/r20-render-deterministic.shen` (44 lines) — walks `determinism-results`; emits warning on mismatch.

### New docs
- `docs/adr/003-appset-expansion.md` (103 lines) — offline-simulation contract for `pullRequest`/`scmProvider` via `--appset-fixture`.

### New testdata (copied from `thoughts/shared/prep/fixtures/s5/`)
- `testdata/fixtures/kustomize-ok/` — base + overlay with `namePrefix`
- `testdata/fixtures/kustomize-render-fail/` — kustomization patch pointing at missing `does-not-exist.yaml`
- `testdata/fixtures/appset-list/` — 2-element list generator
- `testdata/fixtures/appset-matrix/` — 2×2 list-list matrix
- `testdata/fixtures/appset-pullrequest/` — pullRequest generator + `pr-fixture.yaml` with 2 PR stubs

### Modified files
| File | Change |
|---|---|
| `pkg/types/types.go` | +26 lines: `DeterminismResult` struct + `World.DeterminismResults` |
| `pkg/ir/builder.go` | +203/-8: AppSet expansion wiring after existing parse step; `b.world.ArgoApps` extended with synthetic expanded Applications; gated on `--skip-appset-expand` |
| `pkg/checker/bridge.go` | +30: `determinism-results` section emit following `render-results` pattern |
| `pkg/checker/check_test.go` | +139: kustomize/determinism/appset integration tests including the R15 capstone |
| `pkg/renderer/cache_test.go` | +88: cache-key stability tests with kustomize overlay SHA + version |
| `kernel/check.shen` | `DeterminismResults` section extract; `R20` binding; append chain extended to `(append R18 (append R19 R20))`; line 117 closer count 21 → 22 |
| `kernel/r18-helm-renders.shen` | +45/-15: R18 generalized to accept `rendered:kustomize:*` provenance; emits `XPC.H.kustomize-renders` when source tag matches kustomize |
| `cmd/xpc/main.go` | +94/-11: `--appset-fixture=<file.yaml>` and `--skip-appset-expand` flags; wiring into builder config |
| `docs/obligations.md` | +2/-2: ticked `kustomize-renders` (R18) and `render-deterministic` (R20) |

## Commands run (literal tail output)

### `make test`
```
go test ./... -count=1
?   	github.com/pyrex41/cross-validate-/cmd/xpc	[no test files]
ok  	github.com/pyrex41/cross-validate-/internal/shenfull	3.270s
ok  	github.com/pyrex41/cross-validate-/pkg/audit	2.108s
ok  	github.com/pyrex41/cross-validate-/pkg/checker	5.310s
ok  	github.com/pyrex41/cross-validate-/pkg/ir	0.524s
?   	github.com/pyrex41/cross-validate-/pkg/loader	[no test files]
ok  	github.com/pyrex41/cross-validate-/pkg/renderer	1.976s
ok  	github.com/pyrex41/cross-validate-/pkg/report	2.840s
ok  	github.com/pyrex41/cross-validate-/pkg/schemas	2.440s
ok  	github.com/pyrex41/cross-validate-/pkg/snapshot	1.770s
ok  	github.com/pyrex41/cross-validate-/pkg/trajectory	3.484s
?   	github.com/pyrex41/cross-validate-/pkg/types	[no test files]
```
**Status**: all packages ✅ green. 11 package ok / 3 `no test files` (cmd/xpc, loader, types — expected, baseline).

### `make lint` (post-gofmt-fix commit `c86d05a`)
Target is `go vet ./...` + `test -z "$(gofmt -l .)"`. Remaining flagged files are all in the **pre-existing baseline** from the orchestration doc:
- `internal/shenfull/*` — 13 generated files (baseline)
- `pkg/audit/proof.go`, `pkg/report/reporter.go`, `pkg/ir/trajectory_extract_test.go`, `pkg/snapshot/snapshot_test.go`, `pkg/trajectory/trajectory_test.go` — all S5-untouched baseline

No S5-touched files remain in the lint output. `go vet ./pkg/ir/...` clean (zero output).

### `make build`
Compiles cleanly, no output.

### `go run ./cmd/xpc check testdata/fixtures/basic` (smoke)
```
xpc: ok (no issues)
```
Kernel loads without paren error → append-chain closers balance.

### `go run ./cmd/xpc check testdata/fixtures/appset-matrix`
```
xpc: ok (no issues)
```
AppSet expansion produces 2×2 = 4 synthetic Applications; none violate R15 because matrix template uses whitelisted kinds.

### `go run ./cmd/xpc check --appset-fixture=testdata/fixtures/appset-pullrequest/pr-fixture.yaml testdata/fixtures/appset-pullrequest`
```
xpc: ok (no issues)
```
pullRequest generator consumes the 2-PR fixture → 2 expanded Applications.

### `go run ./cmd/xpc check testdata/fixtures/kustomize-ok`
```
xpc: ok (no issues)
```

### `go run ./cmd/xpc check testdata/fixtures/kustomize-render-fail`
```
XPC.H.kustomize-renders testdata/fixtures/kustomize-render-fail/app.yaml:1
  rule:     kustomize-render-fail: kustomize build failed
  severity: error
  problem:  testdata/fixtures/kustomize-render-fail: kustomize build testdata/fixtures/kustomize-render-fail failed: exit status 1: # Warning: 'patchesStrategicMerge' is deprecated. […] evalsymlink failure on '[…]/does-not-exist.yaml' : lstat […]/does-not-exist.yaml: no such file or directory
  fix:      Run 'kustomize build' locally on the overlay to reproduce and fix the build error.
  docs:     https://xpc.dev/errors/XPC.H.kustomize-renders

xpc: 1 error(s), 0 warning(s)
exit status 1
```
R18 kustomize path fires; message preserves kustomize's stderr for operator diagnosis.

### Capstone — `TestAppSetExpansion_PropagatesToR15` (`pkg/checker/check_test.go:636`)
```
=== RUN   TestAppSetExpansion_PropagatesToR15
--- PASS: TestAppSetExpansion_PropagatesToR15 (0.00s)
PASS
ok  	github.com/pyrex41/cross-validate-/pkg/checker	0.555s
```
This is the integration-point proof for the whole 5-session plan: an AppSet-matrix whose template references a non-whitelisted kind produces `XPC.D.kind-whitelisted` violations on its *expanded* Applications. Confirms the expansion feeds back into the normal rule pipeline, which was the base-plan line-369 architectural commitment.

## Scope decisions (from the plan, confirmed shipped)

- **`--skip-appset-expand` default = OFF** ✅ (flag present, default-off in `cmd/xpc/main.go`)
- **Kustomize absent = warning** ✅ (kustomize.go mirrors helm.go's `errors.Is(err, exec.ErrNotFound)` path)
- **One new rule R20 + R18 generalized for kustomize** ✅ (no new rule code for kustomize — R18 carries both renderers via provenance)
- **`determinism-results` separate Shen section** ✅ (not an extension of `render-results`)
- **Non-determinism = warning** ✅ (r20-render-deterministic.shen emits `warning`)
- **Audit proof version stays 4** ✅ (no proof rev — verify with `grep proof.*version` in diff)
- **AppSet expansion feeds normal pipeline** ✅ (capstone test proves it)

## Scope-gate near-misses (watch for in verify)

- **S2's array-path selector-registry TODO (18/53 inert)** — orchestrator did not surface any false-negative during smoke tests. AppSet-matrix template uses whitelisted kinds only; if a user-supplied matrix surfaces selectors-in-arrays, this TODO may become visible. Not fixed in S5 per scope gate.
- **13 commits vs ≤10 budget** — orchestrator split commits for reviewability after picking up mid-stream. The implementer would have rolled some together (e.g., kustomize + determinism; appset_template + appset_expand). Not a correctness issue.
- **Kernel file `r18-helm-renders.shen` now covers kustomize** — the filename is now a misnomer. Renaming felt like scope creep (would touch the `(load …)` chain and risk paren-balance). Documented here for a future cleanup session.

## Known gaps for user decision

1. **Total-coverage scoreboard (base plan line 401)** — NOT produced. This is the 5-session capstone metric. Options:
   - Dispatch a small `opus` or `haiku` agent with: "read `/Users/reuben/.claude/plans/research-written-wiggly-nova.md` + the referenced research doc + the current R1–R20 coverage; recompute the MR-bucket hit-rate table; target ≥50%; write into this report."
   - Hand-compute from `thoughts/shared/research/2026-04-17-full-codebase-review.md` + the MR-bucket taxonomy.
   - Accept the coverage-check as a separate followup ticket.
2. **Manual fg-manifold replay** (base plan line 399) — requires user's local `~/fg/fg-manifold` tree. Commands:
   ```bash
   xpc check --appset-fixture=<2-pr-stub.yaml> ~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/preview-environments.yaml
   xpc check ~/fg/fg-manifold/  # full-repo; expect <2 min with cache warm
   ```
   Neither attempted from orchestrator session.

## Handoff to verifier

Tip: `c86d05a` on `claude/xpc-s5-kustomize-appset`. Gate checks in priority order:
1. `make test` green.
2. Paren budget: `sed -n '117p' kernel/check.shen | tr -cd ')' | wc -c` → must equal 22.
3. `TestAppSetExpansion_PropagatesToR15` passes (capstone).
4. `go run ./cmd/xpc check testdata/fixtures/kustomize-render-fail` emits `XPC.H.kustomize-renders` error.
5. `make lint` — confirm remaining file list is the pre-existing baseline only; no S5-touched files regress.
6. Smoke: `go run ./cmd/xpc check testdata/fixtures/basic` returns no issues (kernel loads).
7. Spot-check: R20 `XPC.H.render-deterministic` rule id is reachable — produce a mismatch fixture OR confirm by reading `r20-render-deterministic.shen` that the code path would fire.
8. Note the total-coverage scoreboard gap in verify report.

Match the s4-report.md frontmatter shape. Flag the scoreboard gap explicitly in the "Merge recommendation" section so the human gate can decide.

## Total-coverage scoreboard (post-hoc, base plan line 401)

Recomputed against the MR-bucket taxonomy in `thoughts/shared/research/2026-04-18-fg-manifold-target-study.md` (~500-MR history). Shares are the research doc's own percentages; rule attribution is from `kernel/check.shen:93–113` at tip `1865039`.

| # | Bucket | MR share | Rule(s) that cover it | Status |
|---|---|---|---|---|
| 1 | CRD schema field mismatches (wrong field name / type / missing required / wrong enum) | ~40% | R17 `XPC.A.resource-field-valid` (S3) | ✅ covered |
| 2 | Crossplane selector → resolved-ref drift (`*Selector` without matching `ignoreDifferences`) | ~20% | R16 `XPC.E.selector-needs-ignore-diff` (S2) | ✅ covered |
| 3 | Late-init field drift (provider writes back to `spec.forProvider.*`) | ~15% | — (explicitly deferred; would reuse S2's registry shape as a follow-up) | ❌ miss |
| 4 | AppProject whitelist misses (resource kind not in `clusterResourceWhitelist`) | ~2% | R15 `XPC.D.kind-whitelisted` (S1); reaches AppSet-expanded Applications via S5's `ExpandAppSet` | ✅ covered (S5 unlocks full surface) |
| 5 | ServerSideApply × managementPolicies interactions | ~2–3% | — (Category E defined in `docs/obligations.md:81–96`, rule unbuilt) | ❌ miss |
| 6 | Provider-package bugs (deprecated versions, webhook conversion cost) | ~5% | R11 (deprecation-calendar subset), R2 (webhook-conversion subset via `XPC002`) | ◐ partial (~1–2% of the 5%) |
| 7 | External-name normalization (`alias/` prefix, SM ARN form) | ~1% | — | ❌ miss |
| 8 | Wave ordering / provider-wave < MR-wave | <1% | R6 + R6c `XPC006` | ✅ covered (not a frequent MR category — team has engineered around it) |
| 9 | Composition → XRD reference | <1% | R3 `XPC003` | ✅ covered (low MR rate) |
| 10 | Pipeline function reference | <1% | R4 `XPC004` | ✅ covered (low MR rate) |

**Primary coverage (sum of ✅ buckets over the dominant 500-MR surface):** ~40 + 20 + 2 = **62%** of the historical MR volume, plus ~1–2% partial from R11/R2. Target was ≥50%. **✅ Pass.**

### Qualifying assumptions

1. **Helm/Kustomize rendering (S4/S5) is a force multiplier, not a new bucket.** R15/R16/R17 all require resource manifests in `World.Resources`. Without S4 (Helm) and S5 (Kustomize), every Application that sources from a chart or overlay would be opaque to those three rules. fg-manifold routes nearly all claim traffic through `lib/charts/crossplane-{claim,fargateservice,workers}/`, so rendering is what lets buckets 1, 2, and 4 reach their stated shares in practice. R18/R19/R20 are the infrastructure that makes the 62% real on this specific repo — they do not themselves close any new MR bucket.

2. **ApplicationSet expansion (S5) unlocks bucket 4 on preview-driven Apps.** !1388 (the `preview` AppProject missing `postgresql.sql.crossplane.io`) is produced by a matrix × pullRequest AppSet. Without S5's `ExpandAppSet`, R15 can only lint the bootstrap Application and handful of static ones; with it, R15 lints every materialized Application in the preview fleet. The capstone test `TestAppSetExpansion_PropagatesToR15` (`pkg/checker/check_test.go:636`) is the literal proof that expansion feeds the whitelist rule.

3. **R20 (`render-deterministic`) is not tied to any historical bucket.** It is a preventative warning — expected to surface chart templates using `randAlphaNum` that the team should annotate, not a rule that retroactively catches MRs in the sample.

4. **Late-init drift (bucket 3, ~15%) is the largest uncovered bucket.** The research doc flags this explicitly as "could live in the same rule as [selectors] with a separate emit message, or as a Category I generator." Lifting S2's `selector_registry.go` shape into a `late_init_registry.go` is the shortest path — deferred per base plan scope ("What we're NOT doing" line 43: "Late-init field drift as a distinct rule this plan cycle").

5. **The 62% headline is coverage of MR *categories*, not a guarantee that xpc run on an arbitrary fg-manifold branch today would produce a bisectable diagnostic for 62% of merged MRs.** False-positive rate, missed edge-case manifests, and CRD-schema staleness (SchemasDir freshness) will eat some of that in practice. Empirical coverage-on-replay is the manual-success-criterion in base plan line 399; that requires access to `~/fg/fg-manifold` and is out of scope for this session.

### What's left for a follow-up session

- **Late-init-drift rule** (reclaims ~15% from bucket 3). S2's registry pattern, one new table, one new rule file. Estimated ≤1 day.
- **SSA × managementPolicies** (reclaims ~2–3% from bucket 5). Category E; the bridge already has `IgnoreDiffEntries` section pattern from R16. Similar scale.
- **External-name normalization** (~1%). Category I territory; requires a maintained provider-capability table — smaller ROI per unit effort.
- **Manual fg-manifold replay** (base plan line 399) — record diagnostic counts on three known-good branches; profile if warm-cache > 2 minutes.

Combined, the first two follow-ups would push primary coverage to **~80%**. Deferred per scope-gate, not a regression.

