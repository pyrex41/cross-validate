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
| 4 | S4 — XPC.H.helm-renders + values-well-typed | ⬜ next | — | Consumes `thoughts/shared/prep/fixtures/s4/`. Reuses S3's `ValidateManifest` for values.schema.json (base plan line 296). |
| 5 | S5 — Kustomize + AppSet + determinism | ⬜ | — | Consumes `thoughts/shared/prep/fixtures/s5/` |

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

## Next session — S4 (dispatch guide, tip-verified)

Wave 3 is merged at `11ecb9c`; the orchestrator commit that added this brief will be `HEAD` at dispatch time. Everything below is ready for an implementer/verifier pair to run without further discovery.

**Base plan section**: `/Users/reuben/.claude/plans/research-written-wiggly-nova.md` lines **257–338** (S4 spec: Helm rendering + values-well-typed). S5 follows at **342–402**.

**Host reality check (already done by previous orchestrator on 2026-04-20)**:
- `helm` IS on PATH at `/opt/homebrew/bin/helm`, v4.1.4. Tests must STILL guard with `t.Skip` per base plan line 328 — CI may not have helm — but on this dev box the `helm-bin` success-criteria commands will run.
- Current tip of `claude/phase1-cleanup`: `940a356` (the commit that landed this briefing) on top of `11ecb9c` (S3 verify report). Implementer must be dispatched against this tip.

### File anchors (grepped at `940a356`, keep in sync if you refresh)

| File | Lines | What's there | S4 action |
|---|---|---|---|
| `pkg/checker/bridge.go` | 1075 | `sortedSection` helper @ 317; section list `worldToShenObj` @ 378–403; last current entry `resource-field-facts` @ 400 | **Insert** `sortedSection("render-results", w.RenderResults, renderResultCmp, renderResultToObj)` after line 400. Add cmp + toObj in this file. |
| `pkg/types/types.go` | 769 | `ResourceInfo` @ 180–190; `ArgoSource` / `RendererHelm` @ 225–230; `ResourceFieldFact` / `ViolationKind` pattern @ 629–652; `World` struct @ 704–737 | **Add** `ResourceInfo.Provenance string` (default `"direct"`); **add** `ValuesIssue` + `RenderResult` structs (mirror `ResourceFieldFact` shape); **add** `World.RenderResults []RenderResult` with the same `json:"-"` tag as S3 used for `ResourceFieldFacts` (RenderResults are serialized into Shen, not public JSON). |
| `pkg/ir/builder.go` | 1025 | `addArgoApplication` @ 409–474; `parseArgoSource` @ 476+ handles helm config into `src.Helm` | **Insert** render-invocation hook AFTER the app is fully parsed (after line 472 `b.world.ArgoApps = append...`). Guard with `b.SkipRender` per base plan line 284. On render success, parse rendered YAML through `loader.LoadReader` and append to `b.world.Resources` with `Provenance = "rendered:helm:<app-name>"`. On render fail OR values-schema violations, append a `RenderResult` to `b.world.RenderResults`. |
| `cmd/xpc/main.go` | 783 | Raw arg-parsing (NOT cobra); `runCheck` flag-switch @ 107–124; help text @ 58–103; `runExplain` @ 520 | **Add** `--skip-render` (bool) and `--helm-bin=<path>` (string) flags matching the `case len(arg) > N && arg[:N] == "--flag=":` pattern. **Update** `printUsage`. **Update** `runExplain` to document `XPC.H.helm-renders` and `XPC.H.values-well-typed`. Thread flags into the `ir.Builder` via a new `SkipRender` / `HelmBin` field. |
| `kernel/check.shen` | 137 | `load` list @ 16–34 ends at `r17`; `extract-section` bindings @ 61–82 end at `ResourceFieldFacts` and `Trajectory`; `mark-rule` bindings @ 88–105 end at `R17`; append cascade @ 107–109 ends with `(append R16 R17)` followed by **19 `)`** | **Add** `(load "r18-helm-renders.shen")` and `(load "r19-values-well-typed.shen")`. **Add** `RenderResults (extract-section render-results Sections)` near the `ResourceFieldFacts` line. **Add** `R18 (mark-rule "XPC.H.helm-renders" (check-r18 RenderResults))` and `R19 (mark-rule "XPC.H.values-well-typed" (check-r19 RenderResults))`. **Change** tail from `(append R16 R17)` to `(append R16 (append R17 (append R18 R19)))` — that adds **exactly 2 `append` opens → 2 more `)` at line 109** (let-bindings themselves contribute zero extra closers; only `append` does). Final closer count: 19 → **21**. |
| `pkg/renderer/renderer.go` | new | — | Create per base plan §S4 item 1: `Renderer` interface, `ResolveChart` helper, chartPath resolution relative to the Application's file cwd. |
| `pkg/renderer/helm.go` | new | — | Create per §S4 item 2: `HelmRenderer{HelmBin string}.Render(...)`. First-use `helm version` probe; on absence return a sentinel that the caller turns into a `RenderResult{Success:false, Error:"helm absent"}` **with severity=warning in the judgment** (base plan line 273). 30s timeout with `context.WithTimeout`. |
| `pkg/renderer/cache.go` | new | — | Create per §S4 item 3: in-memory + `~/.cache/xpc/renders/` two-tier cache. Key = SHA-256(chart-dir-tree hash ‖ sorted values bytes ‖ helm version). 15-min TTL. |
| `pkg/renderer/values_schema.go` | new | — | Create per §S4 item 6: invoke `schemas.ValidateManifest(chartValuesSchemaJSON, mergedValues)`. **IMPORTANT**: `ValidateManifest` was designed for CRDs (top-level apiVersion/kind/metadata exempt under additionalProperties:false), but for a values.schema.json — which usually does NOT set root `additionalProperties:false` — that exemption block is never reached, so the existing walker is correctly reusable. Fixture `helm-values-mismatch` has no `additionalProperties:false` and only checks `replicas: integer`. If you must set `additionalProperties:false` at values root, know that top-level `apiVersion`/`kind`/`metadata`/`status` get silently skipped — a non-issue for charts but worth a code comment. Map each returned `ResourceFieldFact` → `ValuesIssue{Path, Message}`. |
| `kernel/r18-helm-renders.shen` | new | — | `check-r18 RenderResults` — one error judgment per `RenderResult` with `Success == false` AND a real error (non-absent-helm). Absent-helm → severity `warning` (base plan line 273). Use lowercase-dashed kind tags (Shen uppercase = variable — see gotchas). Model after `r17-resource-field-valid.shen`. |
| `kernel/r19-values-well-typed.shen` | new | — | `check-r19 RenderResults` — one error judgment per `ValuesIssue` inside each `RenderResult.ValuesIssues`. Model after r17. |
| `testdata/fixtures/helm-render-ok/` | new | — | Copy from `thoughts/shared/prep/fixtures/s4/helm-render-ok/` verbatim; add a Go test asserting one `Deployment` appears in `World.Resources` with `Provenance == "rendered:helm:helm-render-ok"`. |
| `testdata/fixtures/helm-render-fail/` | new | — | Copy from prep; test asserts one `RenderResult` with `Success == false` and a non-empty error string. |
| `testdata/fixtures/helm-values-mismatch/` | new | — | Copy from prep; test asserts one `RenderResult.ValuesIssues` entry with `Path == "replicas"` and `Violation == WrongType`. |
| `docs/obligations.md` | — | Category H rows for `helm-renders` and `values-well-typed` | Mark both implemented. |

### Reusable surfaces from S3 (do NOT reinvent)

- `schemas.BuildSchemaIndex(*types.World) map[SchemaKey]map[string]interface{}` @ `pkg/schemas/index.go:27` — S4 does NOT need to extend this (values.schema.json is per-chart, not per-GVK). But read it to understand the schema lookup pattern so R17 facts-on-rendered-resources keeps working after S4's builder change.
- `schemas.ValidateManifest(schema, raw)` @ `pkg/schemas/validate_manifest.go:25` — base plan line 296 says S4 reuses this for values.schema.json. Confirmed reusable (see the `pkg/renderer/values_schema.go` row above). Array walker @ line 168 is inline; don't extract.
- `ResourceFieldFact` / `ViolationKind` pattern @ `pkg/types/types.go:629–652` — template for `ValuesIssue` and `RenderResult`.
- **Audit proof version is `4`** (S3 bumped it). S4 does **NOT** bump again — the plan authorizes one bump per multi-rule-add and S3 spent it. Leave `pkg/audit/proof.go` alone.

### Dispatch mechanics

1. **Pre-create worktree with explicit base** — `Agent(isolation: "worktree")` defaults to `origin/HEAD` = `claude/build-xpc-type-checker-TfgsT` (wrong architecture; see `~/.claude/projects/-Users-reuben-projects-cross-validate/memory/feedback_agent_worktree_base.md`):
   ```bash
   git worktree add .claude/worktrees/s4-impl -b claude/xpc-s4-helm-renders claude/phase1-cleanup
   ```
2. **Dispatch Implementer-S4** — `general-purpose`, `model: opus`, `run_in_background: true`. Prompt MUST contain:
   - `cd /Users/reuben/projects/cross-validate/.claude/worktrees/s4-impl` as first action.
   - Sanity checks: `git log --oneline -1` tip must start with the orchestrator-commit SHA (fetch it from the shell before dispatching and embed it literally); `wc -l pkg/checker/bridge.go` must be ≥ 1075; `grep -c BuildSchemaIndex pkg/schemas/index.go` must be ≥ 1 (proves S3 merged).
   - The anchor table above, copied verbatim.
   - Commit discipline: one logical commit per file-group (renderer package first, then types, then builder, then bridge, then kernel files, then CLI, then fixtures+tests, then docs). Messages in imperative mood matching recent history (`git log --oneline -20` for style).
   - Test requirements: `make test` green (tests use `t.Skip` when `helm` not on PATH); `go test ./pkg/renderer/... -count=2` green; `go run ./cmd/xpc check --helm-bin=$(which helm) testdata/fixtures/helm-render-ok` exits 0 and `xpc dump-ir` shows the rendered Deployment; `go run ./cmd/xpc check --skip-render testdata/fixtures/helm-render-ok` exits 0 with one info diagnostic.
   - Literal-output mandate: paste the actual tail of `make test` / `make lint` / `go run ...` into a report at `thoughts/shared/verify/s4-implementer-report.md` — no paraphrasing (S1 burned us on this).
   - Shen gotchas: uppercase identifiers are variables (emit lowercase-dashed from Go); `string-contains?` arg order is `(Haystack Needle)`; string literals can't contain `\"`; after every kernel edit run `go run ./cmd/xpc check testdata/fixtures/basic` as a smoke test.
   - Paren budget: line 109 of `kernel/check.shen` ends in 19 `)` today; after S4 it should end in **21 `)`** (2 new `append` opens). If you see `Panic: &{22}` after a kernel edit, that's paren mis-count — bisect.
3. **Dispatch Verifier-S4** — `general-purpose`, `model: haiku`, read-only to source. Prompt instructs it to cd into the worktree, re-run all automated success-criteria commands fresh, write `thoughts/shared/verify/s4-report.md` with the same frontmatter shape as `thoughts/shared/verify/s3-report.md` (session: S4, rule: XPC.H.helm-renders + XPC.H.values-well-typed, branch: claude/xpc-s4-helm-renders, tip: <actual tip after implementer>, verifier: haiku, date: YYYY-MM-DD).
4. **Human gate** — user reviews report before merge.
5. **Merge sequence**:
   ```bash
   git checkout claude/phase1-cleanup
   git merge --ff-only claude/xpc-s4-helm-renders
   cp .claude/worktrees/s4-impl/thoughts/shared/verify/s4-report.md thoughts/shared/verify/
   git add thoughts/shared/verify/s4-report.md && git commit -m "docs: S4 verify report for XPC.H.helm-renders + values-well-typed"
   git worktree remove -f -f .claude/worktrees/s4-impl
   git branch -d claude/xpc-s4-helm-renders
   ```
6. **Update this handoff** — flip Wave 4 row to ✅ merged, record new tip, replace this "Next session — S4" block with "Next session — S5" (same shape; base plan lines **342–402**).

### Past-session gotchas (all still apply)

- **Shen uppercase identifiers are variables, not symbols** — S3 hit this. A pattern like `(UnknownField -> ...)` binds `UnknownField` as a variable and matches everything. Fix: emit lowercase-dashed symbols from Go (`unknown-field`, `wrong-type`) and match those in Shen. S4's `ValuesIssue` will have similar kind tags (`syntax-error`, `schema-mismatch`, etc.) — use lowercase-dashed.
- **Shen `check-world` paren discipline**: adding one new Section extract + one new Rule binding requires exactly one more `)` at the end of the big `let`. Off-by-one yields `Panic: &{22}` from `PrimSimpleError`. S2 wasted time on this; S3 avoided it by budgeting for it. S4 adds TWO sections + TWO rules → TWO extra `)`. Run `go run ./cmd/xpc check testdata/fixtures/basic` as a smoke test after every kernel edit.
- **Shen string literals DO NOT support `\"`**: keep quotes out of kernel-file strings; use `cn` concatenation with pre-built quote-free segments.
- **Prelude `string-contains?` arg order is `(Haystack Needle)`** — easy to invert.
- **S2's array-path TODO**: 18/53 selector registry rows still inert in `pkg/ir/trajectory_extract.go:extractSelectorUsages` (line 105). S3's walker does NOT close this — different traversal problem. Not blocking S4, but track as future cleanup.
- **Implementer reporting fidelity**: past implementers have paraphrased "make test passed" when they actually ran `go test` directly. Require literal tail output in the report. S1 wave 1 hit this; S2/S3 did not.
- **`make lint` baseline**: pre-existing failures in `internal/shenfull/*` and a few pre-existing-unmodified files are expected. Fail the check only on NEW regressions on files the session touched. (S3 produced a NET improvement by gofmt'ing `pkg/types/types.go` — S4 should avoid regressing this.)

## Key file locations

- S1 verify report: `thoughts/shared/verify/s1-report.md`
- S2 verify report: `thoughts/shared/verify/s2-report.md`
- S3 verify report: `thoughts/shared/verify/s3-report.md` (tip `9ce3a9c`; verified MERGE READY on all 13 automated + 5 S3-specific checks)
- S2 prep artifact: `thoughts/shared/prep/s2/selector-mappings.md` (consumed; 53 rows now live in `pkg/ir/selector_registry.go`)
- S3 prep artifacts: `thoughts/shared/prep/fixtures/s3/` (consumed — copies now live under `testdata/fixtures/resource-field-invalid/` and `testdata/fixtures/resource-field-valid-ok/`)
- S4 prep artifacts: `thoughts/shared/prep/fixtures/s4/` — `helm-render-ok/`, `helm-render-fail/`, `helm-values-mismatch/`. Inspect before briefing.
- S5 prep artifacts: `thoughts/shared/prep/fixtures/s5/` — `appset-list/`, `appset-matrix/`, `appset-pullrequest/`, `kustomize-ok/`, `kustomize-render-fail/`.
- S3 reusable surfaces (for S4+):
  - `pkg/schemas/index.go:27` — `BuildSchemaIndex`
  - `pkg/schemas/validate_manifest.go` — `ValidateManifest` (the S4 values.schema.json path per base plan line 296)
  - `pkg/schemas/validate_manifest.go:168` — array-path walker (`case "array":`)
  - `pkg/types/types.go` — `ResourceFieldFact` / `ViolationKind` pattern for new field-level rules
- Makefile targets: `test`, `lint`, `build` (note: `make lint` has pre-existing failures in `internal/shenfull/*` generated code and a handful of pre-existing-unmodified files — treat those as baseline, fail the check only on NEW regressions)

## Gotchas captured so far

- **Worktree default base**: see memory file, pre-create worktrees manually with explicit base `claude/phase1-cleanup`.
- **Shen uppercase = variable (S3)**: `(UnknownField -> ...)` binds `UnknownField` as a variable and matches everything. Emit lowercase-dashed symbols from Go and match those in Shen.
- **`argoAppToObj` is pattern-matched in r6/r6c/r7**: S1 added a separate `argo-app-proj-links` section rather than modifying the tuple. Future rules touching App facts should follow the same pattern (new section, not field addition) to avoid breaking existing rules.
- **`resolvePatchTypes` semantics shift (S3)**: the refactor to use `BuildSchemaIndex` changed XRD version matching from "any referenceable version" to "explicit `CompositeTypeRef.Version`". R5 tests pass; the old behavior was probably a bug. Noted here in case a future session hits an unexpected "unknown" patch type fallback.
- **Manual fg-manifold replay**: target MRs may already be merged upstream, so "find the miss live" isn't always possible. Fixture-based validation is the primary signal; real-world replay is a crash/false-positive smoke test.
- **Implementer reporting**: S1 implementer v1 reported "`make test` passed" against a worktree that had no Makefile — it ran `go test` directly. Verify success-criteria commands were literally run, not paraphrased.
