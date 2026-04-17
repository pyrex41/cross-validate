---
date: 2026-04-16T16:48:06-05:00
researcher: Reuben Brooks
git_commit: b495e744e02884b87bb9cc8d11c0958524c82aee
branch: claude/build-xpc-type-checker-TfgsT
repository: pyrex41/cross-validate
topic: "Project review + PR #3 review"
tags: [research, codebase, xpc, obligation-framework, argo-cd, shen, review, pr]
status: complete
last_updated: 2026-04-16
last_updated_by: Reuben Brooks
---

# Research: Project review + PR #3 review

**Date**: 2026-04-16 16:48 -05:00
**Researcher**: Reuben Brooks
**Base commit**: `b495e74` on `claude/build-xpc-type-checker-TfgsT`
**PR head**: `91becc0` on `origin/claude/argo-cd-type-model-vCseW` (PR #3)
**Repository**: pyrex41/cross-validate

## Research Question
Document the `cross-validate` project as it exists today on the current branch, and review open PR #3 (`Implement obligation framework and extend Argo CD support`).

## Summary

`cross-validate` (binary `xpc`) is a Go CLI that type-checks Crossplane + Argo CD YAML before it reaches a cluster. On the current base branch, checks are expressed as 11 rules (R1–R11) implemented **twice**: once in the Shen kernel under `kernel/*.shen` (evaluated by an in-process Go Shen runtime in `pkg/shen`), and once as pure-Go functions in `pkg/checker/rules.go`. `Check()` in `pkg/checker/bridge.go` builds the IR, serializes the `World` to Shen cons-list values, evaluates `(check-world ...)`, and converts judgments back to `types.Diagnostic`. Supporting infrastructure includes a content-addressed schema cache, a cluster-state snapshot, and a Merkle-tree audit artifact (`pkg/proof`).

PR #3 introduces a bounded obligation taxonomy (ADR-001, 12 categories A–L) and migrates the execution path. `pkg/obligation/` defines a `Generator` / `Registry` / `Run` framework; 13 generators across 7 sub-packages absorb all of R1–R11. The Shen kernel dispatch is removed from `Check()`; `Check()` now routes exclusively through `obligation.Run`. `pkg/proof` is renamed to `pkg/audit`. The Argo IR is substantially extended: `ArgoApplication` gains `Sources` / `Destination` / `SyncPolicy` / `IgnoreDifferences` / `Hooks`; new top-level `ArgoProjects` and `ArgoAppSets` slices appear on `World`. A pilot test exercises the `comp-xrd-ref` generator; the rest of the generators rely on the existing `pkg/checker/rules_test.go` integration fixtures for coverage — but see §"PR Review / Things To Verify" below for a correction to that claim.

---

## Part 1 — Project As-Is (base branch `b495e74`)

### CLI Entrypoint — `cmd/xpc/main.go`

`main()` at [cmd/xpc/main.go:25](cmd/xpc/main.go) dispatches on `os.Args[1]` with a bare switch:

| Subcommand | Handler | Line |
|---|---|---|
| `check`    | `runCheck`        | 109 |
| `dump-ir`  | `runDumpIR`       | 253 |
| `snapshot` | `runSnapshot`     | 288 |
| `verify`   | `runVerify`       | 380 |
| `proof`    | `runProof` → `runProofShow` / `runProofDiff` | 407 |
| `bisect`   | `runBisect`       | 493 |
| `explain`  | `runExplain`      | 537 |
| `version`  | inline            | 47  |

Flags are parsed by hand (no flag library). `runCheck` pipeline: loader → `ir.NewBuilder().Build` → optional `snapshot.Load` + `mergeSnapshotIntoWorld` → kernel-path auto-detection → `checker.Check` → `report.ReportStdout` → optional `proof.Generate` to `check.xpcproof`. Exit code 1 if any `SeverityError` diagnostic is emitted.

`runExplain` uses a `map[string]string` inline in `main.go` ([lines 613–753](cmd/xpc/main.go)) mapping XPC001–XPC011 to long-form explanations.

### Loader — `pkg/loader/loader.go`

Public API: `LoadDirectory`, `LoadFile`, `LoadReader`, `LoadStdin`, `ClassifyDocument`. `LoadReader` streams YAML through `yaml.NewDecoder` twice per document (once into `*yaml.Node` to capture line/column, once into a `map[string]interface{}`). `LoadedDocument` ([loader.go:18](pkg/loader/loader.go)) holds `Source`, `APIVersion`, `Kind`, `Raw`, `RawNode`.

`ClassifyDocument` returns one of `crd|xrd|composition|function|provider|configuration|argo-application|resource`. The switch is at [lines 120–138](pkg/loader/loader.go).

### IR types — `pkg/types/types.go`

`World` ([types.go:188](pkg/types/types.go)):
```go
type World struct {
    CRDs           []CRDInfo
    XRDs           []CRDInfo
    Compositions   []CompositionInfo
    Functions      []FunctionInfo
    Providers      []ProviderInfo
    Configurations []ConfigurationInfo
    Resources      []ResourceInfo
    ArgoApps       []ArgoApplication
    Schemas        map[string]SchemaInfo  // content-addressed
}
```

Key nested types: `CRDInfo`, `CRDVersion`, `ConversionInfo` (with `CostClass` ∈ `None|Identity|Structural|Webhook`), `CompositionInfo` (with `Mode` ∈ `Pipeline|Resources`), `PipelineStep`, `PatchInfo`, and `Diagnostic` (with `Code`, `Severity`, `Message`, `Source`, `Detail`, `Fix`, `Related`).

### IR builder — `pkg/ir/builder.go`

`Builder.Build(docs)` iterates documents, calls `loader.ClassifyDocument`, and dispatches to private `addCRD` / `addXRD` / `addComposition` / `addFunction` / `addProvider` / `addConfiguration` / `addArgoApplication` / `addResource`.

Notable logic: `addCRD` ([builder.go:60](pkg/ir/builder.go)) SHA-256s each version's `schema` block (truncated to 16 bytes) via `hashSchema`, caches into `w.Schemas`, and infers `CostClass` from the conversion strategy at [line 114](pkg/ir/builder.go). `addXRD` treats `referenceable` as the XRD equivalent of `storage`. `addFunction` looks the function name up in a hard-coded map of 9 well-known Crossplane functions → accepted `inputVersions`. `addArgoApplication` reads `argocd.argoproj.io/tracking-method` annotation, defaulting to `"annotation"`.

`pkg/ir/sexpr.go` serializes `World` as an s-expression ([ToSExpr line 19](pkg/ir/sexpr.go)); `DigestWorld` SHA-256s that text and returns `sha256:<hex>`.

### Checker — `pkg/checker/bridge.go`

`Check(w, cfg)` at [bridge.go:30](pkg/checker/bridge.go):

1. `shen.NewRuntime()` — in-process Shen evaluator.
2. Sets global `*strict-conversions*` from config.
3. `rt.LoadFile(kernelDir + "/check.shen")` — loads the kernel entry, which sequentially `(load ...)`s each rule file.
4. `enrichSyncWaves(w)` — populates `ArgoApps[i].SyncWaves` from resource `argocd.argoproj.io/sync-wave` annotations.
5. `resolvePatchTypes(w)` — for each Resources-mode composition patch, resolves source XRD and destination CRD field types via `schemas.ResolveFieldType` and appends a sentinel `TransformInfo{Type:"__resolved_types", Convert:"fromType→toType"}`.
6. `worldToShenValue(w)` — constructs a Shen cons-list tree mirroring `World`; every resource becomes a labeled sub-list.
7. `rt.Call("check-world", worldVal)` — the Shen entry.
8. `valueToDiagnostics(result)` — pattern-matches `[judgment Code Sev Src Msg Detail Fix Related]` back into `types.Diagnostic`.

The pure-Go rule functions `checkR1` … `checkR11` live in [pkg/checker/rules.go](pkg/checker/rules.go). On this branch they are the backing implementation for direct-call unit tests (`TestR1_*` … `TestR11_*`) in `rules_test.go`; the production `Check()` path uses Shen.

### Shen kernel — `kernel/*.shen` + `pkg/shen`

Rule files on base:

```
kernel/check.shen                     top-level loader + check-world entry
kernel/prelude.shen                   datatypes + helpers + judgment constructors
kernel/r1-versions.shen               CRD/XRD version coherence
kernel/r2-conversion.shen             webhook conversion opt-in
kernel/r3-composition-resolves.shen   Composition.compositeTypeRef → XRD
kernel/r4-pipeline-functions.shen     pipeline step fn + input version
kernel/r5-patch-typecheck.shen        patch field-type compatibility
kernel/r6-wave-ordering.shen          Argo sync-wave ordering
kernel/r7-owner-refs.shen             label-tracking conflicts (XPC007)
kernel/r8-v1v2-machinery.shen         v2 XRD machinery placement
kernel/r9-bootstrap.shen              pipeline step required-resources
kernel/r10-secret-taint.shen          secret taint propagation
kernel/r11-temporal.shen              deprecated API / provider versions
kernel/shen-kernel/                   stock Shen 22 kernel (unused at runtime)
```

`pkg/shen` is the Go-native Shen evaluator used at runtime (not the bundled stock kernel). Public types: `Value` (`Sym`, `Str`, `Num`, `Bool`, `*Cons`, `Form`, `*Lambda`, `*BuiltinFn`, `*Defun`). `Evaluator.Eval` ([pkg/shen/eval.go:76](pkg/shen/eval.go)) dispatches on Go type; special forms include `define`, `let`, `if`, `cond`, `and`, `or`, `not`, `do`, `/.`, `lambda`, `load`, `freeze`, `value`, `thaw`. `evalDefine` ([line 219](pkg/shen/eval.go)) parses `pattern -> body` cases with optional `where` guards; pattern variables are detected by leading uppercase letter (`isPatternVariable`, [line 600](pkg/shen/eval.go)). `applyDefun` does the actual pattern matching via `matchPatterns`. `partialApply` handles under-saturated calls by wrapping in a new builtin capturing the args.

`pkg/shen/parser.go` tokenizes Shen source; `\* ... *\` comments are nested-aware and `{ ... }` type signatures are skipped at tokenization. `pkg/shen/runtime.go` registers ~40 built-in primitives (cons/list, arithmetic, strings, predicates, error handling, global state, xpc-specific `string-downcase` / `string-upcase` / `shen.split-string`). `trap-error` at [runtime.go:322](pkg/shen/runtime.go) is a passthrough — it does not actually catch.

### Schemas — `pkg/schemas/fetcher.go`

`Cache` (two-level: in-memory map + on-disk `~/.cache/xpc/schemas/<digest>.json`). `ResolveFieldType(schema, fieldPath)` ([line 101](pkg/schemas/fetcher.go)) walks nested `properties` maps (unwrapping `openAPIV3Schema` wrappers) and returns `FieldTypeString|Integer|Number|Boolean|Object|Array|Unknown`. `TypeAssignable(from, to)` permits type match, either-unknown, or `integer → number`.

### Proof — `pkg/proof/proof.go`

Constants: `KernelVersion = "0.1.0"`, `RulesetVersion = "2026.04"`. `Proof` has `RootDigest`, `Metadata`, `RuleSubtrees map[string]*RuleSubtree` (keyed `XPC001`..), `ResourceSubtrees map[string]*ResourceSubtree` (keyed `file:line`), flat `Tree []string`. `Generate` ([line 105](pkg/proof/proof.go)) hashes each judgment as `Status|RuleID|Resource|Message`, builds per-rule and per-resource subtrees, concatenates sorted leaf digests, and runs pairwise SHA-256 to produce the root (odd nodes self-pair). `Verify()` rebuilds and compares. `DiffProofs(a, b)` classifies each rule as `unchanged | newly satisfied | newly violated`. Persistence is JSON (`MarshalIndent`).

### Snapshot — `pkg/snapshot/snapshot.go`

`Snapshot` captures CRDs, XRDs, Providers (with `Healthy`), Functions (with `Healthy`), Configurations, Compositions, `ArgoTrackingMode`, Schemas, plus `Digest`, `Timestamp`, optional signature fields. `FromWorld(w, clusterName)` ([line 307](pkg/snapshot/snapshot.go)) converts a filesystem-loaded `World` into a snapshot (marking everything healthy). `ComputeDigest()` sorts by natural keys and SHA-256s for order-independent digests. `IsStale(ttl)` — default TTL 15 min. `Diff(a, b)` compares CRD storage versions, conversion cost classes, Providers, Functions, and `ArgoTrackingMode`.

### Report — `pkg/report/reporter.go`

`Format` values: `FormatHuman`, `FormatAgent` (default), `FormatJSON`, `FormatLSP`, `FormatJUnit`, `FormatSARIF`. `reportAgent` emits LLM-dense text with `rule:`, `severity:`, `problem:`, `source:`, `fix:`, `ack:`, `docs:` lines per diagnostic. `reportHuman` adds source excerpts with `^` underlines and `https://xpc.dev/errors/<code>` links. SARIF 2.1.0 output has one rule per unique code and one result per diagnostic with physical location.

### Skills & Testdata

`skills/xpc-edit.md`, `xpc-commit.md`, `xpc-review.md` — agent playbooks instructing an LLM to run `xpc check`, how to use `--proof`, and how to perform MR review via `xpc proof diff`.

Fixtures in `testdata/fixtures/`: `basic/` (valid XRD + Composition + Function), `webhook-conversion/` (R2 trigger: non-storage version with Webhook CRD), `patch-mismatch/` (R5 trigger), `wave-ordering/` (R6 trigger: XR at same wave as XRD).

### Complete data flow

```
YAML → loader.Load* → []loader.LoadedDocument → ir.Builder.Build → *types.World
                                                           │
                                    [optional] snapshot.Load → mergeSnapshotIntoWorld
                                                           │
                                                  checker.Check
                           ├─ shen.NewRuntime + LoadFile("kernel/check.shen")
                           ├─ enrichSyncWaves + resolvePatchTypes
                           ├─ worldToShenValue
                           ├─ rt.Call("check-world", worldVal)
                           └─ valueToDiagnostics
                                                           │
                                            []types.Diagnostic → report.ReportStdout
                                                           │
                                        [--proof] proof.Generate → check.xpcproof
```

---

## Part 2 — Review of PR #3 (`claude/argo-cd-type-model-vCseW`)

**Base**: `claude/build-xpc-type-checker-TfgsT` (`b495e74`)
**Head**: `91becc0` — 2 commits: Phase 0 (obligation framework skeleton + audit rename + refs pilot) and Phase 1 (absorb all R1–R11 + Argo IR extensions)
**Diff**: +3568 / −282 across 33 files.

### Overview

The PR introduces `pkg/obligation/` — a `Generator`/`Registry`/`Run` framework — and ports all 11 legacy rules into 13 generators across 7 category sub-packages (`refs`, `versions`, `trajectory`, `crossapp`, `conversion`, `secretflow`, `deprecation`). It also:

- **Rewrites `pkg/checker/bridge.go`** to a 46-line file that does nothing but call `obligation.Run` through `CheckWithObligations`. The Shen kernel is no longer invoked from `Check()`.
- **Renames** `pkg/proof` → `pkg/audit` (package `proof` → package `audit`). All call sites in `cmd/xpc/main.go` updated.
- **Extends the Argo IR** with `ArgoAppProject`, `ArgoApplicationSet`, multi-source `ArgoApplication`, typed `ArgoSyncOptions`, and renderer tagged-unions (`Helm|Kustomize|Directory|Plugin`).
- **Adds ADR-001** (`docs/adr/001-bounded-obligation-taxonomy.md`) and `docs/obligations.md` with the 12-category taxonomy and R1–R11 absorption map.

### Architectural Changes

#### Obligation data model — `pkg/obligation/obligation.go`

```go
type Category string                 // "A".."L"
type Obligation struct {
    ID         string                // "XPC.<Cat>.<Generator>.<Instance>"
    Category   Category
    Subject    types.SourceLocation
    Claim      string
    Provenance Provenance            // generator, category, 8-byte InputHash
    LegacyCode string                // "XPC003" etc.
    Discharge  func(*Context) Result
}
type Generator interface {
    Name() string
    Category() Category
    Description() string
    Generate(ctx *Context) []Obligation
}
type Context struct {
    World             *types.World
    StrictConversions bool
}
```

`MakeID(cat, generator, instance)` produces `XPC.<Cat>.<Generator>` if instance is empty, else appends a `sanitizeInstance`-cleaned instance. `ContentHash(s)` returns an 8-byte (16 hex chars) SHA-256 prefix used for `Provenance.InputHash`.

#### Registry — `pkg/obligation/registry.go`

Slice + name-keyed map. `Register` panics on duplicate names. A process-global default registry is initialized via `sync.Once`. Sub-packages populate it from `init()`:

```go
// pkg/obligation/refs/register.go
func init() {
    obligation.RegisterDefault(&CompXRDRef{})
    obligation.RegisterDefault(&PipelineFnRef{})
    obligation.RegisterDefault(&PatchCompat{})
}
```

`cmd/xpc/main.go` blank-imports each sub-package so `init()` fires at process start; `pkg/checker/generators_test.go` does the same blank-imports to ensure the default registry is populated during `go test ./pkg/checker/...`.

#### Run loop — `pkg/obligation/run.go`

```go
func Run(reg *Registry, ctx *Context) RunResult {
    // for each generator: gens = reg.All()
    //   for each obligation ob := gen.Generate(ctx):
    //     r := ob.Discharge(ctx)
    //     switch r.Status {
    //       case Satisfied: result.Satisfied++
    //       case Violated:  if r.Diag != nil { r.Diag.Obligation = &ob.Ref(); append }
    //       case Unknown:   result.Unknown++
    //     }
}
type RunResult struct {
    TotalObligations int
    Satisfied, Violated, Unknown int
    Diagnostics      []types.Diagnostic
    ObligationIDs    []string
}
```

`ObligationRef` (new `types.Diagnostic.Obligation *ObligationRef` field in `pkg/types/types.go`) carries `ID`, `Category`, `Generator` for downstream tooling.

#### New `Check()` — `pkg/checker/bridge.go`

Verified on PR head:

```go
// bridge.go (46 lines total)
func Check(w *types.World, cfg Config) ([]types.Diagnostic, error) {
    result := CheckWithObligations(w, cfg)
    return result.Diagnostics, nil
}
func CheckWithObligations(w *types.World, cfg Config) obligation.RunResult {
    reg := obligation.DefaultRegistry()
    ctx := &obligation.Context{World: w, StrictConversions: cfg.StrictConversions}
    return obligation.Run(reg, ctx)
}
```

Imports on PR head: only `pkg/obligation` and `pkg/types`. **`pkg/shen` is not imported** and not loaded. `Config.KernelPath` and `Config.ShenBinary` are retained but unused (dead fields, documented as "reserved for future Shen integration where the kernel holds the canonical obligation taxonomy").

#### Audit package rename

- `pkg/proof/proof.go` → `pkg/audit/proof.go` (package `proof` → `audit`).
- `pkg/proof/proof_test.go` → `pkg/audit/proof_test.go`.
- `pkg/proof` directory is deleted on PR head (verified via `git ls-tree`).
- `cmd/xpc/main.go` imports and call sites updated: `audit.Generate`, `audit.LoadProof`, `audit.DiffProofs`.
- Public API unchanged: `Proof`, `Generate`, `LoadProof`, `DiffProofs`, `(*Proof).Verify`.

#### Argo IR extensions — `pkg/ir/builder.go`, `pkg/types/types.go`, `pkg/loader/loader.go`

New loader classifications: `argo-appproject` and `argo-applicationset` (with `argoproj.io/` API prefix).

New builder entry points: `addArgoAppProject`, `addArgoApplicationSet`. `addArgoApplication` is extended to parse multi-source or single-source via `parseArgoSource()`, which populates a renderer tagged union (`RendererKind` ∈ `Helm|Kustomize|Directory|Plugin`). `parseArgoSyncPolicy()` translates Argo's `"Replace=true"` / `"Prune=true"` strings into typed `ArgoSyncOptions` bool fields. `parseArgoIgnoreDiff()` handles `spec.ignoreDifferences[]`.

New types in `pkg/types/types.go`: `ArgoSource`, `ArgoHelmSource`, `ArgoKustomizeSource`, `ArgoDirectorySource`, `ArgoPluginSource`, `ArgoDestination`, `ArgoSyncPolicy`, `ArgoSyncOptions`, `ArgoRetryPolicy`, `ArgoIgnoreDiff`, `ArgoHook`, `ResourceRef`, `ArgoAppProject`, `ArgoProjectDestination`, `ArgoGroupKind`, `ArgoSyncWindow`, `ArgoApplicationSet`, `ArgoAppSetGenerator`, `ArgoAppSetGitGenerator`, `ArgoAppSetTemplate`.

`ArgoApplication` gains six additive fields (`Project`, `Sources`, `Destination`, `SyncPolicy`, `IgnoreDifferences`, `Hooks`) — existing `Name`, `Namespace`, `TrackingMode`, `SyncWaves`, `Source` unchanged, so prior tests that construct `ArgoApplication` literals still compile. `World` gains `ArgoProjects []ArgoAppProject` and `ArgoAppSets []ArgoApplicationSet`.

### Generator invariants (spot checks)

**`comp-xrd-ref`** (`pkg/obligation/refs/composition_xrd.go`, Category B, absorbs R3) — builds `map[group/kind]*xrdEntry` at `Generate` time with a version-set per entry; discharge checks XRD exists AND version is referenceable. Correct for the invariant. Closed-over map is stale-safe because `Run` calls `Generate` and `Discharge` back-to-back on the same `World`.

**`patch-compat`** (`pkg/obligation/refs/patch_compat.go`, Category B, absorbs R5) — uses `schemas.ResolveFieldType` on source and destination paths. Returns `Satisfied` when either side resolves to `FieldTypeUnknown` (silent-pass on missing schema). When multiple `convert` transforms exist, only the **last one** is applied, matching the legacy single-`__resolved_types`-sentinel behavior in `bridge.go:resolvePatchTypes`.

**`version-coherence`** (`pkg/obligation/versions/version_coherence.go`, Category C, absorbs R1) — CRD: all versions served; exactly one storage. XRD: all versions served; at least one referenceable. The `len(crd.Versions) > 0` guard means a CRD with empty versions passes silently (consistent with the legacy R1 behavior; empty-versions is a loader concern).

**`trajectory-wave-order`** (`pkg/obligation/trajectory/wave_order.go`, Category F, absorbs R6) — builds a `waveMap[kind/name]int` from resource `sync-wave` annotations (default 0 for XRDs), then iterates `w.ArgoApps`, updating the map from each app's `SyncWaves` before emitting per-pair obligations. Uses strict `xrdWave >= resWave` / `fnWave >= compWave` comparisons (strict less-than required).

    Observed concern: the obligation-emission loop is **inside** the per-application `for` loop, so with N apps you get N copies of each (XRD, XR) pair's obligation. In addition, if two apps assign different `sync-wave` values for the same key, the last-seen value wins for any obligations generated in subsequent app iterations. The legacy R6 Shen rule generated one judgment per (XRD,XR) pair globally; this PR's generator multiplies by `len(ArgoApps)`. A reviewer should confirm this against a multi-app fixture.

**`label-tracking`** (`pkg/obligation/crossapp/label_tracking.go`, Category G, absorbs R7) — discharge always returns `Violated`. The filter lives entirely inside `Generate` (it only emits an obligation when `app.TrackingMode == "label"` and a matching composition exists). Structurally different from other generators; `Satisfied` is never returned, so `RunResult.Satisfied` never increments for this generator. Same pattern in `secret-source-sink` (Category K).

### Discharge behavior under strict mode

`conversion/cost_opt_in.go` discharges in this order: (1) if `ctx.StrictConversions`, unconditionally `Violated`; (2) else if `xpc.dev/accept-conversion-webhook: "true"` annotation present, `Satisfied`; (3) else `Violated` with a suggested-fix annotation. This matches the legacy Shen R2.

### Test coverage

**Added**: `pkg/obligation/refs/composition_xrd_test.go` — 6 tests covering empty world, valid ref, missing XRD, unreferenceable version, structured ID format, and `Run` integration (verifies `Diagnostic.Obligation.Generator == "comp-xrd-ref"` and `.Category == "B"`).

**Blank-import file**: `pkg/checker/generators_test.go` — contains only `import _ "..."` lines to force `init()` execution so the default registry is populated when checker tests run.

**Existing tests**: `pkg/checker/rules_test.go` is **not modified** by the PR. Verified on PR head — the test file still calls the legacy `checkR1(world)`, `checkR2(world, ...)`, …, `checkR7(world)` directly (lines 38, 46, 55, 76, 87, 97, 118, 127, 142, 162, 184). Only `TestR10_*` and `TestR11_*` call `Check(world, Config{})` (lines 192, 205) — which now routes through the obligation framework. This means:

- `TestR1` .. `TestR7` exercise only the **legacy** Go functions in `pkg/checker/rules.go`. They are dead-end reachability tests for code that is no longer wired to production.
- `TestR10`, `TestR11` exercise the **new** secretflow and deprecation generators.
- `TestR8`, `TestR9` — would need inspection to determine which path they exercise; they are not in the `checkR*`-direct grep above.

This contradicts the "Test helper imports generators for checker integration tests" commit message implication that all R1–R11 tests now exercise the new framework. Only R10/R11 actually do.

**No dedicated unit tests** for: `patch_compat`, `pipeline_fn_ref`, `version_coherence`, `machinery`, `wave_order`, `bootstrap`, `label_tracking`, `cost_opt_in`, `source_sink`, `api_calendar`. Coverage for these generators is only via `TestR10/R11` integration (for those two) and via hand-execution by reviewer; the others have no test path.

### Notable discrepancies between PR description and PR head

1. **`pkg/checker/legacy.go` does not exist** on the PR head. The PR body states "Shen kernel bridge code moved to `legacy.go`". Actual state: the Shen bridge code is **deleted** from `bridge.go`, not moved to a new file. `pkg/checker/rules.go` is kept with a `// LEGACY:` banner and the R1→R11 absorption map.
2. **`kernel/r10-secret-taint.shen` and `kernel/r11-temporal.shen` are deleted** in the PR (verified via `git ls-tree`). The other `kernel/r1..r9` files remain on disk but are never loaded.
3. **`pkg/shen` is NOT deleted.** The in-process Shen evaluator still exists on the PR head — it is just no longer imported by `pkg/checker`. A reviewer should decide whether to preserve it (for the stated "future Shen integration" in `Config.KernelPath` comments) or remove it alongside the unused kernel files.

### Provenance & Audit

Diagnostics gain a new `Obligation *ObligationRef` field carrying `{ID, Category, Generator}`. The legacy `Code` field (e.g., `XPC003`) is still populated by each generator's discharge function, so external consumers (agent output, SARIF, LSP) that only inspect `d.Code` see the legacy codes unchanged.

**However**, `pkg/audit/proof.go` is **not updated** to consume the richer provenance. It still groups judgments into `RuleSubtrees` keyed by `XPC001`..`XPC011` and a hardcoded `allRules` list. The `RunResult.ObligationIDs` slice is populated but **not passed to `audit.Generate`** — `cmd/xpc/main.go` extracts only `result.Diagnostics` from `CheckWithObligations` and feeds that to `audit.Generate`. Consequence: the audit log's Merkle tree does not yet encode obligation IDs. When future generators (Categories D, E, H, I) emit diagnostics with codes outside `XPC001–XPC011`, they will silently be omitted from the hardcoded `RuleSubtrees` loop and appear only in `ResourceSubtrees`.

### Things a human reviewer should double-check

1. **Run `go test ./...` on the PR head.** The claim "All existing tests pass" in the commit message is worth re-verifying locally, especially because `rules_test.go` exercises paths that may now behave differently (`TestR10` / `TestR11` routed through obligation framework; the rest still through legacy Go rules).
2. **Wave-order duplicate obligations.** With multiple `ArgoApplication`s, `WaveOrder.Generate` emits N copies of each (XRD, XR) pair's obligation and the captured `resWave`/`xrdWave` values depend on app ordering in `w.ArgoApps`. Reviewer should confirm `TestR6_WaveOrdering` still passes (it uses a single-app fixture, so the bug likely does not manifest there).
3. **`pkg/shen` and `kernel/` orphaning.** PR head keeps `pkg/shen/*.go` and `kernel/r1..r9 + check.shen + prelude.shen + shen-kernel/`. None of these are loaded at runtime anymore. Decide: delete now, or keep per the ADR note that "the kernel should eventually hold the canonical obligation taxonomy."
4. **PR-body vs actual scope mismatch.**
   - Claim: "Shen kernel bridge code moved to `legacy.go`" — file doesn't exist.
   - Claim: "audit package rename (package remains `audit` for clarity)" — correctly renamed, but the `pkg/audit/proof.go` content still uses the term "judgment" and Merkle-tree-rooted-at-rule organization that mirrors the old proof file; no obligation-awareness was added to this file.
5. **Unconditional `Violated` discharge.** `label_tracking` and `source_sink` always return `Violated` when an obligation is emitted. This is functionally correct (the filter is in `Generate`), but it means the invariant "`Satisfied + Violated + Unknown == TotalObligations`" masks the fact that these generators never contribute to `Satisfied`. Reviewer should confirm this is the intended accounting.
6. **`sanitizeInstance` duplicated in each sub-package.** Seven copies of the same 7-line helper. Candidate for a shared helper in `pkg/obligation` once a housekeeping PR lands.
7. **`ArgoProjects` and `ArgoAppSets` parsed but unused.** The new Argo IR fields are populated by the builder but no generator in this PR reads them. Category D (`AppProject-constraint`) is documented in `docs/obligations.md` but has no generator registered yet. Confirm this is intentional (PR description frames it as groundwork).
8. **`pkg/checker/rules.go` is dead code** (623 lines retained with `// LEGACY:` banner). It is still exercised by `rules_test.go` but not by production `Check()`. Consider scheduling deletion after the obligation framework has parity-validation in CI.
9. **`Config.KernelPath` and `Config.ShenBinary`** remain on `Config` but are unused. `cmd/xpc/main.go` still auto-detects the kernel path and sets `KernelPath` — harmless but dead.

### Code references

- `pkg/obligation/obligation.go:2036` — `Obligation` struct
- `pkg/obligation/obligation.go:2081` — `Generator` interface
- `pkg/obligation/registry.go:2814` — `Register` duplicate-name panic
- `pkg/obligation/run.go:2883` — `RunResult`
- `pkg/checker/bridge.go` (PR head, 46 lines total) — new `Check` entry
- `pkg/checker/rules.go:1–26` (PR head) — `LEGACY:` banner + absorption map
- `pkg/obligation/refs/composition_xrd_test.go` — pilot test (only generator-level test in the PR)
- `pkg/ir/builder.go:836` (PR head) — `addArgoAppProject`, `addArgoApplicationSet` dispatch
- `pkg/loader/loader.go:1328` (PR head) — `argo-appproject`, `argo-applicationset` classifications
- `pkg/audit/proof.go` — renamed file, public API unchanged
- `docs/adr/001-bounded-obligation-taxonomy.md` — architectural decision record
- `docs/obligations.md` — Category A–L reference + R1–R11 absorption map

### Architecture Documentation

The obligation framework is designed so that every diagnostic is traceable to (Category, Generator, Instance) for audit. `Context.World` and `Context.StrictConversions` are the only inputs a generator can read. Generators are registered at `init()` via blank imports, producing a **stable, enumerable** list of checks — the framework enforces the ADR's "no ad-hoc rules" policy by construction. The `Obligation.Discharge` closure pattern lets the generator capture per-instance state (e.g., schema maps, wave maps) without re-computing them per discharge.

### Historical Context (from thoughts/)

No prior `thoughts/` directory existed in this repo before this research document. Historical context on the migration is captured in the two commits on the PR branch and in `docs/adr/001-bounded-obligation-taxonomy.md`.

### Related Research

None — first research document in this repository.

### Open Questions

1. Is `pkg/shen` intended to be preserved for a future "Shen holds the canonical taxonomy" direction, or is it eligible for deletion now?
2. Should `pkg/checker/rules.go` be deleted, or left as a dual-implementation for parity validation in CI?
3. Should `rules_test.go` be migrated to exercise `Check()` (the new path) rather than the legacy `checkR*` functions directly?
4. Should `audit.Generate` be updated to consume `ObligationIDs` / `Obligation.Provenance` so the Merkle tree encodes the richer provenance?
5. What is the plan for Categories A, D, E, H, I in the taxonomy? (No generators for these in this PR.)
