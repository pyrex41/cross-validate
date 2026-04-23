# xpc → fg-manifold Coverage — Orchestration Handoff

Running state of the 5-session team-of-agents plan. Update after each wave.

## Plans (external, do not duplicate)

- **Base plan** (source of truth): `/Users/reuben/.claude/plans/research-written-wiggly-nova.md` — defines S1–S5, success criteria, target MRs.
- **Orchestration meta-plan**: `/Users/reuben/.claude/plans/users-reuben-claude-plans-research-writ-virtual-shell.md` — team shape, wave pipeline, dispatch cheat sheet.

## Wave status

| Wave | Session | Status | Branch | Notes |
|---|---|---|---|---|
| 0 | Makefile + prep artifacts | ✅ merged | `claude/phase1-cleanup` @ `64fa2f3` | Makefile, `thoughts/shared/prep/{s2,fixtures/{s3,s4,s5}}/` committed |
| 1 | S1 — XPC.D.kind-whitelisted (R15) | ✅ merged | `claude/phase1-cleanup` @ `849a129` | 6 impl commits + 1 verify report. Verify report: `thoughts/shared/verify/s1-report.md` |
| 2 | S2 — XPC.E.selector-needs-ignore-diff (R16) | ✅ merged | `claude/phase1-cleanup` @ `994e052` | 6 impl commits + verify report + frontmatter fixup. Verify report: `thoughts/shared/verify/s2-report.md`. Registry: 53 entries (35 scalar-active + 18 array-path TODO). |
| 3 | S3 — XPC.A.resource-field-valid (R17) | ✅ merged | `claude/phase1-cleanup` @ `11ecb9c` | 8 impl commits + verify report. Verify report: `thoughts/shared/verify/s3-report.md`. Bumped audit proof v3→v4. New: `pkg/schemas/{index,validate_manifest}.go`, `pkg/ir/field_validation.go`, `kernel/r17-resource-field-valid.shen`. Array-path walker implemented inline in `validate_manifest.go` (partially addresses S2's TODO — see gotchas). |
| 4 | S4 — XPC.H.helm-renders + values-well-typed | ✅ merged | `claude/phase1-cleanup` @ `e3fe53b` | 8 impl commits + implementer report + verify report. Verify report: `thoughts/shared/verify/s4-report.md`. New: `pkg/renderer/{renderer,cache,helm,values_schema,file_util}.go`, `kernel/r18-helm-renders.shen`, `kernel/r19-values-well-typed.shen`, `--skip-render` / `--helm-bin=<path>` CLI flags. Two scope expansions: bridge emits `render-ok` / `render-failed` symbols (Shen booleans didn't pattern-match reliably); `pkg/loader/loader.go` now skips `templates/` dirs adjacent to `Chart.yaml` to keep raw Go-template YAML from breaking the decoder. |
| post-S5a | fg-manifold replay (validate 62% claim) | ✅ committed | `claude/build-xpc-type-checker-TfgsT` @ `45d8f9c` | 3 known-good tips × 2 runs (cold+warm). Report: `thoughts/shared/verify/replay-results.md`. 8 followup tickets filed (P0: R15/XPC006 cartesian FP flood; P1: render cache not populating, remote-Helm "Path required"). |
| post-S5b | R21 — XPC.E.late-init-needs-ignore-diff | ✅ committed | `claude/build-xpc-type-checker-TfgsT` @ pending | Vertical slice mirrors S2/R16. Registry seeded from fg-manifold MRs !1048, !893, !1502 via `glab`. Primary coverage 62% → 77%. Report: `thoughts/shared/verify/r21-report.md`. |
| 5 | S5 — Kustomize + AppSet + determinism | ✅ merged | `claude/phase1-cleanup` @ `86b33fa` | 15 impl commits (13 code + 2 orchestrator docs) + verify report. Verify report: `thoughts/shared/verify/s5-report.md`. Implementer crashed mid-run (`Prompt is too long`); orchestrator committed already-written source in 12 logical chunks, fixed gofmt regression on `pkg/ir/builder.go`, wrote implementer report, appended total-coverage scoreboard. New: `pkg/renderer/{kustomize,determinism}.go`, `pkg/ir/{appset_expand,appset_template}.go`, `kernel/r20-render-deterministic.shen`, `--appset-fixture=` / `--skip-appset-expand` CLI flags, `docs/adr/003-appset-expansion.md`. R18 generalized to carry both Helm and Kustomize provenance (filename now a misnomer; rename deferred). Capstone integration test `TestAppSetExpansion_PropagatesToR15` proves expansion feeds the normal rule pipeline. |

## Dispatch recipe (validated on S1)

**DO NOT** use `Agent(isolation: "worktree")` on this repo — the tool's default base is `origin/HEAD = claude/build-xpc-type-checker-TfgsT` (pre-Shen-runtime, wrong architecture). See `~/.claude/projects/-Users-reuben-projects-cross-validate/memory/feedback_agent_worktree_base.md`.

Correct pattern for each session:

```bash
# 1. Pre-create a worktree with explicit base
git worktree add .claude/worktrees/sN-impl -b claude/xpc-sN-<slug> claude/phase1-cleanup

# 2. Dispatch implementer without isolation parameter, telling it to cd into the pre-made worktree
#    as its first action. Include a sanity check: "wc -l pkg/checker/bridge.go should be ~896+"
```

Gate each session with a separate verifier agent (Haiku is enough). Verifier writes `thoughts/shared/verify/sN-report.md` and does NOT modify source. Human gate reviews the report before merge.

After merge:
```bash
git checkout claude/phase1-cleanup
git merge --ff-only claude/xpc-sN-<slug>
cp <worktree>/thoughts/shared/verify/sN-report.md thoughts/shared/verify/
git add thoughts/shared/verify/sN-report.md && git commit ...
git worktree remove -f -f <worktree-path>
git branch -d claude/xpc-sN-<slug>
```

## Plan complete — 2026-04-20

Five waves merged. Every rule from the base plan shipped green; S5 closed out Category H and added ApplicationSet expansion. `claude/phase1-cleanup` tip: `86b33fa`. Full commit log spans Waves 0 → 5; 14 rules now wired (R1–R14 pre-plan, R15–R20 added by this plan).

### Rule inventory at plan close

| Code | Rule | Wave | File |
|---|---|---|---|
| XPC001–XPC014 | (pre-existing, R1–R14) | pre-plan | `kernel/r1*.shen` … `r14.shen` |
| XPC.D.kind-whitelisted | R15 AppProject whitelist | S1 | `kernel/r15-appproject-whitelist.shen` |
| XPC.E.selector-needs-ignore-diff | R16 selector→ignore-diff cross-check | S2 | `kernel/r16-selector-needs-ignore-diff.shen` |
| XPC.A.resource-field-valid | R17 raw manifest vs CRD schema | S3 | `kernel/r17-resource-field-valid.shen` |
| XPC.H.helm-renders + values-well-typed | R18 + R19 Helm rendering | S4 | `kernel/r18-helm-renders.shen`, `r19-values-well-typed.shen` |
| XPC.H.kustomize-renders | R18 (generalized) | S5 | shared with Helm; provenance-discriminated |
| XPC.H.render-deterministic | R20 double-render byte-compare | S5 | `kernel/r20-render-deterministic.shen` |

Audit proof version was bumped once (S3: v3 → v4); stayed at v4 through S4 and S5 per plan budget.

### Total-coverage scoreboard (capstone metric, base plan line 401)

Recomputed against the ~500-MR fg-manifold history in `thoughts/shared/research/2026-04-18-fg-manifold-target-study.md`. Full bucket-by-bucket breakdown in `thoughts/shared/verify/s5-implementer-report.md` "Total-coverage scoreboard" section.

| Bucket | MR share | Status at plan close |
|---|---|---|
| CRD schema field mismatches | ~40% | ✅ R17 |
| Selector → resolved-ref drift | ~20% | ✅ R16 |
| AppProject whitelist misses | ~2% | ✅ R15 (S5 AppSet expansion extends surface to preview fleet) |
| Wave / Composition / Function ref | <3% combined | ✅ R6 / R6c / R3 / R4 |
| Provider-package bugs | ~5% | ◐ partial (R11 deprecation-calendar subset + R2 webhook-conversion subset) |
| Late-init field drift | ~15% | ✅ R21 (post-S5); array-path lift 2026-04-22 activates the 13 previously-inert registry rows on top of the scalar ones |
| SSA × managementPolicies | ~2–3% | ✅ R22 landed 2026-04-22 — `--ssa-mp-mode={observe,partial,any}` flag, three sub-codes (`XPC.E.ssa-managementpolicies-{observe,partial,nondefault}`), mode-gated |
| External-name normalization | ~1% | ❌ deferred (Category I, unbuilt) |

**Primary coverage: ~62% of historical MR volume** at plan close (target was ≥50%). Post-plan additions take it to **~80%** (R21 late-init +15%; R22 SSA×managementPolicies +2–3%, both merged). The 2026-04-22 array-path lift doesn't add a new bucket but multiplies R16+R21 signal fidelity on the activated rows. Rendering (S4/S5) is a force multiplier — without it, R15/R16/R17 would see only direct-manifest Applications, missing most of fg-manifold's claim-driven workloads. R20 is preventative, not tied to a historical bucket.

### Reusable surfaces delivered by the plan

- **Schema machinery** — `pkg/schemas/{index,validate_manifest}.go` (S3). Now shared by R17 (raw manifests) and R19 (Helm values.schema.json); ready for any future "validate JSON against stored OpenAPI schema" rule.
- **Selector registry pattern** — `pkg/ir/selector_registry.go` (S2). Static table shape; a future `late_init_registry.go` drops in with the same extraction hook.
- **Renderer + cache** — `pkg/renderer/{renderer,cache,helm,kustomize,determinism,values_schema,file_util}.go` (S4 + S5). Content-addressed two-tier cache; `Renderer` interface; absent-binary warning sentinel pattern; `"rendered:<tool>:<app>"` provenance convention.
- **AppSet expansion** — `pkg/ir/{appset_expand,appset_template}.go` (S5). `ExpandAppSet(appset, ctx) []ArgoApplication` for list / matrix / git-directories / merge. `pullRequest` + `scmProvider` consume injected fixtures via `--appset-fixture=<file.yaml>`. Feeds the normal Application pipeline so downstream rules (R15, R16, R17) gain coverage automatically. Documented in `docs/adr/003-appset-expansion.md`.
- **Bridge section pattern** — one `sortedSection[T]` per fact type, lowercase-dashed symbol discriminators for success/failure (never booleans). Five new sections shipped: `argo-appprojects`, `argo-app-proj-links`, `selector-usages`, `ignore-diff-entries`, `resource-field-facts`, `render-results`, `determinism-results`.

### Known follow-ups (non-blocking, ordered by ROI per the scoreboard)

1. ~~**Late-init-drift rule**~~ — ✅ shipped as R21 post-S5; registry has 7 rows across 3 kinds seeded from MRs !1048, !893, !1502. Report: `thoughts/shared/verify/r21-report.md`. Replay-v2 validated: 12 diagnostics per fg-manifold tip, tip-invariant.
2. ~~**R15 n×m cartesian FP flood**~~ — ✅ fixed in replay-v2: 64,484 → 700 (92× reduction) via `ResourceInfo.OwningApp` + `r15-owned-by?` filter. Every R15 diagnostic now blamed on its actual owning Application. Report: `thoughts/shared/verify/replay-results-v2.md`.
3. ~~**XPC006 cartesian blowup**~~ — ✅ shipped 2026-04-21 via commit `3530f0c`; replay-v3 dropped 1,980 → 30 diagnostics per tip (66× reduction). `OwningApp` now threaded through `XRDInfo` / `CompositionInfo` / `FunctionInfo` / `ProviderInfo` facts; kernel `xpc006-owned-by?` filter mirrors the R15 pattern. Remaining 30 are genuine wave-ordering constraints on `function-*` vs Compositions, correctly scoped to their owning Applications. Report: `thoughts/shared/verify/replay-results-v3.md`.
4. ~~**Render cache not populating**~~ — ✅ silent-failure fixed (`pkg/renderer/cache.go` now MkdirAlls eagerly, surfaces write errors, zeroes `DiskDir` on failure). Unit tests cover both success and read-only-parent paths. `~/.cache/xpc/renders/` now exists after first run. With #5 resolved, disk hits on the fg-manifold workload are now observable on a warm replay with `--helm-cache-dir` set.
5. ~~**Remote Helm chart "Path required"**~~ — ✅ fixed via `--helm-cache-dir=<dir>` + `HelmRenderer.PullRemote` (`helm pull --repo --version --destination tmp --untar`, then rename into `<cacheDir>/charts/<sha256(RepoURL/Chart/TargetRevision)>`). `ResolveChart` now returns `renderer.ErrRemoteChart` when `src.Path == ""`; the builder catches it and invokes `PullRemote`, or emits `helm-remote-unsupported` when the flag is absent. Unblocks warm-cache perf measurement on fg-manifold. Commit: `fa027fb`.
6. ~~**xpc `--kernel-path` flag**~~ — ✅ `--kernel-path=<dir>` + `XPC_KERNEL_PATH` env var now supported. Replay-v2 ran from `/tmp` cwd to prove it works outside the xpc repo tree.
7. ~~**SSA × managementPolicies rule**~~ — ✅ R22 landed 2026-04-22 via Sonnet pickup of the Opus overflow. Root cause of the prior `can't apply object` panic: Shen's `cn` is strictly 2-argument; `(cn a b c)` partially applies and then tries to invoke the resulting string as a function. Fix nested all `cn` chains strictly pairwise, replaced `map`+`flatten` with explicit tail-recursive accumulator, swapped nested `and`/`or` for sequential `if` chains, swapped symbol-pattern mode gating for `(= Mode sym)`. Three tests pass (observe under all modes, partial-default-suppressed, safe-under-all-modes). Commits: `a15af7b` (Go side) + `fd0934f` (kernel + tests).
8. ~~**S2's array-path selector-registry TODO**~~ — ✅ shipped 2026-04-22 via `pkg/ir/path_walk.go` (`WalkPath` utility) wired into `extractSelectorUsages` + `extractLateInitUsages`. All 13 previously-inert registry rows now activate. New fixture `testdata/fixtures/selector-drift-array/` + `TestR16_SelectorDrift_ArrayPath` asserts 6 diagnostics across array elements. Commits: `2df298f` + `85bf746` + `ba7fdce`.
9. **External-name normalization** — ~1% of MRs; requires a maintained provider-capability table.
10. **R18/r18-helm-renders.shen rename** — file now covers both Helm and Kustomize; pure hygiene.
11. ~~**Manual fg-manifold replay (v3)**~~ — ✅ re-executed 2026-04-22 against the same 3 main tips. Results: `thoughts/shared/verify/replay-results-v3.md`. XPC006 prediction held (1,980 → ~30, −98%); `XPC.H.helm-renders` stayed at 34 because `helm template` still fails on the pulled remote charts (pull works, template fails — new followup #12 below). Warm-cache chart-pull hit rate = 100% / 13 MB / 19 charts; render cache stays empty because template-step never completes.
12. ~~**CI integration doc**~~ — ✅ shipped 2026-04-22 as `docs/ci-integration.md` + `docs/templates/gitlab-ci.yml` (GitLab SAST via `--format=sarif`). Commit: `80c1b2f`.
13. ~~**R18 template-failure triage**~~ — ✅ xpc-side observability fix shipped 2026-04-22. `pkg/renderer/subprocessErrTail` now propagates real helm/kustomize stderr (with stdout fallback, 4 KiB cap) into the `XPC.H.{helm,kustomize}-renders` Detail field. Regression tests: `TestRenderChart_PropagatesHelmStderr` + extended `TestR18_HelmRenders/helm-render-fail` assert the tail survives end-to-end. Re-run against fg-manifold HEAD surfaces the next layer of root causes (see `replay-results-v3.md` §#4b update): unresolved `{{provider}}/{{region}}/{{cluster}}` template vars in AppSet-expanded `$values` refs (22/35 — new followup #14) and missing local `lib/charts/crossplane-*` paths (13/35 — new followup #15).
14. ~~**AppSet Helm-field template substitution**~~ — ✅ shipped 2026-04-22. `pkg/ir/appset_expand.go` `substituteSource` now calls a new `substituteHelm` helper that walks `Helm.ValueFiles`, `Helm.Values`, `Helm.ReleaseName`, and each `Helm.Parameters[].Name`/`.Value` through `substituteTemplate`. The helper returns a fresh `*ArgoHelmSource` so the AppSet template is never mutated in place across parameter sets. `ValuesObject` (nested map) deferred — no current fg-manifold signal. Tests: `TestExpandAppSet_SubstitutesHelmValueFiles` + `TestExpandAppSet_HelmSubstitutionDoesNotMutateTemplate`. Expected replay-v4 delta: 22/35 placeholder-leak `helm-renders` failures should disappear; the 13/35 local-chart failures stay (see #15). Stale comment "we intentionally don't substitute into Helm values" replaced.
15. ~~**Missing fg-manifold `lib/charts/crossplane-*` paths**~~ — ✅ closed 2026-04-22 as fg-manifold repo-state artifact. `ls /Users/reuben/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/lib/charts/` returns "No such file or directory" at all three replayed tips (`441fb679a`, `2ca71f228`, `4dd584566`). `ResolveChart` at `pkg/renderer/renderer.go:42` joins `src.Path` to `filepath.Dir(appFile)` verbatim; no xpc-side prefix-strip to blame. Stays as 12 residual `XPC.H.helm-renders` diagnostics per tip until fg-manifold adds or removes those source references.
16. ~~**Argo `$values` multi-source resolution**~~ — ✅ shipped 2026-04-22 via commit `b6ab26b`. `ArgoSource.Ref` added; `pkg/ir/values_ref.go` resolves `$<ref>/...` prefixes against sibling sources (explicit `Path` wins; otherwise walks up from the appfile to find `.git`). Info-level diagnostics `XPC.H.values-ref-{unknown,remote}` for the fallback paths. Replay-v5 (`thoughts/shared/verify/replay-results-v5.md`): 21 `$values` leaks → 0; `XPC.H.helm-renders` 34 → 15/tip. 12 `lib/charts/crossplane-*` remain (followup #15), 1 renovate pull protocol mismatch, 2 newly-surfaced rendered-YAML duplicate-key parse errors (content issue on fg-manifold side, not xpc). R15 jumped 700 → 1,018 as ~19 charts' rendered resources now flow into the World — masked-signal unmask, not a regression.

### Gotcha ledger (still load-bearing for any future rule additions)

- **Worktree default base is wrong** — pre-create worktrees with explicit base `claude/phase1-cleanup` (see `feedback_agent_worktree_base.md` memory).
- **Shen uppercase identifiers are variables** — emit lowercase-dashed symbols from Go as fact discriminators; never `true`/`false`.
- **Shen `check-world` paren discipline** — each new `append` adds exactly one `)` to the trailing line. Off-by-one yields `Panic: &{22}` at kernel load. Smoke with `go run ./cmd/xpc check testdata/fixtures/basic` after every kernel edit.
- **Shen string literals do not support `\"`** — use `cn` concatenation over pre-built quote-free segments.
- **Shen `cn` is strictly 2-argument** — `(cn a b c)` does NOT concatenate three strings. It partially applies `cn` to `(a, b)`, yielding a string, then tries to apply *that string* to `c` and panics with `can't apply object`. Always nest pairwise: `(cn a (cn b c))`. R22's initial Opus attempt crashed on this exact pattern; see commit `fd0934f`.
- **Prelude `string-contains?`** — arg order is `(Haystack Needle)`.
- **Loader skips `templates/`** adjacent to `Chart.yaml` (`pkg/loader/loader.go`) — the carve-out pattern to remember for any renderer-owned raw-template subdir.
- **Helm v4 enforces values.schema.json during `template`** — R18 and R19 both fire on a schema-violating chart. Integration test (`helm-values-mismatch` fixture) asserts this explicitly.
- **`make lint` baseline** — `internal/shenfull/*` + a handful of pre-existing untouched files are expected failures. Only new regressions in session-touched files are real.
- **Implementer reporting fidelity** — require literal tail output. Paraphrased success claims have burned this plan once (S1).
- **Implementer context overflow** — S5's opus run crashed with `Prompt is too long` after ~17 minutes. Mitigation: keep implementer prompts tight; orchestrator should be prepared to pick up uncommitted work (S5 recovered cleanly this way).

## Key file locations

- S1 verify report: `thoughts/shared/verify/s1-report.md`
- S2 verify report: `thoughts/shared/verify/s2-report.md`
- S3 verify report: `thoughts/shared/verify/s3-report.md` (tip `9ce3a9c`; verified MERGE READY on all 13 automated + 5 S3-specific checks)
- S4 verify report: `thoughts/shared/verify/s4-report.md` (tip `e3fe53b`; verified MERGE READY — 11/0/0)
- S4 implementer report: `thoughts/shared/verify/s4-implementer-report.md`
- S2 prep artifact: `thoughts/shared/prep/s2/selector-mappings.md` (consumed; 53 rows now live in `pkg/ir/selector_registry.go`)
- S3 prep artifacts: `thoughts/shared/prep/fixtures/s3/` (consumed — copies now live under `testdata/fixtures/resource-field-invalid/` and `testdata/fixtures/resource-field-valid-ok/`)
- S4 prep artifacts: `thoughts/shared/prep/fixtures/s4/` (consumed — copies now live under `testdata/fixtures/helm-render-ok/`, `helm-render-fail/`, `helm-values-mismatch/`)
- S5 prep artifacts: `thoughts/shared/prep/fixtures/s5/` — `appset-list/`, `appset-matrix/`, `appset-pullrequest/`, `kustomize-ok/`, `kustomize-render-fail/`. Inspect before briefing.
- S3 reusable surfaces (for S4+):
  - `pkg/schemas/index.go:27` — `BuildSchemaIndex`
  - `pkg/schemas/validate_manifest.go` — `ValidateManifest` (S4 reused it for values.schema.json — confirmed working)
  - `pkg/schemas/validate_manifest.go:168` — array-path walker (`case "array":`)
  - `pkg/types/types.go` — `ResourceFieldFact` / `ViolationKind` pattern for new field-level rules
- S4 reusable surfaces (for S5):
  - `pkg/renderer/renderer.go` — `Renderer` interface; Kustomize should implement the same shape
  - `pkg/renderer/cache.go` — two-tier SHA-256 cache; extend key inputs for Kustomize (overlay tree + kustomize version)
  - `pkg/renderer/helm.go` — reference implementation of absent-binary sentinel, timeout, stdout parse. Kustomize parallels all three.
  - `pkg/renderer/values_schema.go` — thin adapter over `ValidateManifest`; illustrates how to plug schemas into the render pipeline.
  - `pkg/types/types.go` — `ResourceInfo.Provenance`, `RenderResult`, `ValuesIssue`, `World.RenderResults` — reuse as-is or extend; don't duplicate.
  - Bridge section wiring: `pkg/checker/bridge.go` `render-results` section — add a sibling `determinism-results` section for R20 per base plan line 358.
- Makefile targets: `test`, `lint`, `build` (note: `make lint` has pre-existing failures in `internal/shenfull/*` generated code and a handful of pre-existing-unmodified files — treat those as baseline, fail the check only on NEW regressions)

## Gotchas captured so far

- **Worktree default base**: see memory file, pre-create worktrees manually with explicit base `claude/phase1-cleanup`.
- **Shen uppercase = variable (S3)**: `(UnknownField -> ...)` binds `UnknownField` as a variable and matches everything. Emit lowercase-dashed symbols from Go and match those in Shen.
- **Shen booleans are unreliable as fact discriminators (S4)**: `true`/`false` in Shen patterns did not match consistently in S4's `RenderResult`. The fix was to emit `render-ok` / `render-failed` symbols from Go. Use lowercase-dashed success/failure tags as a rule; avoid encoding booleans into bridge output.
- **`argoAppToObj` is pattern-matched in r6/r6c/r7**: S1 added a separate `argo-app-proj-links` section rather than modifying the tuple. Future rules touching App facts should follow the same pattern (new section, not field addition) to avoid breaking existing rules.
- **`resolvePatchTypes` semantics shift (S3)**: the refactor to use `BuildSchemaIndex` changed XRD version matching from "any referenceable version" to "explicit `CompositeTypeRef.Version`". R5 tests pass; the old behavior was probably a bug. Noted here in case a future session hits an unexpected "unknown" patch type fallback.
- **Loader `templates/` skip (S4)**: `pkg/loader/loader.go` now skips `templates/` directories adjacent to a `Chart.yaml` so raw Go-template YAML doesn't break the decoder. Pattern to remember for Kustomize-style renderer-owned subdirs.
- **Helm v4 enforces values.schema.json during `template` (S4)**: producing BOTH R18 render-failure and R19 schema-violation errors for a schema-violating chart. Integration test asserts this; trigger-for-flip is noted inline.
- **Manual fg-manifold replay**: target MRs may already be merged upstream, so "find the miss live" isn't always possible. Fixture-based validation is the primary signal; real-world replay is a crash/false-positive smoke test.
- **Implementer reporting**: S1 implementer v1 reported "`make test` passed" against a worktree that had no Makefile — it ran `go test` directly. Verify success-criteria commands were literally run, not paraphrased.
