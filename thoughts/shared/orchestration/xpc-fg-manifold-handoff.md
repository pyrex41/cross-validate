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

## Next session — S4 (briefing guide for the incoming orchestrator)

You are the orchestrator now. Wave 3 is merged; Wave 4 is your job. Follow the same shape the previous orchestrator used: verify anchors at the current tip, inspect S4 prep, author a fully-briefed S4 dispatch section (replacing THIS section), commit the brief, then fire implementer + verifier.

**Base plan section**: `/Users/reuben/.claude/plans/research-written-wiggly-nova.md` lines **257–338** (S4 spec: Helm rendering + values-well-typed). S5 follows at **342–402**.

**Current HEAD**: `claude/phase1-cleanup` @ `11ecb9c` (S3 verify report). Verify before dispatching:

```bash
cd /Users/reuben/projects/cross-validate
git log --oneline -5     # tip should be 11ecb9c
```

### What S3 shipped that S4 should reuse

- **`schemas.BuildSchemaIndex(*types.World) map[SchemaKey]map[string]interface{}`** at `pkg/schemas/index.go` — single-source schema map. S4's `values.schema.json` validation should go through this OR extend it rather than re-reading CRDs.
- **`schemas.ValidateManifest(schema, raw) []ResourceFieldFact`** at `pkg/schemas/validate_manifest.go` — **the base plan at line 296 explicitly says S4's values.schema.json validation reuses this** (that's why A1 came first). The function handles `type`, `enum`, `required`, `additionalProperties:false`, and arrays of any element type (including nested objects).
- **Array-path walker** is inline in `validate_manifest.go` around line 168 (the `case "array":` branch). If S4's `values.schema.json` paths need array indexing (`replicas[0]`, etc.), this already works — don't rewrite it.
- **`ResourceFieldFact` / `ViolationKind` pattern** in `pkg/types/types.go` — the template for S4's `ValuesIssue` type (see base plan line 291).
- **Audit proof is v4** — S4 should NOT bump it again. The plan authorizes exactly one bump per multi-rule-add, and S3 spent it.

### S4 prep artifacts (already on disk)

`thoughts/shared/prep/fixtures/s4/` — **inspect before you brief**:
- `helm-render-ok/` — minimal chart that renders cleanly to one Deployment.
- `helm-render-fail/` — chart with a template syntax error.
- `helm-values-mismatch/` — chart with `values.schema.json` requiring `replicas: integer`, values file passing `replicas: "three"`.

Implementer copies these into `testdata/fixtures/` at dispatch time (same pattern S3 used).

### S4-specific briefing TODOs (do these before you write the dispatch section)

1. **Verify the 10+ file anchors** at tip `11ecb9c` — file paths, line numbers, insertion points. Use the base plan §S4 as the spec but fresh-grep line numbers because bridge.go / check.shen have grown since S3.
2. **Check helm availability strategy**: base plan line 273 says "if helm absent, emit `XPC.H.helm-renders` severity=warning" — confirm the test suite uses `t.Skip` when helm is not on PATH (line 328). The verifier's automated phase needs to know whether `which helm` is expected to succeed or not.
3. **Decide the `--skip-render` default**: base plan lines 313–314 add this flag. Default behavior when `helm` is absent on the test host matters for CI.
4. **`kernel/check.shen` paren-count bump**: S4 adds TWO new sections (`render-results`) and TWO new rules (R18, R19) — the `let` binding at lines 66–76 + mark-rule block at 89–102 will need one additional `)` per new let-binding + per new mark-rule. Budget time for paren bisection (see gotchas).
5. **S2 follow-up consideration**: S3's array walker lives in `validate_manifest.go` but R16's selector-path TODO in `trajectory_extract.go` is a separate traversal (expanding `[]` placeholders vs. walking a concrete array). Out of scope for S4, but if S4 naturally refactors to a shared path-walker helper, note it and flag the S2 follow-up.

### Dispatch pattern (same as S1/S2/S3)

1. **Pre-create worktree** (do NOT use `Agent(isolation: "worktree")` — wrong base branch; see memory):
   ```bash
   git worktree add .claude/worktrees/s4-impl -b claude/xpc-s4-helm-renders claude/phase1-cleanup
   ```
2. **Dispatch Implementer-S4** — `general-purpose`, `model: opus`, run in background. Self-contained prompt including: sanity checks (tip = `11ecb9c`, line counts, grep for `BuildSchemaIndex` to prove S3 merged), file-by-file anchor table, gating rules, test requirements, commit discipline, literal-output report mandate.
3. **Dispatch Verifier-S4** — `general-purpose`, `model: haiku`, read-only to source, writes `thoughts/shared/verify/s4-report.md`. Match S3 report frontmatter shape.
4. **Human gate** — user reviews report, then merge / iterate / abort.
5. **Merge sequence** (same as S3):
   ```bash
   git checkout claude/phase1-cleanup
   git merge --ff-only claude/xpc-s4-helm-renders
   cp .claude/worktrees/s4-impl/thoughts/shared/verify/s4-report.md thoughts/shared/verify/
   git add thoughts/shared/verify/s4-report.md && git commit -m "docs: S4 verify report for XPC.H.helm-renders + values-well-typed"
   git worktree remove -f -f .claude/worktrees/s4-impl
   git branch -d claude/xpc-s4-helm-renders
   ```
6. **Update this handoff** — flip the Wave 4 row to ✅ merged, record new tip, note anything worth carrying forward, replace this "Next session — S4" section with "Next session — S5" (same shape).

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
