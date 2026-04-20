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
| 3 | S3 — XPC.A.resource-field-valid | ⬜ next | — | Bumps audit proof to v4. Consumes `thoughts/shared/prep/fixtures/s3/` |
| 4 | S4 — XPC.H.helm-renders + values-well-typed | ⬜ | — | Consumes `thoughts/shared/prep/fixtures/s4/` |
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

## S3 dispatch — fully briefed (ready to fire)

**Base plan section**: `/Users/reuben/.claude/plans/research-written-wiggly-nova.md` lines **185–253** (S3 spec), plus line **430** (proof-version bump authorization) and line **423** (schema index must be built once per World, not per rule).

**HEAD at dispatch time**: `claude/phase1-cleanup` @ `e180e0e` (post-S2 merge + handoff update).

### Step 1 — Pre-create worktree

```bash
git worktree add .claude/worktrees/s3-impl -b claude/xpc-s3-resource-field-valid claude/phase1-cleanup
```

### Step 2 — Anchors verified at `e180e0e` (paste into agent prompt)

| File | Lines | Key anchors |
|---|---|---|
| `pkg/schemas/fetcher.go` | 158 | `FieldType` consts `87–98`; `ResolveFieldType` `100–131`; `TypeAssignable` `133–146`; `getNestedMap` `148–158`. **Keep these untouched** (R5 depends on them). |
| `pkg/schemas/index.go` | — | **New file**. Exports `SchemaKey{APIVersion, Kind string}`, `BuildSchemaIndex(*types.World) map[SchemaKey]map[string]interface{}`. Consolidates the xrd/crd schema-map construction done ad-hoc in `bridge.go:243–262`. |
| `pkg/audit/proof.go` | 518 | **`Version: 3` literal at line 132** — bump to `4`. No replay code branches on Version (grep confirmed); the bump is pure metadata. `Version` field declaration at line 22 stays `int`. `KernelVersion = "0.1.0"` at 122, `RulesetVersion = "2026.04"` at 125 — unchanged. |
| `pkg/audit/proof_test.go` | — | Line 32 asserts `expected version 3` — update to 4. This is the ONLY call site that hard-codes 3. |
| `pkg/checker/bridge.go` | 1041 | `resolvePatchTypes` call at `155`, def at `240–298`. S3 should **refactor this to use `BuildSchemaIndex`** (plan §2, "pure cleanup, no behavior change" — existing R5 tests must still pass). `sortedSection` helper at line `331`, `worldToShenObj` at `341`. Sections block ends at `412` — insert `sortedSection("resource-field-facts", …)` before `trajectoryToObj`. `obligationRefForCode` table starts at ~`811`; **do NOT add an entry for `XPC.A.*`** (same gating as R15/R16). |
| `pkg/types/types.go` | 738 | `World` struct at `630–648`. Add `ResourceFieldFacts []ResourceFieldFact` (with `json:"-"` to mirror `ImmutableFields`). Add `ResourceFieldFact` struct + `ViolationKind` string const group (values: `UnknownField`, `WrongType`, `MissingRequired`, `InvalidEnum`) alongside `ImmutableField` at `622`. |
| `pkg/ir/builder.go` | 1024 | `Build` at `30–63`. Current tail: `EnrichTrajectoryData(b.world); return b.world, nil`. Add a call to the new field-validation enrichment AFTER `EnrichTrajectoryData` so schemas are already loaded. |
| `pkg/ir/field_validation.go` | — | **New file**. Exports `EnrichFieldValidation(*types.World)`. Uses `schemas.BuildSchemaIndex(w)` and `schemas.ValidateManifest(schema, raw)` to populate `w.ResourceFieldFacts`. |
| `kernel/check.shen` | 134 | Loads end at line `33` (r16). Add `(load "r17-resource-field-valid.shen")` at `34`. `extract-section` bindings in `check-world` at `66–76` — add `ResourceFieldFacts (extract-section resource-field-facts Sections)`. Rule bindings at `89–102` — add `R17 (mark-rule "XPC.A.resource-field-valid" (check-r17 ResourceFieldFacts))`. Extend the final `append` chain accordingly. **Paren count warning — see gotchas below.** |
| `kernel/r17-resource-field-valid.shen` | — | **New file**. Template: copy `kernel/r15-appproject-whitelist.shen` shape. `check-r17 Facts` — one judgment per fact. Per-kind message template driven by the ViolationKind symbol in the fact tuple. |

### Step 3 — Fixtures already prepped (inspect first)

`thoughts/shared/prep/fixtures/s3/` contents verified:

- `crd.yaml` — one Widget CRD: `group=example.com`, `spec.properties.{name:string(required), size:integer, color:string enum=[red,green,blue], tags:array<string>}`, `additionalProperties:false` on `spec`.
- `invalid-enum/widget.yaml` — `spec: {name: gizmo, color: purple}` → expects `InvalidEnum` at `spec.color`
- `missing-required/widget.yaml` — `spec: {size: 3}` → expects `MissingRequired` for `spec.name`
- `unknown-field/widget.yaml` — `spec: {name: gizmo, nonsense: 42}` → expects `UnknownField` at `spec.nonsense`
- `wrong-type/widget.yaml` — `spec: {name: 7}` → expects `WrongType` at `spec.name` (got int, want string)

Implementer should **copy these under `testdata/fixtures/resource-field-invalid/`** (subdirs preserved) — prep dir stays as source-of-truth archive. Also add one positive-control `testdata/fixtures/resource-field-valid-ok/` with a well-formed Widget → expects zero R17 diagnostics.

### Step 4 — Sanity-check commands (first agent action)

```bash
cd /Users/reuben/projects/cross-validate/.claude/worktrees/s3-impl
git log --oneline -3     # expect tip = e180e0e
wc -l pkg/checker/bridge.go pkg/ir/selector_registry.go pkg/audit/proof.go kernel/check.shen
# expect: 1041  465  518  134
grep -n "Version: 3" pkg/audit/proof.go     # expect exactly one hit at line 132
```

If any mismatch: STOP, report back — not on the right tree.

### Step 5 — Gating rules for the prompt

- ✅ **Proof version bump IS authorized** — bump `pkg/audit/proof.go:132` from 3→4 and `proof_test.go:32` from 3→4. The plan (line 430) explicitly earmarks this bump for "once, at the start of S3."
- ❌ No `t.Parallel()` in any new test.
- ❌ No obligation-framework wiring — R17 is a direct Shen rule like R15/R16 (do NOT add `XPC.A.*` to `obligationRefForCode`).
- ⚖️ **Mixed Go/Shen split is expected this time**: unlike R15/R16 (Shen-heavy), R17's smarts live in Go `ValidateManifest` and the Shen rule is a trivial fact→judgment mapper. This mirrors R5's pattern — cite `kernel/r5-patch-typecheck.shen` for shape.
- 🔒 `resolvePatchTypes` refactor to use `BuildSchemaIndex` is **pure cleanup** — all existing R5 tests must still pass, no behavior change.

### Step 6 — Success criteria the verifier will check (base plan 243–251)

**Automated**:
- `make test` passes, including new `TestR17_FieldValidation` covering all four subfixtures + the positive-control
- New unit tests in `pkg/schemas/` for `ResolveFieldFacts` + `ValidateManifest` — ≥8 cases (happy, unknown at depth 1/2/3, enum match/miss, required present/absent, additionalProperties allowed/forbidden)
- R5 tests still pass after `resolvePatchTypes` refactor
- `xpc check testdata/fixtures/resource-field-invalid/unknown-field` exits non-zero with exactly one `XPC.A.resource-field-valid`
- Each of the four subfixtures triggers exactly one R17 diagnostic with the expected ViolationKind
- `resource-field-valid-ok/` produces zero R17 diagnostics
- Proof emitted by `xpc check --audit=<path>` has `"version": 4`

**Manual (human gate)**:
- Run against `~/fg/fg-manifold` — top 20 findings reviewed; false-positive rate <5% (plan 249). If above threshold, revisit walker strictness before merge.
- Historical MR `!1186` (LaunchTemplate.privateIpAddresses) replay: checkout pre-fix tree, run xpc, confirm R17 fires with the right path (plan 251).

### Step 7 — Verifier dispatch (Haiku)

Same shape as S1/S2 verifier. Report path: `.claude/worktrees/s3-impl/thoughts/shared/verify/s3-report.md`. Additional checks beyond the boilerplate:

- `grep -n 'Version: 4' pkg/audit/proof.go` shows exactly one hit (the bump landed).
- `grep -n 'Version: 3' pkg/audit/proof.go` shows zero hits (the old literal is gone).
- `pkg/audit/proof_test.go` expected-version assertion is 4.
- `BuildSchemaIndex` is used from both `field_validation.go` AND the refactored `resolvePatchTypes` (grep confirms single-source).
- `obligationRefForCode` table in `bridge.go` has no `XPC.A.*` entry.
- Paren discipline: `go build ./...` AND a `xpc check` run against any existing fixture (e.g. `appproject-whitelist-miss`) succeed — catches silent Shen-paren breakage that wouldn't show in `go test` alone.

### Past-session gotchas (keep for S3 to read)

- **Shen `check-world` paren discipline**: adding one new Section extract + one new Rule binding requires exactly one more `)` at the end of the big `let`. Off-by-one yields `Panic: &{22}` from `PrimSimpleError`. S2 wasted time on this — when it fires, bisect paragraph-by-paragraph. Consider running `go run ./cmd/xpc check testdata/fixtures/basic` as a smoke test after every kernel edit.
- **Shen string literals DO NOT support `\"`**: keep all quotes out of kernel-file strings. Use `cn` concatenation with pre-built quote-free segments.
- **Prelude `string-contains?` arg order is `(Haystack Needle)`** — easy to get backwards.
- **Array-indexed paths in S2 trajectory_extract were punted** (18/53 rows inert). For S3's field walker, arrays MUST work — the CRD schema's `tags: array<string>` case is real and the `spec.tags[0] = 7` case should fire `WrongType`. Don't reuse S2's punt strategy; S3 needs full array walking in `ValidateManifest`. If S3 builds a shared array-path walker, note it in the handoff so S2's TODO can be closed in follow-up.
- **Implementer reporting fidelity**: past implementers have paraphrased "make test passed" when they actually ran `go test` directly. Require literal tail output in the report. S1 wave 1 hit this, S2 did not.

## Key file locations

- S1 verify report: `thoughts/shared/verify/s1-report.md`
- S2 verify report: `thoughts/shared/verify/s2-report.md`
- S2 prep artifact: `thoughts/shared/prep/s2/selector-mappings.md` (consumed; 53 rows now live in `pkg/ir/selector_registry.go`)
- S3 prep artifacts: `thoughts/shared/prep/fixtures/s3/` — inspected 2026-04-20. One Widget CRD + 4 parallel violation manifests (invalid-enum, missing-required, unknown-field, wrong-type). Implementer copies these into `testdata/fixtures/resource-field-invalid/` at dispatch time.
- Makefile targets: `test`, `lint`, `build` (note: `make lint` has pre-existing failures in `internal/shenfull/*` generated code and a handful of pre-existing-unmodified files — treat those as baseline, fail the check only on NEW regressions)

## Gotchas captured so far

- **Worktree default base**: see memory file, pre-create worktrees manually.
- **`argoAppToObj` is pattern-matched in r6/r6c/r7**: S1 added a separate `argo-app-proj-links` section rather than modifying the tuple. Future rules touching App facts should follow the same pattern (new section, not field addition) to avoid breaking existing rules.
- **Manual fg-manifold replay**: target MRs may already be merged upstream, so "find the miss live" isn't always possible. Fixture-based validation is the primary signal; real-world replay is a crash/false-positive smoke test.
- **Implementer reporting**: S1 implementer v1 reported "`make test` passed" against a worktree that had no Makefile — it ran `go test` directly. Verify success-criteria commands were literally run, not paraphrased.
