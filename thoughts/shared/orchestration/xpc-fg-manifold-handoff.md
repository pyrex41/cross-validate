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
| 5 | S5 — Kustomize + AppSet + determinism | ⬜ next | — | Consumes `thoughts/shared/prep/fixtures/s5/`. Reuses S4's renderer/cache shape for Kustomize. |

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

## Next session — S5 (briefing guide for the incoming orchestrator)

You are the orchestrator now. Wave 4 is merged; Wave 5 is your job and closes out the plan. Follow the same shape the previous orchestrator used: verify anchors at the current tip, inspect S5 prep, author a fully-briefed S5 dispatch section (replacing THIS section), commit the brief, then fire implementer + verifier.

**Base plan section**: `/Users/reuben/.claude/plans/research-written-wiggly-nova.md` lines **342–402** (S5 spec: Kustomize + ApplicationSet expansion + render-determinism). This is the last session.

**Current HEAD**: `claude/phase1-cleanup` @ `a8acd8c` (S4 verify report) on top of `e3fe53b` (S4 implementer tip). Verify before dispatching:

```bash
cd /Users/reuben/projects/cross-validate
git log --oneline -5     # tip should be a8acd8c
```

### What S4 shipped that S5 should reuse

- **Renderer package shape** (`pkg/renderer/{renderer,helm,cache,values_schema,file_util}.go`) — S5's Kustomize renderer should implement the same `Renderer` interface and reuse the same two-tier cache. Key additions only: overlay tree hash + kustomize version in the cache key.
- **`ResourceInfo.Provenance` field** (`pkg/types/types.go`) — Kustomize-rendered resources should tag as `"rendered:kustomize:<app-name>"`.
- **`RenderResult` type** — extend (don't duplicate) if Kustomize needs new issue fields, OR add `KustomizeResult` mirroring it. Prefer the former; the Shen `render-results` section already exists.
- **Loader skip-`templates/`** (`pkg/loader/loader.go`) — S4 added a special-case to skip `templates/` directories adjacent to a `Chart.yaml`. Kustomize doesn't have an analogous issue (kustomize overlays don't have unrenderable raw files), but be aware of the pattern if you need a similar carve-out.
- **Bridge render-results section** — S5 may extend to carry determinism facts OR add a separate `determinism-results` section (per base plan line 358: "facts surface as `DeterminismResults` on World"). A separate section is cleaner and matches the existing one-section-per-fact-type pattern.
- **Symbol-vs-boolean lesson (S4)**: the bridge emits lowercase-dashed symbol discriminators (`render-ok`, `render-failed`) instead of Shen booleans (`true`/`false`), because the Shen pattern-matcher treats the latter inconsistently across contexts. Use the same approach for any new success/failure flags in S5 facts.
- **Audit proof version is `4`** — S5 does NOT bump it again unless absolutely necessary. The plan authorized exactly one bump per multi-rule-add (S3 spent it).

### S5 prep artifacts (already on disk)

`thoughts/shared/prep/fixtures/s5/` — **inspect before you brief**:
- `kustomize-ok/` — minimal kustomization that renders cleanly.
- `kustomize-render-fail/` — kustomization with a resource/patch that fails `kustomize build`.
- `appset-list/` — ApplicationSet with a `list` generator.
- `appset-matrix/` — ApplicationSet with a `matrix` (list × list or list × git) generator.
- `appset-pullrequest/` — ApplicationSet with a `pullRequest` generator + a sample PR-stub fixture file for the `--appset-fixture=` flag.

Implementer copies these into `testdata/fixtures/` at dispatch time (same pattern S3/S4 used).

### S5-specific briefing TODOs (do these before you write the dispatch section)

1. **Verify the 10+ file anchors** at tip `a8acd8c` — file paths, line counts, insertion points. `pkg/checker/bridge.go`, `pkg/types/types.go`, `pkg/ir/builder.go`, `cmd/xpc/main.go`, `kernel/check.shen` have all grown since S4. Fresh-grep line numbers.
2. **Check kustomize availability strategy**: base plan (implicit) parallels Helm — if `kustomize` absent, emit warning-severity diagnostic. Confirm tests use `t.Skip` when `which kustomize` fails. Probe the dev host before dispatching (`which kustomize && kustomize version`).
3. **`--skip-appset-expand` flag default**: base plan line 378. Decide: default to expand (slower, richer) or skip (faster CI). S4 defaulted render to ON; consider parallel default here.
4. **`kernel/check.shen` paren-count bump**: S5 likely adds ONE new rule (`r20-render-deterministic`) plus ONE new section (`determinism-results`). Expected tail change: `(append R17 (append R18 R19))` + 21 closers → `(append R17 (append R18 (append R19 R20)))` + 22 closers (+1 append = +1 `)`). Confirm by counting actual current closers before dispatching. If S5 naturally adds a second rule (e.g., a separate `XPC.H.kustomize-renders` that wasn't obvious from the plan), budget +2 instead.
5. **ApplicationSet expansion is subtle** — base plan lines 360–374. Expanded Applications feed back into the normal pipeline, so S1's R15 (kind-whitelisted) and S2's R16 (selector-needs-ignore-diff) gain coverage automatically. Implementer should write a test that proves this (e.g., appset-matrix produces a whitelist violation on one of its expanded Applications). That's the integration-point proof for the whole 5-session plan.
6. **S2's array-path TODO is still open** — 18/53 selector-registry rows inert. S5 is NOT responsible for closing it, but if AppSet expansion produces selectors-in-arrays, the resulting false-negative may become more visible. Note in the S5 brief.
7. **Total-coverage assessment** (base plan line 401): S5's manual success criterion asks for a recomputed MR-bucket hit-rate table. Don't let the implementer skip this — it's the final scoreboard for the 5-session investment.

### Dispatch pattern (same as S1–S4)

1. **Pre-create worktree** (do NOT use `Agent(isolation: "worktree")` — wrong base branch; see memory):
   ```bash
   git worktree add .claude/worktrees/s5-impl -b claude/xpc-s5-kustomize-appset claude/phase1-cleanup
   ```
2. **Dispatch Implementer-S5** — `general-purpose`, `model: opus`, `run_in_background: true`. Self-contained prompt including: sanity checks (tip = `a8acd8c` or your new brief-commit SHA, line counts, grep for `pkg/renderer/helm.go` to prove S4 merged), file-by-file anchor table, the S4 symbol-vs-boolean lesson, gating rules, test requirements, commit discipline, literal-output report mandate, the ApplicationSet integration-point test requirement.
3. **Dispatch Verifier-S5** — `general-purpose`, `model: haiku`, read-only to source, writes `thoughts/shared/verify/s5-report.md`. Match S4 report frontmatter shape.
4. **Human gate** — user reviews report, then merge / iterate / abort.
5. **Merge sequence**:
   ```bash
   git checkout claude/phase1-cleanup
   git merge --ff-only claude/xpc-s5-kustomize-appset
   cp .claude/worktrees/s5-impl/thoughts/shared/verify/s5-report.md thoughts/shared/verify/
   git add thoughts/shared/verify/s5-report.md && git commit -m "docs: S5 verify report for Kustomize + AppSet + determinism"
   git worktree remove -f -f .claude/worktrees/s5-impl
   git branch -d claude/xpc-s5-kustomize-appset
   ```
6. **Final handoff update** — flip Wave 5 to ✅ merged. This is the last wave; append a brief "Plan complete" summary with total-coverage numbers from the manual success criterion, then retire the "Next session" block.

### Past-session gotchas (all still apply)

- **Shen uppercase identifiers are variables, not symbols** — S3 hit this; S4 hit it again with its success flag and had to switch to `render-ok`/`render-failed` symbols. Emit lowercase-dashed symbols from Go and match those in Shen. Do NOT use `true`/`false` as discriminators in facts.
- **Shen `check-world` paren discipline**: each new `append` adds exactly one `)` to line 109. Let-bindings add ZERO closers. S4 went 19 → 21; S5 likely goes 21 → 22 (one new rule). Off-by-one yields `Panic: &{22}` from `PrimSimpleError`. Run `go run ./cmd/xpc check testdata/fixtures/basic` as smoke test after every kernel edit.
- **Shen string literals DO NOT support `\"`**: keep quotes out of kernel-file strings; use `cn` concatenation with pre-built quote-free segments.
- **Prelude `string-contains?` arg order is `(Haystack Needle)`** — easy to invert.
- **S2's array-path TODO**: 18/53 selector registry rows still inert in `pkg/ir/trajectory_extract.go:extractSelectorUsages` (line 105). Not blocking S5, but the AppSet-expansion integration test may surface a false-negative that points here.
- **Implementer reporting fidelity**: require literal tail output in the implementer report. S1 burned us with paraphrased "make test passed" when the worktree had no Makefile.
- **`make lint` baseline**: pre-existing failures in `internal/shenfull/*` and handful of unmodified files are expected. Fail only on NEW regressions in files the session touched.
- **Loader skip-`templates/` (S4)**: `pkg/loader/loader.go` now skips `templates/` directories adjacent to a `Chart.yaml` so raw Go-template YAML doesn't break the decoder. Any S5 fixture with a similar pattern (raw-template files in a renderer-owned subdir) needs a similar carve-out or the pipeline will fail before the renderer runs.
- **Helm v4 enforces values.schema.json during `template`** — `helm-values-mismatch` produces BOTH R18 and R19 errors. The R18/R19 test documents this. If an older helm ever relaxes enforcement, the test will fail loudly — the comment there flags the trigger for flipping the assertion.

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
