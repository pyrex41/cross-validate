---
date: 2026-04-17T17:41:46+0000
researcher: Reuben Brooks
git_commit: 26c93edd56cf647744e598b37c5c5a312e797875
branch: claude/phase1-cleanup
repository: pyrex41/cross-validate
topic: "Vision recap — what is xpc and where are we after the back-and-forth?"
tags: [research, codebase, xpc, vision, architecture, shen-as-spec, obligation-framework, history]
status: complete
last_updated: 2026-04-17
last_updated_by: Reuben Brooks
last_updated_note: "Added follow-up rule-by-rule audit of what's actually in the Shen kernel, classified by 'real-time/space interaction invariant' vs 'static structural check'."
---

# Research: Vision recap after the Shen ⇄ obligation framework ⇄ Shen pivot

**Date**: 2026-04-17 17:41 UTC
**Researcher**: Reuben Brooks
**Git Commit**: `26c93ed` (HEAD of `claude/phase1-cleanup`, with uncommitted phase-1 cleanup deletions in the working tree)
**Branch**: `claude/phase1-cleanup`
**Repository**: pyrex41/cross-validate

## Research Question
"wtf are we doing here -- we've gone back and forth so much I've lost track of the vision."

## Summary

**The vision has stayed constant. The implementation strategy has flipped three times.**

`xpc` is a Go CLI that type-checks Crossplane + Argo CD YAML before it reaches a cluster, against a **bounded taxonomy of 12 obligation categories (A–L)** documented in `docs/adr/001-bounded-obligation-taxonomy.md` and `docs/obligations.md`. The completeness claim — "for input I and cluster context C, if xpc returns no errors, no obligation in any modeled category is violated" — has not changed. R1–R11 (the original numbered rules) are how the categories are populated today; the taxonomy is the long-term frame.

What *has* moved is **where the rules live**. There have been three implementations of the same set of checks:

1. **Pure-Go functions** (`pkg/checker/rules.go`, `checkR1`…`checkR11`) — the original baseline.
2. **Go-native obligation framework** (`pkg/obligation/{refs,versions,trajectory,crossapp,conversion,secretflow,deprecation}/`) — generators registered via `init()` blank imports, each emitting structured `Obligation`s with discharge functions.
3. **Shen kernel** (`kernel/*.shen` evaluated in-process by a Shen-on-Go runtime) — the canonical declarative specification of every rule.

The current branch (`claude/phase1-cleanup`, working tree) is collapsing the dual implementation by **deleting the obligation framework Go code and routing `Check()` through the Shen kernel exclusively**. The taxonomy concept (Category × Generator × Instance, Merkle-rooted audit) is preserved as the *architectural shape*, but the Shen kernel is now the source of truth for the rule logic. The bridge in `pkg/checker/bridge.go` is a translator: it serializes `World` to Shen objects, calls `(check-world ...)`, and translates judgments back into `types.Diagnostic`s. The `RunResult` struct that the obligation framework returned is preserved as a stub in `pkg/checker/result.go` so callers don't have to change.

In short: **"Shen-as-spec"** is the current bet. The Shen kernel IS the obligation taxonomy specification; Go is a dumb translator + IR loader + UI.

---

## The back-and-forth, in chronological order

Every commit-level move below is verifiable from `git log --all --graph` and the file deltas attached to each.

### Phase 0 — Pure Go baseline (PR #1, merged as `7a0d22b`)

- `51da085` "Implement xpc type checker for Crossplane + Argo CD" — the baseline. R1–R9 implemented as standalone Go functions in `pkg/checker/rules.go`. `Check()` calls each in sequence.
- `1200ed5` "Add snapshot system, proof system, R10/R11 rules, agent output format, and CLI commands" — Add R10 (secret taint) and R11 (deprecation), the snapshot system (`pkg/snapshot`), the Merkle proof system (`pkg/proof`), and the LLM-friendly agent output format.

State at this point: 11 hand-written Go functions, no taxonomy, no Shen, no obligation framework.

### Phase 1 — Shen as a second backend (PR #2, merged as `b495e74`)

- `94ab3fb` "Replace Go rule fallback with in-process Shen runtime" — Build `pkg/shen` (a *custom* Go-native Shen evaluator), translate `World` to Shen cons-list values, evaluate `kernel/check.shen` which loads `r1..r11.shen`, and convert judgments back. Production `Check()` now routes through Shen. The pure-Go rule functions stay in `pkg/checker/rules.go` but are only called by direct unit tests (`TestR1_*` … `TestR11_*` in `rules_test.go`).

State at this point: dual implementation. Shen is canonical for production; Go rules linger as test-only.

### Phase 2 — Obligation framework on top of Go (PR #3, merged as `7e51e1e`)

- `9cd1765` "Phase 0: bounded obligation taxonomy and framework" — Adds `docs/adr/001-bounded-obligation-taxonomy.md` defining categories A–L. Builds `pkg/obligation/` (Generator interface, Registry, Run loop, Obligation/Result types). Renames `pkg/proof` → `pkg/audit`.
- `91becc0` "Phase 1: absorb R1-R11 into obligation framework, extend Argo CD IR" — Ports all 11 rules into 13 generators across 7 sub-packages. **Shen is removed from `Check()`**: the bridge becomes a 46-line file that does nothing but call `obligation.Run`. Argo IR substantially extended (`ArgoAppProject`, `ArgoApplicationSet`, multi-source, typed `ArgoSyncOptions`).

State at this point: triple implementation on disk. Production `Check()` routes through the obligation framework. The Shen kernel files (`kernel/*.shen` and `pkg/shen`) are still in the repo but no longer loaded. The legacy Go functions in `pkg/checker/rules.go` are still in the repo but only exercised by unit tests.

This pre-cleanup state is recorded in the commits `9cd1765` and `91becc0`.

### Phase 3 — Shen reasserted as the spec (current branch `claude/phase1-cleanup`)

- `f254d83` "Phase 1 cleanup: single-impl checker, obligation-aware audit" — committed.
- **Working-tree deletions** (uncommitted on top of `26c93ed`): the entire `pkg/obligation/` tree is being deleted (verified by `git status` showing `D` on every file in `pkg/obligation/{conversion,crossapp,deprecation,refs,secretflow,trajectory,versions}/` and the framework root). `pkg/checker/generators_test.go` is also deleted.
- **Working-tree additions**: `internal/shenfull/` (15 files: a vendored bootstrap of the Shen language on top of `tiancaiamao/shen-go/kl`), and `pkg/checker/result.go` (a stub `RunResult` struct preserved from the obligation framework).
- **`go.mod` change**: switches from the custom `pkg/shen` evaluator to `github.com/tiancaiamao/shen-go v0.0.0-20251114030759-7a6a67ac131d`. The current `kl`-based bridge replaces the prior in-tree `pkg/shen` runtime.
- **`pkg/checker/bridge.go` rewrite**: 671 lines (vs. 46 on PR #3). Now boots the Shen runtime via `shenfull.Init`, loads `kernel/check.shen`, serializes `World` → Shen `kl.Obj` cons-lists (with `crd-fact`, `xrd-fact`, `composition-fact`, `function-fact`, `argo-app-fact` sections), calls `(check-world world-obj)`, and decodes the returned judgment list. The `enrichSyncWaves` and `resolvePatchTypes` passes are **restored verbatim from the pre-obligation-framework version** of the bridge (commit `94ab3fb`).
- **`cmd/xpc/main.go` change**: drops the seven `_ "…/pkg/obligation/<sub>"` blank imports.

State now: single implementation. The Shen kernel under `kernel/` is the canonical specification of all 11 rules. The obligation framework is gone as code, but its shape is preserved in two places:

- `pkg/checker/result.go` — `RunResult` struct kept identical to the deleted `obligation.RunResult` so the Go API surface is unchanged.
- `pkg/checker/bridge.go:597` — `obligationRefForCode(code string) *types.ObligationRef` — a **hardcoded table** mapping `XPC001`…`XPC011` back to `(Category, Generator)` so the obligation provenance still rides along on every `Diagnostic`. This is the bridge between the Shen kernel's emit-by-code style and the taxonomy's expectation of `Obligation.Category` / `Obligation.Generator` provenance.

---

## What "Shen-as-spec" means in practice

Read this section as a contract, not a wish list. Every claim below is in code on the working-tree of `claude/phase1-cleanup`.

### The Shen kernel is the canonical rule set

`kernel/check.shen` ([check.shen:15-27](kernel/check.shen)) loads `prelude.shen` and 11 rule files (`r1-versions.shen` through `r11-api-deprecation.shen`). The top-level entry is `(check-world WorldExpr)`, which extracts named sections out of the IR and runs `(check-r1 …)` … `(check-r11 …)` per rule, appending all judgments. There is no Go rule code in the call path.

Each rule file defines its rule in pattern-matching Shen with a typed signature, e.g. `kernel/r3-composition-resolves.shen` for "composition references a real, referenceable XRD".

### Go is the IR loader, the bridge, and the UI

The Go side has four jobs and only four:

1. **Load YAML → IR.** `pkg/loader/loader.go` + `pkg/ir/builder.go` produce a `*types.World`. (Unchanged across all three phases.)
2. **Enrich IR with Go-only computations.** `enrichSyncWaves` ([bridge.go:160](pkg/checker/bridge.go)) populates per-app sync-wave tables from annotations; `resolvePatchTypes` ([bridge.go:214](pkg/checker/bridge.go)) walks the schema cache to compute `fromType→toType` strings and stamps them onto each patch as a sentinel `__resolved_types` transform. These are pre-computed because the Shen kernel does not get schema-walking primitives — it gets *resolved* facts.
3. **Translate `World` to Shen `kl.Obj`** ([bridge.go:301-552](pkg/checker/bridge.go)). The world becomes a tagged cons-list: `(world (crds …) (xrds …) (compositions …) … (resolved-patches …))`. Every CRD/XRD/Composition/Function/Resource/ArgoApp becomes a `*-fact` sub-list. Order is sorted for determinism.
4. **Translate judgments back.** `objToDiagnostics` ([bridge.go:568](pkg/checker/bridge.go)) walks the returned `(judgment Code Sev Src Msg Detail Fix Related)` tuples and produces `[]types.Diagnostic`. Each one is annotated with an `Obligation *ObligationRef` derived from the legacy XPC code (`obligationRefForCode`, [bridge.go:597](pkg/checker/bridge.go)).

The Go side also owns: CLI dispatch (`cmd/xpc/main.go`), reporters (`pkg/report`), audit/Merkle artifacts (`pkg/audit` — renamed from `pkg/proof` in PR #3), snapshot system (`pkg/snapshot`), and the schema cache (`pkg/schemas`). None of these have been touched on this branch.

### The Shen runtime is `tiancaiamao/shen-go`, not the in-tree evaluator

This is the second sub-pivot inside Phase 3 worth flagging. PR #2's Shen path used `pkg/shen`, an evaluator written specifically for this project. The current branch swaps to the upstream `github.com/tiancaiamao/shen-go` library plus a vendored bootstrap (`internal/shenfull/`).

`internal/shenfull/init.go` ([init.go:21](internal/shenfull/init.go)) bootstraps Shen on top of `kl.ControlFlow` by loading 14 generated `*Main` entry points (`TopLevelMain`, `CoreMain`, `SysMain`, `SequentMain`, `YaccMain`, `ReaderMain`, `PrologMain`, `TrackMain`, `LoadMain`, `WriterMain`, `MacrosMain`, `DeclarationsMain`, `TStarMain`, `TypesMain`) — these mirror `shen-go`'s `cmd/shen/main.go regist` sequence but surface errors to the caller instead of printing.

`pkg/checker/bridge.go:initShen` ([bridge.go:45](pkg/checker/bridge.go)) calls `shenfull.Init(&shenCF)`, then `chdir`s into the kernel directory (because Shen's `read-file` opens paths literally, so `(load "prelude.shen")` only works when cwd is the kernel dir), runs `(load "check.shen")`, and `chdir`s back. The `sync.Once` guarantees this happens once per process.

The custom `pkg/shen` evaluator is **gone** from this branch (it was present on PR #3 and earlier).

### The obligation taxonomy survives, even though the framework code does not

The taxonomy's value was never the Go framework; it was the *frame*: 12 categories with documented scopes, a discipline that says no check exists outside the taxonomy, and structured `Category.Generator.Instance` IDs in every diagnostic.

That frame is preserved in:

- `docs/adr/001-bounded-obligation-taxonomy.md` — the architectural decision (still authoritative).
- `docs/obligations.md` — the reference for categories A–L and the R1→category absorption map.
- `pkg/types/types.go` — the `ObligationRef` struct (`ID`, `Category`, `Generator`) on every `Diagnostic`.
- `pkg/checker/bridge.go:597` — the hardcoded `obligationRefForCode` table that re-attaches taxonomy provenance to each `XPC00x` code that the Shen kernel emits.
- `pkg/checker/result.go` — the `RunResult` struct (`TotalObligations`, `Satisfied`, `Violated`, `Unknown`, `ObligationIDs`) is preserved as the Go-side return shape, even though the Shen kernel does not yet populate the count fields. Comment on `result.go:7-8`: "*Phase 4 of the shen-as-spec migration moved this type into the checker package so the bridge no longer depends on the obligation framework.*"

So: the **discipline** (Cat × Gen × Inst, audit-friendly provenance) is alive in the Go data model. The **enforcement mechanism** for that discipline shifts from Go (compile-time: Generators registered via init()) to Shen (the kernel must emit judgments with codes the bridge knows how to map). That trade-off is not yet documented in an ADR.

---

## Why the back-and-forth happened

The repo has no decision log entries for the pivots, so the following is reconstructed from commit messages and file deltas, not first-hand notes. Treat as a hypothesis you can confirm/correct.

1. **Pure Go (Phase 0) → Shen (Phase 1):** Driven by the desire for a *declarative* rule language separated from the Go infrastructure. Shen offers pattern matching + sequent calculus + a typed-functional style that matches the "rule" abstraction better than imperative Go loops. PR #2's commit message frames this as "Replace Go rule fallback with in-process Shen runtime."
2. **Shen (Phase 1) → Obligation Framework (Phase 2):** Driven by the need to make a *completeness claim* and to give every diagnostic structured provenance. ADR-001 makes the case explicitly: "There is no structural relationship between the rules, no story for why R11 exists and R12 doesn't, and no mechanism to claim 'we check everything in category X.'" The obligation framework was the answer — but it expressed the rules in *Go again*, duplicating the Shen kernel.
3. **Obligation Framework (Phase 2) → Shen-as-Spec (Phase 3, current):** Driven by the realization that the obligation framework and the Shen kernel were both implementing the same rule set. Two sources of truth means drift. The current branch picks Shen as the canonical spec (declarative; the rule logic lives in the kernel) and keeps the taxonomy as the *Go-side data model* (every diagnostic has provenance), but throws away the duplicate Go rule code.

The thing that has not moved across all three pivots is the IR (`pkg/types/types.go`, `pkg/ir/builder.go`, `pkg/loader/loader.go`), the snapshot system (`pkg/snapshot`), the audit/proof system (`pkg/audit`, formerly `pkg/proof`), the schema cache (`pkg/schemas`), the report formats (`pkg/report`), and the CLI surface (`cmd/xpc/main.go` subcommands: `check`, `dump-ir`, `snapshot`, `verify`, `proof`, `bisect`, `explain`, `version`).

---

## Where things live right now (orientation map)

```
cross-validate/
├── cmd/xpc/main.go               CLI dispatch — unchanged across pivots
├── kernel/                       SHEN — the canonical spec
│   ├── check.shen                  top-level: loads prelude + r1..r11, defines check-world
│   ├── prelude.shen                Shen helpers: judgment ctor, list ops, etc.
│   ├── r1-versions.shen            \
│   ├── r2-conversion.shen          |
│   ├── r3-composition-resolves.shen|
│   ├── r4-pipeline-functions.shen  |
│   ├── r5-patch-typecheck.shen     | the 11 rules, pattern-matching style
│   ├── r6-wave-ordering.shen       |
│   ├── r7-owner-refs.shen          |
│   ├── r8-v1v2-machinery.shen      |
│   ├── r9-bootstrap.shen           |
│   ├── r10-secret-taint.shen       |
│   └── r11-api-deprecation.shen    /
│
├── internal/shenfull/            SHEN BOOTSTRAP — vendored from tiancaiamao/shen-go
│   ├── init.go                     Init(*kl.ControlFlow): runs the 14 *Main loaders
│   └── (14 generated *.go)         core, sys, sequent, yacc, reader, prolog, …
│
├── pkg/
│   ├── checker/
│   │   ├── bridge.go               THE BRIDGE: World ⇄ Shen kl.Obj, judgments → Diagnostics
│   │   ├── result.go               RunResult struct (preserved from deleted obligation pkg)
│   │   └── check_test.go           integration tests — see `git status` for current shape
│   ├── ir/builder.go               YAML doc → World construction
│   ├── loader/loader.go            YAML parsing + classification (crd|xrd|composition|…)
│   ├── types/types.go              IR types: World, CRDInfo, CompositionInfo, Diagnostic, ObligationRef, …
│   ├── schemas/fetcher.go          content-addressed schema cache, ResolveFieldType
│   ├── snapshot/snapshot.go        cluster-state snapshot, FromWorld, Diff, IsStale
│   ├── audit/proof.go              Merkle-tree audit artifact (was pkg/proof until PR #3)
│   └── report/reporter.go          Format ∈ {Human, Agent, JSON, LSP, JUnit, SARIF}
│
├── docs/
│   ├── obligations.md              Categories A–L reference + R1..R11 absorption map
│   └── adr/001-bounded-obligation-taxonomy.md   architectural decision
│
├── skills/                       LLM agent playbooks (xpc-edit, xpc-commit, xpc-review)
├── testdata/fixtures/            R-trigger fixtures: basic, webhook-conversion, patch-mismatch, wave-ordering
└── thoughts/shared/research/     research docs (this one + 2026-04-17-full-codebase-review.md)
```

What used to be there but is being removed (working tree of this branch):

```
pkg/obligation/                  DELETED — replaced by Shen kernel + Go provenance table
├── obligation.go
├── registry.go
├── run.go
├── refs/{composition_xrd.go, pipeline_fn_ref.go, patch_compat.go, register.go, …}
├── versions/{version_coherence.go, machinery.go, register.go}
├── trajectory/{wave_order.go, bootstrap.go, register.go}
├── crossapp/{label_tracking.go, register.go}
├── conversion/{cost_opt_in.go, register.go}
├── secretflow/{source_sink.go, register.go}
└── deprecation/{api_calendar.go, register.go}

pkg/checker/generators_test.go  DELETED — was only a blank-import file to populate registry
```

What's still around that the current branch hasn't touched but was orphaned by the migration (worth flagging as still-on-disk dead/legacy code):

- `pkg/checker/rules.go` and `pkg/checker/rules_test.go` — the original pure-Go rule functions and their direct unit tests. Per the previous research doc, these were already orphaned by PR #3 (`Check()` no longer calls them). Have not been re-checked on this branch in this research session.

---

## Detailed Findings

### The current `Check()` data flow

```
YAML → loader.Load* → []loader.LoadedDocument → ir.Builder.Build → *types.World
                                                           │
                                    [optional] snapshot.Load → mergeSnapshotIntoWorld
                                                           │
                                                  checker.Check
                           ├─ initShen(cfg.KernelPath)                 (sync.Once)
                           │   ├─ shenfull.Init(&shenCF)               (vendored Shen bootstrap)
                           │   ├─ chdir kernelDir; (load "check.shen") (loads prelude + r1..r11)
                           │   └─ chdir back
                           ├─ enrichSyncWaves(w)                       (annotation → SyncWaves)
                           ├─ resolvePatchTypes(w)                     (schema walk → __resolved_types sentinel)
                           ├─ worldToShenObj(w, strict)                (cons-list serialization)
                           ├─ kl.Call(&shenCF, check-world, worldObj)  (THE rule evaluation)
                           ├─ objToDiagnostics(result)                 (judgment tuples → []Diagnostic)
                           └─ buildRunResult(diags)                    (Phase 3a stub: only Diagnostics field is filled)
                                                           │
                                            []types.Diagnostic → report.ReportStdout
                                                           │
                                        [--proof] audit.Generate → check.xpcproof
```

References:
- [pkg/checker/bridge.go:119](pkg/checker/bridge.go) — `Check`
- [pkg/checker/bridge.go:127](pkg/checker/bridge.go) — `CheckWithObligations`
- [pkg/checker/bridge.go:45](pkg/checker/bridge.go) — `initShen`
- [pkg/checker/bridge.go:160](pkg/checker/bridge.go) — `enrichSyncWaves`
- [pkg/checker/bridge.go:214](pkg/checker/bridge.go) — `resolvePatchTypes`
- [pkg/checker/bridge.go:301](pkg/checker/bridge.go) — `worldToShenObj`
- [pkg/checker/bridge.go:568](pkg/checker/bridge.go) — `objToDiagnostics`
- [pkg/checker/bridge.go:597](pkg/checker/bridge.go) — `obligationRefForCode` (Code → ObligationRef table)
- [pkg/checker/bridge.go:678](pkg/checker/bridge.go) — `buildRunResult` (stub: count fields are zero)
- [kernel/check.shen:51](kernel/check.shen) — `check-world`

### The IR contract between Go and Shen

The Shen side does not see the Go `World` struct; it sees a tagged cons-list. The schema is fixed in `worldToShenObj` ([bridge.go:418](pkg/checker/bridge.go)):

```
(world
  (crds            (crd-fact Group Kind Scope (Versions...) Conversion Source) ...)
  (xrds            (xrd-fact Group Kind Scope APIVersion (Versions...) Source) ...)
  (compositions    (composition-fact Name (gvk G V K) Mode (Steps...) Source) ...)
  (functions       (function-fact Name Package (InputVersions...) Source) ...)
  (providers       (provider-fact Name Package Source) ...)
  (configurations  (configuration-fact Name Package Source) ...)
  (resources       (resource-fact APIVersion Kind Name Namespace (Annotations...) Source) ...)
  (argo-apps       (argo-app-fact Name TrackingMode (SyncWaves...) Source) ...)
  (schemas         )                  ; intentionally empty; pre-resolved in resolved-patches below
  (resolved-patches (resolved-patch CompName Source FromPath ToPath FromType ToType) ...))
```

A judgment comes back as `(judgment Code Sev (source File Line) Msg Detail Fix Related)`.

This is the contract. If a future generator needs facts the Shen kernel doesn't have, you either (a) add another section to `worldToShenObj` and a corresponding `extract-section` in `check.shen`, or (b) pre-compute the fact in Go and stamp it onto an existing record (the `__resolved_types` precedent on `Patch.Transforms`). Option (b) is the established escape hatch when the kernel needs Go-only info (schema walking, here).

### What's in the Argo IR right now (legacy of PR #3, intact on this branch)

PR #3 substantially extended the Argo IR. Those types are still in `pkg/types/types.go` and the loader/builder support for them is intact:

- `ArgoApplication` has `Project`, `Sources` (multi-source), `Destination`, `SyncPolicy`, `IgnoreDifferences`, `Hooks` in addition to the original `Name`, `Namespace`, `TrackingMode`, `SyncWaves`, `Source`.
- `World` has `ArgoProjects []ArgoAppProject` and `ArgoAppSets []ArgoApplicationSet`.
- New types: `ArgoSource`, `ArgoHelmSource`, `ArgoKustomizeSource`, `ArgoDirectorySource`, `ArgoPluginSource`, `ArgoDestination`, `ArgoSyncPolicy`, `ArgoSyncOptions`, `ArgoRetryPolicy`, `ArgoIgnoreDiff`, `ArgoHook`, `ResourceRef`, `ArgoAppProject`, `ArgoProjectDestination`, `ArgoGroupKind`, `ArgoSyncWindow`, `ArgoApplicationSet`, `ArgoAppSetGenerator`, `ArgoAppSetGitGenerator`, `ArgoAppSetTemplate`.

The Shen kernel currently consumes only the original fields (it sees `argo-app-fact Name TrackingMode (SyncWaves...) Source`). The richer types are populated by the builder but not yet surfaced to Shen — they are groundwork for Categories D, E, H (AppProject constraints, sync-option interactions, rendering) that the existing 11 rules don't touch.

### The taxonomy → code mapping (where the obligation provenance comes from now)

Hardcoded in `bridge.go:600-613`:

| Code  | Category | Generator                       | Original rule |
|-------|----------|----------------------------------|---------------|
| XPC001| C        | version-coherence                | R1            |
| XPC002| J        | conversion-cost-opt-in           | R2            |
| XPC003| B        | comp-xrd-ref                     | R3            |
| XPC004| B        | pipeline-fn-ref                  | R4            |
| XPC005| B        | patch-compat                     | R5            |
| XPC006| F        | trajectory-wave-order            | R6            |
| XPC007| G        | cross-app-label-tracking         | R7            |
| XPC008| C        | crossplane-machinery-placement   | R8            |
| XPC009| F        | trajectory-bootstrap             | R9            |
| XPC010| K        | secret-source-sink               | R10           |
| XPC011| L        | api-deprecation-calendar         | R11           |

Codes outside this table return `nil` from `obligationRefForCode` — i.e. the diagnostic still ships, but with no obligation provenance.

---

## Code References (current branch, working tree)

- [`pkg/checker/bridge.go`](pkg/checker/bridge.go) — the entire Shen bridge: `initShen`, world serialization, judgment decoding, `obligationRefForCode`
- [`pkg/checker/result.go`](pkg/checker/result.go) — `RunResult` struct (preserved from the deleted obligation framework)
- [`internal/shenfull/init.go`](internal/shenfull/init.go) — Shen language bootstrap on top of `tiancaiamao/shen-go/kl`
- [`kernel/check.shen`](kernel/check.shen) — top-level Shen entry: loads prelude + r1..r11, defines `check-world`
- [`kernel/prelude.shen`](kernel/prelude.shen) — Shen-side helpers (judgment constructor, list ops)
- [`kernel/r1-versions.shen`](kernel/r1-versions.shen) … [`kernel/r11-api-deprecation.shen`](kernel/r11-api-deprecation.shen) — the 11 rules
- [`docs/obligations.md`](docs/obligations.md) — categories A–L reference + R1–R11 absorption map
- [`docs/adr/001-bounded-obligation-taxonomy.md`](docs/adr/001-bounded-obligation-taxonomy.md) — architectural decision
- [`pkg/types/types.go`](pkg/types/types.go) — IR types including `Diagnostic.Obligation *ObligationRef`
- [`go.mod`](go.mod) — `require github.com/tiancaiamao/shen-go v0.0.0-20251114030759-7a6a67ac131d`
- [`cmd/xpc/main.go`](cmd/xpc/main.go) — CLI surface (the `pkg/obligation/*` blank imports were removed in this branch's working tree)

---

## Architecture Documentation

**The architecture in one sentence:** Go owns the IR and the world model; Shen owns the rules; an `ObligationRef` carries taxonomy provenance through every diagnostic.

**The contract surface between Go and Shen:** the tagged cons-list in `worldToShenObj` (the IR-on-the-wire) and the judgment tuple format in `objToDiagnostics`. Anything else is private to one side.

**Where the taxonomy is enforced:** by convention now, not by code structure. With the obligation framework in place, the Go compiler enforced that the only way to emit a diagnostic was through `Generator.Generate` → `Obligation.Discharge`. With Shen-as-spec, the equivalent guarantee would have to come from a Shen-side type signature (every `check-rN` returns `(list judgment)`) and the `obligationRefForCode` table. Worth tracking explicitly if/when an ADR-002 lands.

**Determinism:** `worldToShenObj` sorts every input slice before serialization ([bridge.go:304-341](pkg/checker/bridge.go)). `audit.Generate` Merkle-trees the diagnostics with a stable order. Same input → same proof root.

---

## Related Research

- [`thoughts/shared/research/2026-04-17-full-codebase-review.md`](2026-04-17-full-codebase-review.md) — post-pivot snapshot of the full codebase with ADR-002 and the trajectory simulator in place.

---

## Open Questions

1. **Is there an ADR-002 planned to document the Shen-as-spec decision?** ADR-001 explicitly says "the only way to emit a diagnostic is through the obligation framework in `pkg/obligation/`." That sentence is now false on this branch. Either ADR-001 needs amending, or an ADR-002 needs to supersede §3 of ADR-001.
2. **What is the path for Categories A, D, E, H, I, J?** The taxonomy says 12 categories; the kernel currently implements 11 rules absorbed into 7 categories (B, C, F, G, J, K, L). The Argo IR extensions needed for D/E/H exist but are not surfaced into the Shen world. No generator emits A or I yet.
3. **Should `pkg/checker/rules.go` (the original Go rule functions) be deleted on this branch?** Per the prior research doc, those were already orphaned in PR #3. This research did not re-confirm their state on the working tree of `claude/phase1-cleanup`.
4. **Should `audit.Generate` consume `RunResult.ObligationIDs`?** The prior research flagged that the audit Merkle tree only knows about `XPC001`..`XPC011`. With Shen-as-spec preserving the same legacy codes, this is unchanged — but it remains an architectural debt for any future generator that emits a non-legacy code.
5. **What's the test story for the Shen kernel?** PR #3 added `pkg/obligation/refs/composition_xrd_test.go` as a pilot. Those tests are deleted with the rest of `pkg/obligation/`. The current branch has `pkg/checker/check_test.go` (not read in this session) — that file's coverage scope is the natural next thing to verify.
6. **Stable name for the project codebase:** The Go module is `github.com/pyrex41/cross-validate-` (with a trailing hyphen — visible in `go.mod`). The repo is `pyrex41/cross-validate`. Worth deciding whether this is intentional or a leftover.

---

## Follow-up Research 2026-04-17 (later same day)

**Question (paraphrased):** "What's actually in the Shen kernel? We don't want a bunch of ad-hoc rules — we want clear invariants constraining how Argo + Crossplane + the infra interact in real time/space."

This is a sharper cut than the A–L taxonomy makes. A rule can sit in a "real" category (F = trajectory) and still be hollow; another can sit in a "structural" category (B = reference resolution) and still be useful. So I read every `kernel/r*.shen` file and classified each rule by whether it is (a) a genuine invariant on the *joint dynamics* of Argo × Crossplane × infra, or (b) a static structural check that any decent linter or `kubectl apply --dry-run=server` could do.

### Rule-by-rule audit (kernel as it stands today)

| Rule | Code | What it actually checks | Joint-dynamics or static? | Implementation honesty |
|------|------|--------------------------|---------------------------|--------------------------|
| R1   | XPC001 | Every CRD/XRD version is `served`; CRDs have exactly one `storage`; XRDs have ≥1 `referenceable`. | **Static, single-resource.** No Argo, no Crossplane controller, no time. | Direct; could be a CEL admission policy. |
| R2   | XPC002 | If you write a resource at a non-storage version of a `Webhook`-conversion CRD, every read/write hits the conversion webhook → flag unless `xpc.dev/accept-conversion-webhook=true`. | **Semi-dynamic.** Real claim about runtime cost (per-request webhook hop). | Direct check on (resource version, CRD storage version, conversion strategy). |
| R3   | XPC003 | Composition `compositeTypeRef` resolves to an existing XRD, and the version is `referenceable`. | **Static reference resolution.** | Direct. |
| R4   | XPC004 | Pipeline step `functionRef` resolves to a Function; input apiVersion is in the function's accepted set. | **Static reference resolution.** | Direct. The accepted-version set comes from a hardcoded map of 9 well-known Crossplane functions in `pkg/ir/builder.go` — not from inspecting the Function's actual schema. |
| R5   | XPC005 | Patch `fromFieldPath` type is assignable to `toFieldPath` type after `convert` transforms. | **Static structural typing.** | Real type-walking, but the type discovery happens *in Go* (`resolvePatchTypes` in `bridge.go`) and the kernel only sees the pre-resolved `(resolved-patch …)` facts with `FromType`/`ToType` strings. |
| **R6** | XPC006 | Across an Argo Application's sync waves: `wave(XRD) < wave(XR)`, `wave(Function) < wave(Composition)`, `wave(Composition) ≤ wave(XR)`. | **YES — genuine trajectory invariant.** This is the only rule that models a real temporal interaction between Argo (wave executor) and Crossplane (controller readiness for XRDs/Functions). | Direct, but only on the *declared* sync-wave annotations, not on actual readiness gates. R6c (Provider wave < MR wave) is documented in the file comment but **not implemented** — only R6a/R6b/R6d exist. |
| **R7** | XPC007 | If Argo Application uses `tracking-method: label` AND any Composition is present, warn — Crossplane label-propagation will fight Argo's label-tracking forever (crossplane#2121). | **YES — genuine cross-system interaction claim.** Standing conflict, not strictly temporal but real. | Direct. Always emits when both conditions hold; no nuance. |
| R8   | XPC008 | Resource targeting v2 XRD uses v1-style top-level machinery (e.g. `publishConnectionDetailsTo` outside `spec.crossplane`). | **Static.** | The Shen rule is essentially **a passthrough**: the actual structural detection happens in Go (the IR builder is supposed to set `xpc.dev/v1-machinery-on-v2-xrd: true` on the resource), and Shen just reports that annotation. The `check-r8-against-xrd` predicate in this file always returns `[]` (line 30). |
| R9   | XPC009 | Composition pipeline step requires a resource that won't exist on first reconcile — bootstrap gap. | **Aspirational dynamic.** Wants to be a temporal invariant on Crossplane's first-reconcile state. | **Hollow.** `check-r9-step` always returns `[]` (line 22). Real check is supposed to come from Go-side annotations (`xpc.dev/required-resource-missing`) but I did not verify any Go code sets this annotation. |
| R10  | XPC010 | For each patch, if `FromPath` looks secret-y AND `ToPath` is not a secret sink, flag a leak. | **Static substring lookup.** Categorized as "K: Secret-flow / information-flow typing with a taint lattice" in the docs. | **Substantially weaker than the docs claim.** Implementation is a hardcoded list of ~20 path strings (`spec.forProvider.password`, `spec.forProvider.apiKey`, …) plus a substring match for `password`/`secret`/`credential`/`token`/`apikey` in the lower-cased path. There is no taint propagation across patches, no multi-hop tracking, no lattice. It's a single-edge filename check. |
| R11  | XPC011 | Resource using a deprecated apiVersion; Composition referencing one; provider pinned below a hardcoded version floor; CRD version no longer served. | **Static lookup tables.** Documented as "L: Deprecation/calendar — `(API × k8s version × date)`." | **No dates, no k8s version awareness.** It's a hardcoded list of 5 deprecated apiVersions and 2 provider package floors (`provider-aws < v0.40.0`, `provider-family-aws < v1.0.0`). The "calendar" framing is aspirational. |

### What's NOT in the kernel that the taxonomy promises

`docs/obligations.md` lists 12 categories with multiple generators each. Many generators named in the doc have no Shen rule and never had a Go generator either:

| Category | Promised generators in `docs/obligations.md` | Actually implemented? |
|----------|----------------------------------------------|------------------------|
| **A — Schema** | `patch-source-type`, `patch-target-type`, `crossplane-machinery-placement` | Partially (R5 absorbs the patch-type pieces; R8 absorbs machinery-placement but is mostly a Go-side annotation passthrough). No general "for every CRD field, verify required/types/enums/formats/immutability" — that's `kubectl --dry-run=server`'s job and it's not done in xpc. |
| **B — Reference resolution** | `comp-xrd-ref`, `pipeline-fn-ref`, `patch-compat` | Yes — R3, R4, R5. This is the most populated category. |
| **C — Version coherence** | `version-coherence`, `crossplane-machinery-version` | R1 yes; the v2-XRD-version constraint piece exists in Go but the Shen side is the same passthrough as R8. |
| **D — AppProject constraints** | `source-repo-allowed`, `destination-allowed`, `kind-whitelisted`, `sync-window-permitted` | **None.** Argo IR was extended in PR #3 to *load* `AppProject`s, but no rule reads them. |
| **E — Sync-option interactions** | `replace-immutable-safety`, `ssa-field-manager-conflict`, `prune-target-exists`, `createnamespace-not-colliding` | **None.** |
| **F — Trajectory invariants** | `no-dangling-mount`, `no-immutable-change`, `no-rbac-regression`, `trajectory-wave-order`, `trajectory-bootstrap` | Only `trajectory-wave-order` (R6) is real. `trajectory-bootstrap` (R9) is a hollow passthrough. The other three named — `no-dangling-mount`, `no-immutable-change`, `no-rbac-regression` — are exactly the "real trajectory invariants over a simulated sync" that the user is asking about, and **none of them are implemented**. |
| **G — Cross-application** | `no-duplicate-ownership`, `cross-app-label-tracking`, `no-namespace-overlap` | Only `cross-app-label-tracking` (R7). The other two (which would actually require simulating multi-app rendering) **are not implemented**. |
| **H — Rendering** | `helm-renders`, `kustomize-renders`, `values-well-typed`, `render-deterministic` | **None.** xpc never invokes a renderer. |
| **I — Provider capability** | `field-available-in-version`, `field-not-deprecated`, `controller-healthy` | **None.** No generator inspects provider CRDs against MR field usage. |
| **J — Conversion cost** | `conversion-cost-opt-in` | Yes — R2. |
| **K — Secret-flow** | `secret-source-sink` (with "taint lattice" framing) | R10 exists but the taint-lattice framing is overstated; it's a substring lookup, not an IFC analysis. |
| **L — Deprecation calendar** | `api-deprecation-calendar` | R11 exists but is a static blocklist, not a date-aware calendar. |

### Honest summary against the user's framing

Of 11 rules in the kernel:

- **~1.5 rules are genuine real-time/space interaction invariants** on the joint Argo × Crossplane state-space: **R6 (sync-wave ordering)** unambiguously, **R7 (label-tracking conflict)** as a standing-conflict claim. R2 (conversion cost) is a semi-dynamic runtime claim about per-request webhook hops.
- **~6 rules are static structural checks** that don't really need cross-system awareness: R1 (version coherence), R3 (composition→XRD), R4 (pipeline→function), R5 (patch types), R8 (v1/v2 machinery, mostly Go-side), R11 (deprecation lookups).
- **~2 rules are hollow or under-implemented:** R9 (bootstrap — kernel side returns `[]`); R10 (advertised as taint-lattice IFC but is a substring blocklist).
- **6 of the 12 promised categories have zero rules in the kernel** (A in any general form, D, E, H, I; plus most of F's interesting invariants are missing).

The named rules in **F** that *would* meet the user's framing — `no-dangling-mount` (a Pod won't reference a deleted ConfigMap mid-sync), `no-immutable-change` (no update to an immutable field across the trajectory), `no-rbac-regression` (a SA's permissions hold at every step) — are exactly the kind of invariants the user is asking about. They are **named in `docs/obligations.md` and ADR-001 but have no generator and no Shen rule.** The taxonomy was designed with these in mind; the implementation never reached them.

This means the back-and-forth that the original question complained about has not actually been about the *invariants* — it has been about the *plumbing* that generates them. The taxonomy promises a lot of dynamic cross-system invariants. The Shen kernel today still mostly implements the static structural checks that the original 11 hand-written rules covered. The pivots between Go-rules / obligation-framework / Shen-as-spec moved the rules from one substrate to another but **did not add a single new dynamic invariant** to the implemented set.

### Code references for the audit

- [`kernel/r1-versions.shen`](kernel/r1-versions.shen) — R1 (CRD/XRD version coherence; static)
- [`kernel/r2-conversion.shen`](kernel/r2-conversion.shen) — R2 (webhook conversion cost; semi-dynamic)
- [`kernel/r3-composition-resolves.shen`](kernel/r3-composition-resolves.shen) — R3 (Composition → XRD reference; static)
- [`kernel/r4-pipeline-functions.shen`](kernel/r4-pipeline-functions.shen) — R4 (Pipeline step → Function; static)
- [`kernel/r5-patch-typecheck.shen`](kernel/r5-patch-typecheck.shen) — R5 (patch type assignability; static, types pre-resolved in Go)
- [`kernel/r6-wave-ordering.shen`](kernel/r6-wave-ordering.shen) — **R6 (Argo sync-wave ordering; THE genuine trajectory invariant)**. R6c (Provider < MR) named in comment but not implemented.
- [`kernel/r7-owner-refs.shen`](kernel/r7-owner-refs.shen) — R7 (Argo label-tracking × Crossplane label-propagation conflict; standing claim)
- [`kernel/r8-v1v2-machinery.shen`](kernel/r8-v1v2-machinery.shen) — R8 (v1/v2 machinery; **mostly delegated to Go-side annotation**; `check-r8-against-xrd` returns `[]`)
- [`kernel/r9-bootstrap.shen`](kernel/r9-bootstrap.shen) — R9 (bootstrap gap; **`check-r9-step` returns `[]`**, depends on `xpc.dev/required-resource-missing` annotation set elsewhere)
- [`kernel/r10-secret-taint.shen`](kernel/r10-secret-taint.shen) — R10 (secret leak; **substring lookup, not IFC**; ~20 hardcoded paths + ~10 substrings)
- [`kernel/r11-api-deprecation.shen`](kernel/r11-api-deprecation.shen) — R11 (deprecation; **5 hardcoded API strings + 2 provider version floors**, no dates)
- [`docs/obligations.md`](docs/obligations.md) — taxonomy A–L with promised generators; many are unimplemented
- [`docs/adr/001-bounded-obligation-taxonomy.md`](docs/adr/001-bounded-obligation-taxonomy.md) — names the invariants the user is asking about (e.g. `no-dangling-mount`, `no-immutable-change`, `no-rbac-regression`) under category F

### Open questions raised by this follow-up

7. **Is the Shen kernel currently the right home for invariants like `no-dangling-mount`, `no-immutable-change`, `no-rbac-regression`?** These would require simulating an Argo sync trajectory step by step over a Composition's rendered output, with intermediate cluster states. That's a lot more state than the current `World` carries to Shen.
8. **What is the threshold for promoting an annotation-passthrough rule (R8, R9) into either (a) a real Shen rule that examines IR fields, or (b) honestly demoting it to "this check is owned by the Go IR builder"?** Today they straddle and neither side fully owns them.
9. **Is the goal for the kernel to add the missing trajectory invariants (F + G + I), or is the current kernel content close to "done" for static checks and the next move is something else?** The PR/branch history shows three implementations of the same 11 rules, not new invariants — knowing which way you're pointing matters before another pivot.
