---
date: 2026-04-21T14:45:27Z
researcher: Reuben Brooks
git_commit: d97bde3eda9085beb4d2157cd28c52001f79ebdc
branch: claude/phase1-cleanup
repository: pyrex41/cross-validate
topic: "Vision status after S1–S5 + R21: where are we on the three layers of the vision?"
tags: [research, vision, status, xpc, taxonomy, shen-as-spec, fg-manifold-coverage]
status: complete
last_updated: 2026-04-21
last_updated_by: Reuben Brooks
---

# Research: Where are we wrt accomplishing the vision?

**Date**: 2026-04-21 14:45 UTC
**Researcher**: Reuben Brooks
**Git Commit**: `d97bde3` (`claude/phase1-cleanup`, post-R21)
**Repository**: pyrex41/cross-validate

## Research Question
"and tell me where we are at wrt accomplishing the vision" — after shipping S1–S5 (R15–R20), the post-S5 fg-manifold replay, and R21.

## Summary

The vision has three nested layers, documented in three places. Status against each, in one sentence:

1. **Outer layer — bounded obligation taxonomy + completeness claim (ADR-001).** 22 rules shipped across **10 of 12 categories**; Category **I (provider-capability) has zero rules**, and Categories **D, E, F, G** have only 1 generator each out of 3–5 named in `docs/obligations.md`.
2. **Middle layer — Shen is the canonical spec, Go is IR + bridge + UI (ADR-002).** **Fully intact.** Every rule lives in `kernel/*.shen`; the obligation-framework Go code was deleted in Phase 1 cleanup; the bridge serializes a 20-section `World` s-expression and decodes judgments.
3. **Inner layer — ≥50% of the ~500-MR fg-manifold history caught so the tool is useful in CI (fg-manifold coverage plan).** **Paper coverage 77% (40% R17 + 20% R16 + 15% R21 + 2% R15).** Replay against 3 known-good tips validated that xpc runs cleanly (<2 min cold, no crashes), but **R15 emits a cartesian FP flood (64k errors from 288 real + 224 Applications)** that makes its 2% slice unusable in CI until fixed, and R21's 15% has not yet been replay-validated.

The architecture bet (Shen-as-spec) has held steady across the 5-session plan — every new rule dropped into the same slot without architectural friction. The coverage bet partially cashed: rules work, numbers on paper hit the target, but three gates stand between the paper claim and "useful in CI today": R15 cartesian attribution, render cache not populating (warm runs ≈ cold runs), and `xpc` can't find the kernel when invoked from outside its own repo tree.

---

## Layer 1 — Bounded obligation taxonomy (ADR-001)

**Where it's defined:** [`docs/adr/001-bounded-obligation-taxonomy.md`](docs/adr/001-bounded-obligation-taxonomy.md) (§3 superseded by ADR-002), [`docs/obligations.md`](docs/obligations.md) (generator reference).

**The completeness claim:** "For input I and cluster context C, if xpc returns no errors, no obligation in any modeled category is violated." Qualifiers: only *modeled* categories, only *modeled* primitives, only the simulated trajectory.

**Category-by-category status** (as of `d97bde3`, cross-referenced with `kernel/check.shen:16-38` and `docs/obligations.md`):

| Cat | Scope | Generators named in `obligations.md` | Shipped | File |
|-----|-------|----------|---------|------|
| **A** | Schema obligations (CRD × field) | `patch-source-type`, `patch-target-type`, `crossplane-machinery-placement`, `resource-field-valid` | 2/4: R5 (patch types, pre-resolved in Go), R17 (raw-manifest walk against CRD schema) | `kernel/r5-patch-typecheck.shen`, `kernel/r17-resource-field-valid.shen` |
| **B** | Reference resolution | `comp-xrd-ref`, `pipeline-fn-ref`, `patch-compat` | 3/3: R3, R4, R5 | `kernel/r3…shen`, `r4…shen`, `r5…shen` |
| **C** | Version coherence | `version-coherence`, `crossplane-machinery-version` | 1/2: R1 (R8 is a Go-side annotation passthrough) | `kernel/r1-versions.shen`, `r8-v1v2-machinery.shen` |
| **D** | AppProject constraints | `source-repo-allowed`, `destination-allowed`, `kind-whitelisted`, `sync-window-permitted` | 1/4: R15 | `kernel/r15-appproject-whitelist.shen` |
| **E** | Sync-option interactions | `replace-immutable-safety`, `ssa-field-manager-conflict`, `prune-target-exists`, `createnamespace-not-colliding`, `selector-needs-ignore-diff`, `late-init-needs-ignore-diff` | 2/6: R16, R21 | `kernel/r16-…`, `r21-…` |
| **F** | Trajectory invariants | `no-dangling-mount`, `no-immutable-change`, `no-rbac-regression`, `trajectory-wave-order`, `trajectory-bootstrap` | 4/5 shipped; **R13 dormant** (Delta.Updated always nil) | R6/R6c/R9/R12/R13/R14 |
| **G** | Cross-Application | `no-duplicate-ownership`, `cross-app-label-tracking`, `no-namespace-overlap` | 1/3: R7 | `kernel/r7-owner-refs.shen` |
| **H** | Rendering | `helm-renders`, `kustomize-renders`, `values-well-typed`, `render-deterministic` | 4/4: R18 (Helm + Kustomize generalized), R19, R20 | `kernel/r18-…`, `r19-…`, `r20-…` |
| **I** | Provider capability | `field-available-in-version`, `field-not-deprecated`, `controller-healthy` | **0/3** | — |
| **J** | Conversion cost | `conversion-cost-opt-in` | 1/1: R2 | `kernel/r2-conversion.shen` |
| **K** | Secret-flow | `secret-source-sink` | 1/1: R10 (substring lookup; the "taint lattice" framing in docs is aspirational per `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md` follow-up audit) | `kernel/r10-secret-taint.shen` |
| **L** | Deprecation calendar | `api-deprecation-calendar` | 1/1: R11 (hardcoded block-list, no date awareness) | `kernel/r11-api-deprecation.shen` |

**Rule inventory** (22 total, verified in [`kernel/check.shen:16-38`](kernel/check.shen#L16-L38)): R1, R2, R3, R4, R5, R6, R6c, R7, R8, R9, R10, R11, R12, R13, R14, R15, R16, R17, R18, R19, R20, R21.

**Totals against the taxonomy:** 10 of 12 categories have ≥1 shipped rule; 20 of ~30 named generators are implemented. Category I is the only category with zero rules.

**Honesty caveats flagged in prior research** (still current, per [`thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md`](thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md) §"Rule-by-rule audit"):

- **R8, R9 framework-only.** Shen side returns `[]`; the real detection is Go-side annotations (R8) or not yet wired (R9).
- **R10 is a ~20-path substring lookup,** not the information-flow-control taint lattice the taxonomy promises.
- **R11 is a 5-apiVersion + 2-provider-floor hardcoded list,** not a date-aware calendar.
- **R13 is dormant** until the trajectory simulator learns multi-snapshot diffing (`pkg/trajectory/trajectory.go`: `Delta.Updated` is always `nil`).

---

## Layer 2 — Shen-as-spec + trajectory simulator (ADR-002)

**Where it's defined:** [`docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md`](docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md), enacted in [`pkg/checker/bridge.go`](pkg/checker/bridge.go) and [`internal/shenfull/`](internal/shenfull/init.go).

**Status: fully intact.** Every one of the 22 rules is a Shen file under `kernel/`. There is no fallback Go rule code in the call path — `pkg/obligation/` was deleted during Phase 1 cleanup, and `pkg/checker/rules.go` was removed. The Go side has four jobs:

1. **Parse YAML → typed IR.** `pkg/loader/loader.go` + `pkg/ir/builder.go` produce `*types.World`.
2. **Enrich IR with Go-only facts.** `enrichSyncWaves`, `resolvePatchTypes`, `EnrichTrajectoryData`, `SelectorRegistry()`, `extractSelectorUsages`, `LateInitRegistry()`, `extractLateInitUsages`, `ValidateManifest`, `ExpandAppSet`, `renderer.Render` + cache, `trajectory.Simulate`.
3. **Translate `World` → Shen s-expression.** [`worldToShenObj`](pkg/checker/bridge.go#L341) emits 20 tagged sections; [`kernel/check.shen:65-90`](kernel/check.shen#L65-L90) extracts each with `extract-section`.
4. **Decode judgments → `[]types.Diagnostic`.** [`objToDiagnostics`](pkg/checker/bridge.go#L740) walks `(judgment Code Sev Src Msg Detail Fix Related)` tuples; `obligationRefForCode` re-attaches `(Category, Generator)` provenance for legacy XPC001–XPC014 codes.

**Sections emitted today** (`kernel/check.shen:65-90`): `crds`, `xrds`, `compositions`, `functions`, `providers`, `configurations`, `resources`, `argo-apps`, `argo-app-proj-links`, `argo-appprojects`, `schemas`, `resolved-patches`, `mount-refs`, `sa-refs`, `rbac-bindings`, `rbac-rules`, `immutable-fields`, `selector-mappings`, `selector-usages`, `late-init-mappings`, `late-init-usages`, `ignore-diff-entries`, `resource-field-facts`, `render-results`, `determinism-results`, `trajectory`. That's 26 sections (some tagged in the same extract-bundle).

**Trajectory simulator status** (`pkg/trajectory/simulate.go:27`):

- Sorts ArgoApps by name; buckets resources by sync-wave annotation; applies creates then hook-deletes per wave; emits `Step{AppName, Wave, Delta, State}` per wave.
- Drives R12 (no-dangling-mount) and R14 (no-rbac-regression) today.
- **Known limitation documented in code:** no cluster/project/label-selector scoping; `Delta.Updated` empty until multi-snapshot extension lands; `State` carries only keys. This is why R13 is dormant.

---

## Layer 3 — fg-manifold coverage (the pragmatic target)

**Where it's defined:** [`thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md`](thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md) ("Target: ≥50% of the ~500-MR history would have been caught"). MR-bucket taxonomy in [`thoughts/shared/research/2026-04-18-fg-manifold-target-study.md`](thoughts/shared/research/2026-04-18-fg-manifold-target-study.md).

### Paper coverage after R21 (77%)

From [`thoughts/shared/verify/r21-report.md`](thoughts/shared/verify/r21-report.md) "Coverage scoreboard":

| Wave | Rule | MR bucket | Coverage |
|------|------|-----------|---------:|
| S1 | R17 | CRD schema field mismatches | 40% |
| S2 | R16 | Selector → resolved-ref drift | 20% |
| S3 | R15 | AppProject whitelist misses | 2% |
| S4–S5 | R18–R20 | Rendering (structural, not in MR bucket) | — |
| post-S5 | R21 | Late-init field drift | 15% |
| **Total primary** | | | **77%** |

The uncovered ~23% distributes across smaller buckets (SSA × managementPolicies ~2–3%, external-name normalization ~1%, plus long-tail per-service fixes).

### Replay-validated status (as of 2026-04-21)

Source: [`thoughts/shared/verify/replay-results.md`](thoughts/shared/verify/replay-results.md).

- **3 tips of fg-manifold `main`** × cold + warm = 6 runs. All exited 1. Wall-time 1:14–1:34 (cold-budget <2 min met).
- **Identical diagnostic counts across all 6 runs.** Determinism: clean. Tip-sensitivity: nil (three adjacent merge commits don't differ in anything the rules look at).
- **R16 (20%)** — 374 diagnostics, all unique. "Quietly healthy"; vertical slice generalized well.
- **R15 (2%)** — 64,484 diagnostics from 288 unique messages × ~224 Applications. **Cartesian FP flood**: rule joins each resource against every Application rather than only the owning Application.
- **XPC006 (pre-plan)** — 1,980 diagnostics from 30 unique messages × ~66 multiplier. Same cartesian shape as R15; likely structural, not rule-specific.
- **R17 (40%)** — not separately broken out in the replay diagnostic table because S3 landed before the replay ran and was part of the "exit=1" count, but the replay's total is 66,913 and R15+XPC006+R16+AppSet+R18 account for 66,913. R17 produced zero diagnostics on these tips, which the replay notes could be because these tips don't exercise field-validation misses or could be a coverage gap for further investigation.
- **R18 (rendering)** — 34 render failures, all "Path required" for remote Helm charts (`{chart: X, repoURL: …, path: ""}` rejected by `pkg/renderer/`).
- **R21 (15%, late-init)** — **not exercised by the replay** (R21 shipped after the replay ran). The 77% claim needs a follow-up replay to validate.
- **Render cache** — `~/.cache/xpc/` absent after 6 runs. Warm runs not faster than cold. Violates S4's "hit rate >95% on second run" success criterion.

### Gates between paper-claim and "useful in CI"

Filed as followups in [`thoughts/shared/orchestration/xpc-fg-manifold-handoff.md`](thoughts/shared/orchestration/xpc-fg-manifold-handoff.md) §"Known follow-ups":

| # | Severity | Item |
|---|---|---|
| 1 | P0 | R15 n×m cartesian FP flood — 224× over-attribution per `(kind, group)` pair |
| 2 | P0 | XPC006 same-shape cartesian blowup |
| 3 | P1 | Render cache not populating (`pkg/renderer/cache.go:55` disk-tier write silent) |
| 4 | P1 | Remote Helm chart "Path required" failure (34 render failures on one tip) |
| 5 | P2 | No `xpc --kernel-path` flag — `resolveKernelPath` walks cwd upward only; can't run from outside the xpc repo tree |
| 6 | P2 | `preview-environments` AppSet uses Go-template syntax; 41 emissions of `XPC.H.appset-unsupported-generator` — expander upgrade needed |
| 7 | P3 | R21 registry cross-check against R15's 288 observed kinds; re-run replay with R21 online to validate 77% |
| — | — | CI integration doc (`gitlab-ci.yml` SARIF snippet) not yet written |

---

## Detailed Findings

### What "the vision" is, precisely

Three nested statements, each documented in its own file:

1. **[ADR-001](docs/adr/001-bounded-obligation-taxonomy.md) §4 (completeness claim):** "For input I and cluster context C, xpc enumerates every obligation in categories A–L against (I, C) and discharges each one. If xpc returns no errors, no obligation in any modeled category is violated by I against C."

2. **[ADR-002](docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md):** Shen is the canonical rule spec; Go owns IR, trajectory simulation, and the bridge. The obligation-framework Go code from ADR-001 §3 is superseded.

3. **[`thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md`](thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md) §"S5 success criteria":** "Total coverage assessment: recompute the MR-bucket hit-rate table from the research doc. Target: ≥50% of the ~500-MR history would have been caught."

### What's moved since the pre-plan state (`0ecd1db`)

Comparing the post-S5 + R21 state (`d97bde3`) against the `2026-04-18-so-what-have-we-actually-built.md` tour (`0ecd1db`):

- **14 rules → 22 rules.** R15 (S1), R16 (S2), R17 (S3), R18 + R19 (S4), R20 (S5), R21 (post-S5).
- **20 World sections → 26 World sections.** Added: `argo-appprojects`, `argo-app-proj-links`, `selector-mappings`, `selector-usages`, `late-init-mappings`, `late-init-usages`, `ignore-diff-entries`, `resource-field-facts`, `render-results`, `determinism-results`.
- **New Go packages:** `pkg/renderer/` (Helm + Kustomize + cache + values-schema + determinism), `pkg/ir/appset_expand.go` + `appset_template.go`, `pkg/schemas/index.go` + `validate_manifest.go`.
- **New CLI flags:** `--skip-render`, `--helm-bin=<path>`, `--appset-fixture=<file.yaml>`, `--skip-appset-expand`.
- **Audit proof version:** v3 → v4 (bumped during S3).
- **New ADR:** [ADR-003 "ApplicationSet expansion as offline simulation"](docs/adr/003-appset-expansion.md).

### What hasn't moved

- **Category I (provider-capability)** — still 0 rules. Named generators `field-available-in-version`, `field-not-deprecated`, `controller-healthy` unimplemented.
- **Most of Category D** — `source-repo-allowed`, `destination-allowed`, `sync-window-permitted` unimplemented (only `kind-whitelisted` via R15).
- **Most of Category E** — `replace-immutable-safety`, `ssa-field-manager-conflict`, `prune-target-exists`, `createnamespace-not-colliding` unimplemented (only `selector-needs-ignore-diff` + `late-init-needs-ignore-diff`).
- **Most of Category G** — `no-duplicate-ownership`, `no-namespace-overlap` unimplemented (only `cross-app-label-tracking` via R7).
- **R13 (no-immutable-change)** — dormant; requires multi-snapshot trajectory.
- **`xpc bisect`** — still a stub (`cmd/xpc/main.go:507-517` per prior research; not re-verified this session).

### Reusable surfaces delivered

Per [`thoughts/shared/orchestration/xpc-fg-manifold-handoff.md`](thoughts/shared/orchestration/xpc-fg-manifold-handoff.md) §"Reusable surfaces":

- **Schema machinery** — `pkg/schemas/{index,validate_manifest}.go` now shared by R17 (manifests) and R19 (values.schema.json).
- **Selector-style static registry** — `pkg/ir/selector_registry.go` (S2) + `pkg/ir/late_init_registry.go` (R21) demonstrate the "hand-curated table → enrichment pass → Shen substring check" pattern.
- **Renderer + cache** — `pkg/renderer/` interface, two-tier SHA-256 cache, absent-binary sentinel pattern, `"rendered:<tool>:<app>"` provenance convention.
- **AppSet expansion** — `ExpandAppSet(appset, ctx) []ArgoApplication` supports list / matrix / git-directories / merge; `pullRequest`/`scmProvider` via `--appset-fixture=`. Feeds the normal Application pipeline so downstream rules (R15, R16, R17, R21) get AppSet coverage automatically.
- **Bridge section pattern** — `sortedSection[T]` generic + lowercase-dashed symbol discriminators (not booleans) is the convention for every new fact type.

---

## Code References

- [`kernel/check.shen:16-38`](kernel/check.shen#L16-L38) — rule load list (22 rule files + prelude)
- [`kernel/check.shen:65-90`](kernel/check.shen#L65-L90) — World section extraction (26 named sections)
- [`kernel/check.shen:96-117`](kernel/check.shen#L96-L117) — `mark-rule` calls + the 22-closer append chain
- [`pkg/checker/bridge.go`](pkg/checker/bridge.go) — the bridge (Shen bootstrap, World serialization, judgment decoding)
- [`pkg/trajectory/simulate.go:27`](pkg/trajectory/simulate.go#L27) — trajectory simulator (powers R12/R13/R14)
- [`pkg/renderer/`](pkg/renderer/) — Helm + Kustomize + cache + values-schema + determinism
- [`pkg/ir/appset_expand.go`](pkg/ir/appset_expand.go) — ApplicationSet expansion
- [`pkg/ir/selector_registry.go`](pkg/ir/selector_registry.go) + [`pkg/ir/late_init_registry.go`](pkg/ir/late_init_registry.go) — the two static registries
- [`docs/obligations.md`](docs/obligations.md) — category-by-category generator list (authoritative reference for what each category promises)
- [`docs/adr/001-bounded-obligation-taxonomy.md`](docs/adr/001-bounded-obligation-taxonomy.md) — taxonomy + completeness claim
- [`docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md`](docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md) — Shen-as-spec
- [`docs/adr/003-appset-expansion.md`](docs/adr/003-appset-expansion.md) — AppSet expansion contract

---

## Historical Context (from thoughts/)

- [`thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md`](thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md) — the "vision has stayed constant; the implementation has flipped three times" recap, with the rule-by-rule honesty audit (R1–R11 classified as static/semi-dynamic/hollow).
- [`thoughts/shared/research/2026-04-18-so-what-have-we-actually-built.md`](thoughts/shared/research/2026-04-18-so-what-have-we-actually-built.md) — pre-plan (`0ecd1db`) architecture tour with 14 rules, 20 sections, 7 fixtures.
- [`thoughts/shared/research/2026-04-18-fg-manifold-target-study.md`](thoughts/shared/research/2026-04-18-fg-manifold-target-study.md) — MR-pain taxonomy that sets the 50% coverage target.
- [`thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md`](thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md) — the 5-session plan that executed S1–S5.
- [`thoughts/shared/verify/{s1,s2,s3,s4,s5}-report.md`](thoughts/shared/verify/) — per-session verify reports.
- [`thoughts/shared/verify/r21-report.md`](thoughts/shared/verify/r21-report.md) — R21 implementer report (coverage scoreboard 62% → 77%).
- [`thoughts/shared/verify/replay-results.md`](thoughts/shared/verify/replay-results.md) — post-S5 replay results that exposed the R15 cartesian flood.
- [`thoughts/shared/orchestration/xpc-fg-manifold-handoff.md`](thoughts/shared/orchestration/xpc-fg-manifold-handoff.md) — running orchestration log with wave table, followup queue, and gotcha ledger.

---

## Related Research

- This doc builds on [`2026-04-17-vision-recap-after-phase1-cleanup.md`](thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md) (4 days old) and [`2026-04-18-so-what-have-we-actually-built.md`](thoughts/shared/research/2026-04-18-so-what-have-we-actually-built.md) (3 days old), updating them to the post-R21 state.

---

## Open Questions

Carried over from prior research docs, still open:

1. **Categories D, E, F, G, I completeness.** Is the intended next direction to fill in the remaining generators under existing categories (SSA × managementPolicies for E; provider-capability for I; no-duplicate-ownership for G; multi-snapshot R13 for F), or is 22 rules across 10 categories considered "enough" for now?
2. **R13 activation** requires multi-snapshot trajectory diffing. Still gated on simulator extension.
3. **R8 / R9 status honesty.** Both have Shen rule files that return `[]`; real logic is Go-side annotation passthrough. Promote to real Shen rules or officially classify as Go-side only?
4. **R10 / R11 framing mismatch.** Docs promise "taint lattice" (R10) and "date-aware calendar" (R11); implementations are substring lookup + static block-list. Adjust framing in docs or expand implementations?
5. **`audit.Generate` and new-style codes.** Proof subtrees still keyed off legacy `XPC001`–`XPC014`; the new `XPC.<Cat>.<Gen>` codes from R15–R21 are carried but not all audit infrastructure consumes them.
6. **Replay validation of the 77% claim.** R21 shipped after the replay; a second replay against the same 3 tips with R21 online would convert the 15% late-init bucket from paper to validated.
