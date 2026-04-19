---
date: 2026-04-18T00:00:00Z
researcher: Reuben Brooks
git_commit: 0ecd1db3a8a14db3e91314f0f96393248d185784
branch: claude/phase1-cleanup
repository: pyrex41/cross-validate
topic: "So what have we actually built?"
tags: [research, codebase, tour, xpc, shen, trajectory, obligations]
status: complete
last_updated: 2026-04-18
last_updated_by: Reuben Brooks
---

# Research: So what have we actually built?

**Date**: 2026-04-18
**Researcher**: Reuben Brooks
**Git Commit**: `0ecd1db` (`claude/phase1-cleanup`)
**Repository**: `pyrex41/cross-validate`

## Research Question

A tour of the current state of the codebase: what is `xpc`, what does it do today, how do the pieces fit together?

## Summary

`xpc` is a command-line type-checker for a Crossplane + Argo CD stack. It ingests Kubernetes/Crossplane/Argo YAML, builds a typed IR called a **World**, simulates an Argo sync trajectory wave-by-wave, hands the result to an **in-process Shen interpreter** that runs 14 hand-written rules (R1–R14 → error codes XPC001–XPC014), and emits diagnostics in one of six formats (agent, human, JSON, SARIF, JUnit, LSP).

The build is organized around two ADRs:

- **ADR-001**: All checks fit a bounded 12-category obligation taxonomy (A–L). New checks must fit an existing category or require a new ADR.
- **ADR-002**: Shen is the canonical rule spec. Go owns parsing, the IR, the trajectory simulator, and the Shen bridge. The Go-side generator registry from ADR-001 §3 was removed; the Shen kernel files under `kernel/*.shen` are the executable rulebook.

Alongside the check engine, the tool ships a **signed, content-addressed audit proof** system (`.xpcproof`, Merkle-hashed, version 3) and a **snapshot** format (`.xpcsnap`, SHA-256 digest, 15-minute TTL) for capturing cluster type-environments. Seven fixture directories drive integration tests that exercise every implemented rule.

State-of-the-art: R1–R12 + R14 are fully implemented. **R13** (`no-immutable-change`) is framework-only — the simulator consumes a single snapshot per resource, so `Delta.Updated` is always `nil` and the rule currently has nothing to fire on. **`xpc bisect`** is a stubbed command that prints what it would do but does not run git bisect yet.

## Detailed Findings

### 1. CLI surface — `cmd/xpc/main.go`

The `main()` dispatcher at [`cmd/xpc/main.go:30-55`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/cmd/xpc/main.go#L30-L55) routes to eight subcommands. Flag parsing is hand-rolled per subcommand (no third-party flag library).

| Subcommand | Purpose | Key flags |
|---|---|---|
| `check <path>...` | Run all rules over one or more manifest paths | `--format`, `--strict-conversions`, `--proof`, `--snapshot=<path>` |
| `dump-ir <path>` | Print the IR World as s-expression | — |
| `snapshot [<path>]` | Capture or diff cluster type environment | `--output`, `--cluster`, `--diff=a,b` |
| `verify <proof>` | Re-hash a `.xpcproof` and check root digest | — |
| `proof show <proof>` | Print proof summary or per-rule judgments | `--rule=<id>` |
| `proof diff <a> <b>` | Diff two proofs | — |
| `bisect` | **Stub** — prints plan, no execution | `--rule`, `--good`, `--bad` |
| `explain <code>` | Print explanation text for XPC001–XPC011 | — |
| `version` | Print `xpc 0.1.0` | — |

The `check` subcommand exits non-zero on any error-severity diagnostic ([`main.go:228-233`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/cmd/xpc/main.go#L228-L233)). When `--proof` is set, it writes `check.xpcproof` in the cwd and prints the root digest to stderr.

**Report formats** — `pkg/report/reporter.go` declares six format constants at [`reporter.go:19-26`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/report/reporter.go#L19-L26):

- `agent` (default) — labeled blocks (rule/severity/problem/source/fix/ack/docs) with a trailing `xpc: N error(s), N warning(s)` footer
- `human` — traditional `file:line:col: severity: code: message` with source excerpt + caret underline
- `json` — `[]types.Diagnostic` pretty-printed
- `sarif` — SARIF 2.1.0 with `tool.driver.rules[]` and `results[].locations`
- `junit` — `<testsuites>`/`<testsuite>`/`<testcase>` XML
- `lsp` — file-keyed map of LSP diagnostics with 0-based ranges

### 2. Input pipeline — `pkg/loader` → `pkg/ir` → `pkg/types`

The pipeline turns YAML files into a `*types.World` ready for the checker.

**Types ([`pkg/types/types.go`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/types/types.go))** — the entire IR lives in one file:

| Type | Role |
|---|---|
| `World` (line 630) | Root container of all typed slices + `Schemas` map |
| `CRDInfo` / `CRDVersion` / `ConversionInfo` | CRDs and XRDs with per-version served/storage flags; `CostClass` ∈ {None, Identity, Structural, Webhook} |
| `CompositionInfo` / `PipelineStep` / `ComposedResource` | Crossplane Compositions in Pipeline or Resources mode |
| `ResourceInfo` | Any unrecognized manifest; carries full `Raw` map for enrichment |
| `ArgoApplication` / `ArgoAppProject` / `ArgoApplicationSet` | Argo CD surface |
| `MountRef` / `SARef` / `RBACBinding` / `RBACRule` | Derived cross-references populated by `EnrichTrajectoryData` |
| `ImmutableField` | Static registry (8 entries) for fields immutable post-create |
| `SchemaInfo` | Content-addressed OpenAPI schema fragment, `sha256:<16-hex>` |
| `SourceLocation` | File/line/column back-pointer for diagnostics |

**Loader ([`pkg/loader/loader.go`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/loader/loader.go))** — `LoadDirectory` walks `.yaml`/`.yml`, `LoadReader` streams each document through `yaml.Decoder`, skipping docs without `apiVersion`/`kind`. `ClassifyDocument` at [`loader.go:119`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/loader/loader.go#L119) dispatches on Kind + apiVersion prefix into 10 categories (crd, xrd, composition, function, provider, configuration, argo-application, argo-appproject, argo-applicationset, resource).

**Builder ([`pkg/ir/builder.go`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/ir/builder.go))** — per-category `add*` methods populate the World. Notable:

- `addCRD` computes `CostClass` by comparing schema digests across versions
- `addFunction` runs `inferFunctionInputVersions` against a hardcoded table of well-known Crossplane function packages
- `hashSchema` is the one-way content-address for every CRD/XRD/pipeline schema
- `ParseSyncWave` reads the `argocd.argoproj.io/sync-wave` annotation

After the loop, `EnrichTrajectoryData` ([`pkg/ir/trajectory_extract.go:12`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/ir/trajectory_extract.go#L12)) runs a second pass over `World.Resources` to dispatch on Kind — Pod, Deployment/StatefulSet/DaemonSet/ReplicaSet/Job, CronJob, RoleBinding/ClusterRoleBinding, Role/ClusterRole — and emit `MountRef`, `SARef`, `RBACBinding`, `RBACRule` entries. The `ImmutableFieldRegistry()` static table is appended to `World.ImmutableFields` in the same pass.

**Schemas ([`pkg/schemas/fetcher.go`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/schemas/fetcher.go))** — a two-tier (in-memory + `~/.cache/xpc/schemas/`) content-addressed store. `ResolveFieldType` walks a dotted path through a JSONSchema tree; `TypeAssignable` treats `integer→number` and anything involving `unknown` as compatible.

### 3. Go → Shen bridge — `pkg/checker/bridge.go`

The embedded Shen-go interpreter runs in-process; no subprocess. The bridge is protected by a `sync.Once` and initialized on first `CheckWithObligations` call.

**Initialization ([`bridge.go:40-108`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/checker/bridge.go#L40-L108))**:

1. `shenfull.Init(&shenCF)` bootstraps the KL runtime by calling 14 pre-compiled loader functions (`TopLevelMain`, `CoreMain`, `SysMain`, … `TypesMain`) from [`internal/shenfull/`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/internal/shenfull/init.go#L25-L50).
2. `resolveKernelPath` walks upward from cwd to find `kernel/check.shen`.
3. The bridge `chdir`s into the kernel directory, redirects `*stoutput*` to `/dev/null` (silences banner), runs `(load "check.shen")`, then restores cwd.

**Pre-serialization enrichment**:

- `enrichSyncWaves` ([`bridge.go:179`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/checker/bridge.go#L179)) adds `SyncWaveEntry` records to every ArgoApp for tracked resources.
- `resolvePatchTypes` ([`bridge.go:242`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/checker/bridge.go#L242)) walks composition patches and resolves from/to types against stored schema digests.
- `trajectory.Simulate` runs the wave-by-wave simulator.

**World → s-expression** ([`worldToShenObj`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/checker/bridge.go#L341) at `bridge.go:341-412`) builds one nested `kl.Obj` list with 16 tagged sections:

```
(world
  (crds …) (xrds …) (compositions …) (functions …)
  (providers …) (configurations …) (resources …) (argo-apps …)
  (schemas …) (resolved-patches …)
  (mount-refs …) (sa-refs …) (rbac-bindings …) (rbac-rules …)
  (immutable-fields …) (trajectory …))
```

Each section is built via the `sortedSection[T any]` generic helper ([`bridge.go:331`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/checker/bridge.go#L331)) which clones, sorts with `slices.SortFunc`, and maps each element through a per-type `*ToObj` function.

**Call and decode** ([`bridge.go:159-170`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/checker/bridge.go#L159-L170)):

```go
checkWorld := kl.PrimFunc(kl.MakeSymbol("check-world"))
result := kl.Call(&shenCF, checkWorld, worldObj)
```

`objToDiagnostics` ([`bridge.go:740`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/checker/bridge.go#L740)) decodes an 8-tuple `(judgment Code Sev Src Msg Detail Fix Related)`. The internal sentinel severity `"satisfied"` means "rule ran, no violations" — these populate `RunResult.Satisfied` but are stripped from `Diagnostics`.

**RunResult** ([`pkg/checker/result.go:6-21`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/checker/result.go)) has five fields: `Diagnostics`, `TotalObligations`, `Satisfied`, `Violated`, `ObligationIDs`. (The `Unknown` field was removed in commit `6a82ff5`.)

### 4. Kernel rulebook — `kernel/*.shen`

14 rule files, one orchestrator, one prelude.

**`check.shen`** — orchestrator. Destructures the World s-expression, dispatches all 14 rules via `run-checker`, concatenates their judgments, writes to stdout.

**`prelude.shen`** — library of shared types (`judgment`, `source-loc`, `severity`) and helpers (`member`, `filter`, `find-first`, `count-if`, `string-contains?`, `delta-created/updated/deleted-keys`, `state-keys`, `mark-rule`, `make-error`, `make-warning`, `make-satisfied`).

| File | Rule | Emits | Consumes | Status |
|---|---|---|---|---|
| `r1-versions.shen` | CRD/XRD served-storage coherence | XPC001 | `crds`, `xrds` | ✅ |
| `r2-conversion.shen` | Webhook conversion requires annotation ack | XPC002 | `resources`, `crds` | ✅ |
| `r3-composition-resolves.shen` | Composition → XRD reference resolves | XPC003 | `compositions`, `xrds` | ✅ |
| `r4-pipeline-functions.shen` | Pipeline step Function reference + input version | XPC004 | `compositions`, `functions` | ✅ |
| `r5-patch-typecheck.shen` | Patch source → target type assignability | XPC005 | `resolved-patches` | ✅ |
| `r6-wave-ordering.shen` | R6a (XRD<XR), R6b (Fn<Comp), R6d (Comp≤XR) | XPC006 | `argo-apps`, `compositions`, `xrds`, `functions` | ✅ |
| `r6c-provider-wave.shen` | R6c: Provider wave < MR wave | XPC006 | `argo-apps`, `crds` | ✅ |
| `r7-owner-refs.shen` | Warn on Argo label-tracking + Compositions | XPC007 (warn) | `argo-apps`, `compositions` | ✅ |
| `r8-v1v2-machinery.shen` | v1/v2 XRD machinery placement | XPC008 | `resources`, `xrds` | Framework-only (logic in Go) |
| `r9-bootstrap.shen` | Required resources exist at step 0 | XPC009 | `compositions`, `resources` | Framework-only |
| `r10-secret-taint.shen` | Secret-tainted field → non-secret-sink | XPC010 | `resolved-patches` | ✅ |
| `r11-api-deprecation.shen` | Deprecated apiVersions, floor provider versions | XPC011 (warn) | `resources`, `compositions`, `providers`, `crds` | ✅ |
| `r12-no-dangling-mount.shen` | Deleted ConfigMap/Secret with live non-optional mount | XPC012 | `trajectory`, `mount-refs` | ✅ |
| `r13-no-immutable-change.shen` | Immutable field changed during Update | XPC013 | `trajectory`, `immutable-fields` | **Dormant** (Delta.Updated always nil) |
| `r14-no-rbac-regression.shen` | Workload loses RBAC mid-sync | XPC014 | `trajectory`, `sa-refs`, `rbac-bindings` | ✅ |

### 5. Trajectory simulator — `pkg/trajectory/`

A Go-only, stateful, wave-by-wave simulation of the cluster contents during an Argo sync. Emits per-step facts that the Shen kernel treats as enumerated tuples.

**Data model** ([`trajectory.go`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/trajectory/trajectory.go)):

- `ResourceKey { APIVersion, Kind, Namespace, Name }` — canonical handle
- `Delta { Created, Updated, Deleted []ResourceKey }` — `Updated` is always `nil` (single-snapshot limitation)
- `State { Resources map[ResourceKey]struct{} }` — keys-only set (the full `ResourceInfo` lives in the world's `resources` section)
- `Step { AppName, Wave int, Delta, State }`

**`Simulate`** ([`simulate.go:27`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/trajectory/simulate.go#L27)) algorithm:

1. Sort ArgoApps by name for determinism.
2. Per app, scope resources by `Destination.Namespace` (or all if unset).
3. Bucket each resource by its `argocd.argoproj.io/sync-wave` annotation; additionally, if `hook-delete-policy` ∈ {HookSucceeded, HookFailed}, enqueue it for deletion at the end of its own wave.
4. For each wave in ascending order: apply creates, then hook-deletes; emit a `Step` with sorted `Delta` key slices and a `maps.Clone`d `State` snapshot.

The simulator is serialized into the `(trajectory …)` section by `trajectoryToObj`/`stepToObj`/`deltaToObj`/`resourceKeyToObj` in `bridge.go:660-704`.

**Known scope limits** (documented in the code):

- No cluster/project/label-selector scoping; namespace pin or everything
- `Delta.Updated` empty until multi-snapshot extension
- `State` carries only keys

### 6. Audit / proof system — `pkg/audit/proof.go`

A `.xpcproof` file is a `json.MarshalIndent`-serialized `Proof` struct at version **3** (bumped from v2 in commit `6a82ff5` when `RunSummary.Unknown` was dropped).

**Structure** ([`proof.go:279-285`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/audit/proof.go#L279-L285)):

- `version: 3`
- `rootDigest` — SHA-256 Merkle root (hex)
- `metadata` — `irDigest`, `snapshotDigest`, `kernelVersion: "0.1.0"`, `rulesetVersion: "2026.04"`, `rulesetDigest`, `timestamp`, optional `signingIdentity`, `signature`, `repo`, `commit`, `cluster`
- `run` — `RunSummary { totalObligations, satisfied, violated, obligationIds }`
- `ruleSubtrees` — map `ruleID → { digest, judgments[] }`
- `resourceSubtrees` — map `"file:line" → { digest, judgments[] }`
- `tree` — flat list of leaf hashes

**Hashing**:

- `hashJudgment` concatenates `status|ruleId|resource|message|obligationId|category|generator|` and SHA-256s
- `hashRunSummary` hashes `totalObligations|satisfied|violated|` + sorted obligation IDs (format string is `"%d|%d|%d|"` post-cleanup)
- Subtree digests hash `ruleID` + judgment-digest bytes in order
- `buildMerkleTree` ([`proof.go:211`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/audit/proof.go#L211)) assembles leaves in fixed order: metadata-JSON hash, run-summary hash, rule subtree digests (sorted by ID), resource subtree digests (sorted by key). `computeMerkleRoot` folds bottom-up; odd nodes hash with themselves.

**Verify** — `Verify()` recomputes the tree and compares against the stored `RootDigest`. `VerifyInclusion` checks a `(ruleID, resource)` pair is in the judgments list.

**`DiffProofs`** ([`proof.go:368`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/audit/proof.go#L368)) iterates the union of rule IDs and classifies each as `unchanged`, `newlySatisfied`, or `newlyViolated`. Also flags changes in `SnapshotDigest` between runs.

### 7. Snapshot system — `pkg/snapshot/snapshot.go`

A `.xpcsnap` file captures a cluster's type environment (CRDs, XRDs, Providers with `Healthy` + `Version`, Functions, Configurations, Compositions, Schemas, ArgoTrackingMode). Version field is `1`.

**`ComputeDigest`** ([`snapshot.go:94-144`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/snapshot/snapshot.go#L94-L144)) hashes content fields only (excluding `Digest`, `Signature`, `Timestamp`) in fixed order: version string, cluster name, k8s version, sorted CRDs by `group/kind`, sorted XRDs, sorted Providers/Functions/Compositions by name, ArgoTrackingMode. Schemas are **not** in the digest.

**TTL** — `IsStale(ttl)` checks `time.Since(timestamp) > ttl`; `DefaultTTL` = 15 minutes ([`snapshot.go:339-344`](https://github.com/pyrex41/cross-validate/blob/0ecd1db/pkg/snapshot/snapshot.go#L339-L344)). The `check` subcommand prints a warning if a loaded snapshot is stale.

**`Diff`** emits `+`/`-`/`~` lines for CRDs (by group/kind, diffing storage version + conversion cost-class), Providers (by name, diffing Package + Healthy), Functions (by name, diffing Package). Configurations and Compositions are not diffed.

**`FromWorld` / `ToWorld`** — bidirectional conversion so snapshots can be produced offline from YAML and later fed back into `check --snapshot`.

### 8. Test fixtures — `testdata/fixtures/`

Seven directories, each tied to a specific rule in `pkg/checker/check_test.go`:

| Fixture | Files | Expected diagnostic |
|---|---|---|
| `basic/` | `xrd.yaml`, `composition.yaml`, `function.yaml` | **None** (happy path; exercises R1/R3/R4/R7) |
| `webhook-conversion/` | `crd.yaml`, `bucket.yaml` | XPC002 |
| `patch-mismatch/` | `composition.yaml` | XPC005 |
| `provider-wave/` | `app.yaml` | XPC006 (R6c) |
| `wave-ordering/` | `app.yaml` | XPC006 (R6a) |
| `dangling-mount/` | `app.yaml` | XPC012 |
| `rbac-regression/` | `app.yaml` | XPC014 |

No fixtures for R8 (framework-only), R9 (framework-only), R13 (dormant).

## Code References

- `cmd/xpc/main.go:30-55` — main dispatcher
- `cmd/xpc/main.go:107-234` — `check` subcommand
- `pkg/report/reporter.go:19-26` — format constants
- `pkg/types/types.go:630` — `World` root type
- `pkg/loader/loader.go:119` — `ClassifyDocument` kind dispatcher
- `pkg/ir/builder.go:30` — `Builder.Build` entry point
- `pkg/ir/trajectory_extract.go:12` — `EnrichTrajectoryData`
- `pkg/ir/immutable_registry.go:7` — static immutable-field registry
- `pkg/checker/bridge.go:145` — `CheckWithObligations` entry point
- `pkg/checker/bridge.go:331` — `sortedSection[T]` generic helper
- `pkg/checker/bridge.go:341` — `worldToShenObj` — World → s-expr
- `pkg/checker/bridge.go:740` — `objToDiagnostics` — Shen → `[]Diagnostic`
- `pkg/checker/result.go:6-21` — `RunResult`
- `pkg/trajectory/simulate.go:27` — `Simulate`
- `pkg/audit/proof.go:211` — `buildMerkleTree`
- `pkg/audit/proof.go:368` — `DiffProofs`
- `pkg/snapshot/snapshot.go:94-144` — `ComputeDigest`
- `kernel/check.shen` — orchestrator
- `kernel/prelude.shen` — shared library
- `kernel/r1-versions.shen` … `kernel/r14-no-rbac-regression.shen` — 14 rule files

## Architecture Documentation

**Substrate split (ADR-002)**:

- Go owns: YAML parsing, typed IR, enrichment passes, trajectory simulator, Shen bridge, audit/proof + snapshot serialization, CLI.
- Shen owns: every rule that emits a diagnostic.
- The formal interface is the s-expression shape produced by `worldToShenObj`.

**Obligation taxonomy (ADR-001)**: 12 categories A–L; every generator belongs to one. Legacy XPC001–XPC014 codes remain as aliases for the rules that existed before the taxonomy was formalized; new obligations get structured codes `XPC.<Category>.<Generator>[.<Instance>]`.

**Determinism contract**: every serialization path sorts before hashing or emitting. `sortedSection` in the bridge, explicit `slices.SortFunc` in `snapshot.ComputeDigest`, `buildMerkleTree`'s fixed leaf order, and `Simulate`'s sorted key slices.

**Embedded Shen**: the `shen-go` interpreter runs in-process (`github.com/tiancaiamao/shen-go`). No subprocess, no IPC. Bootstrap is a 14-function sequence in `internal/shenfull/init.go`. Kernel source files are loaded via `(load "check.shen")` at first-check time, guarded by a `sync.Once`.

**Two-tier schema cache** (`pkg/schemas`): content-addressed by SHA-256 of canonicalized schema JSON; in-memory + `~/.cache/xpc/schemas/` disk tier. Cache is separate from the in-World `Schemas` map; it exists to persist across runs.

## Historical Context (from thoughts/)

- `thoughts/shared/research/2026-04-17-full-codebase-review.md` — full codebase review written the day before this tour, at the Phase 1 cleanup commit.
- `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md` — vision/architectural summary immediately following the Phase 1 cleanup.
- `thoughts/shared/research/2026-04-17-deferred-simplify-candidates.md` — catalog of four simplification candidates deferred from the `/simplify` pass; all four have since been landed in commits `91fc554`, `6a82ff5`, `80248ce`, `0ecd1db`.
- `docs/adr/001-bounded-obligation-taxonomy.md` — taxonomy ADR (§3 superseded by 002).
- `docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md` — Shen-as-spec ADR, dated 2026-04-17.
- `docs/obligations.md` — reference definition of the 12 categories, keyed to the generator contract.

## Related Research

- `thoughts/shared/research/2026-04-17-full-codebase-review.md` — more granular per-file walkthrough
- `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md` — the "why" context behind the current shape

## Open Questions / Deferred Work

These are explicit TODOs surfaced by the code and ADRs, not recommendations:

- **R13 (no-immutable-change)** — dormant until the simulator learns multi-snapshot diffing so `Delta.Updated` can be populated. Flagged in ADR-002 §Consequences and `docs/obligations.md:107`.
- **R8 (v1/v2 machinery)** and **R9 (bootstrap)** — kernel side is framework-only; primary logic lives in Go preprocessing. No outstanding bug, just noted.
- **`xpc bisect`** — command scaffold exists; implementation prints its plan and exits (`main.go:507-517`).
- **Helm/Kustomize rendering** — the simulator sees Applications' `Sources` field but does not execute renderers (`pkg/trajectory/trajectory.go` package comment). Category H (rendering obligations) is defined in the taxonomy but has no implemented generators yet.
- **Generators B–L beyond absorbed R1–R14** — the taxonomy lists generators for Category D (AppProject constraints), E (SyncOption interactions), G (cross-Application), I (Provider capability) that are currently not implemented. `docs/obligations.md` marks these as "new — no legacy rule."
- **`audit.Generate` XPC001–XPC011 allow-list** — flagged as debt in the deferred-simplify research doc; separate ticket.
- **`pkg/ir/sexpr.go ToSExpr`** — non-deterministic over `w.Schemas`; flagged as open concern for whether `dump-ir` is a stable surface.
