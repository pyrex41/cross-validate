---
date: 2026-04-17T22:09:27Z
researcher: Reuben Brooks
git_commit: 91b0317093bfd7cb558b00b26f80f72823685089
branch: claude/phase1-cleanup
repository: cross-validate
topic: "Full codebase review"
tags: [research, codebase, architecture, xpc, shen, trajectory, obligations, crossplane, argo-cd]
status: complete
last_updated: 2026-04-17
last_updated_by: Reuben Brooks
---

# Research: Full codebase review

**Date**: 2026-04-17T22:09:27Z
**Researcher**: Reuben Brooks
**Git Commit**: `91b0317093bfd7cb558b00b26f80f72823685089`
**Branch**: `claude/phase1-cleanup`
**Repository**: `cross-validate`

## Research Question

Review everything we have here — a full census of the `cross-validate` repository: its purpose, its packages, its Shen kernel, its documentation, its test fixtures, its tooling, and the work captured in `thoughts/`.

## Summary

`cross-validate` (binary name: `xpc`) is a Go-based cross-validator for Crossplane + Argo CD configurations. Go code (`pkg/*`, `cmd/xpc`) loads YAML manifests, builds a typed intermediate representation (IR, `types.World`), enriches it with cross-resource references, simulates an Argo CD sync trajectory, and then hands the whole thing to an embedded Shen (Lisp) kernel that owns every rule. The kernel returns structured judgments; Go maps them back to diagnostics, renders them in six output formats, and optionally emits a Merkle-tree proof for audit.

The repository is on the `claude/phase1-cleanup` branch, which completes a substrate pivot captured in two ADRs and several thoughts docs: rules used to live in Go (`pkg/checker/rules.go`), then in a Go obligation framework (`pkg/obligation/`, deleted on this branch), and now live exclusively in `kernel/*.shen`. ADR-002 (new on this branch) locks the division of labor: "Go simulates, Shen checks." A new `pkg/trajectory/` package was introduced to compute step-by-step sync state so that Shen rules can pattern-match on the result.

Major things present today:
- **CLI** (`cmd/xpc/main.go`, 738 lines) with 7 subcommands (`check`, `dump-ir`, `snapshot`, `verify`, `proof`, `bisect`, `explain`).
- **Typed IR + loader** (`pkg/types`, `pkg/loader`, `pkg/ir`) producing a `World` with CRDs, XRDs, Compositions, Functions, Providers, Configurations, Resources, Argo Applications/AppProjects/ApplicationSets, schemas, and enriched cross-resource refs (mounts, SAs, RBAC, immutable fields).
- **Trajectory simulator** (`pkg/trajectory`) producing a deterministic `[]Step` wave sequence.
- **Shen bridge** (`pkg/checker/bridge.go`, ~970 lines) responsible for enrichment, World→s-expression serialization, runtime bootstrap, and judgment→diagnostic decoding.
- **Shen kernel** (`kernel/`, 17 files, ~1540 lines) with prelude + `check.shen` entry point + 14 rule files implementing XPC001–XPC014 (plus R6c reusing XPC006).
- **Shen runtime embedding** (`internal/shenfull/`, 16 Go files, ~1.4MB generated code) bootstrapping `github.com/tiancaiamao/shen-go`.
- **Audit proofs** (`pkg/audit`) producing signed, content-addressed Merkle-tree attestations.
- **Reporting** (`pkg/report`) with human, agent, JSON, LSP, JUnit, and SARIF formats.
- **Snapshots** (`pkg/snapshot`) of cluster type environment for cross-environment validation.
- **Docs** (`docs/obligations.md` + ADR-001 + ADR-002).
- **Fixtures** (`testdata/fixtures/`, 7 directories covering XPC002/003/004/005/006/006c/012/014).
- **Deploy** (`deploy/presync-hook.yaml`, Argo PreSync Job + RBAC) and **workflow skills** (`skills/xpc-*.md`).

## Detailed Findings

### 1. CLI: `cmd/xpc/main.go`

Single-file CLI using manual argument parsing (no `flag` package). Subcommands dispatched in a switch on `os.Args[1]` at `cmd/xpc/main.go:30-55`.

| Subcommand | Handler | Purpose |
|---|---|---|
| `check` | `runCheck` (`cmd/xpc/main.go:107-235`) | Load manifests → build IR → optionally merge snapshot → `checker.CheckWithObligations` → `report.ReportStdout` → optional `.xpcproof` |
| `dump-ir` | `runDumpIR` (`cmd/xpc/main.go:237-270`) | Load + build IR → `ir.ToSExpr(world)` → stdout |
| `snapshot` | `runSnapshot` (`cmd/xpc/main.go:272-362`) | Capture cluster type env via `snapshot.FromWorld` or diff two snapshots via `snapshot.Diff` |
| `verify` | `runVerify` (`cmd/xpc/main.go:364-389`) | `audit.LoadProof` → `Verify()` Merkle root |
| `proof show`/`diff` | `runProofShow` / `runProofDiff` (`cmd/xpc/main.go:408-475`) | Human summary or structured diff of proof files |
| `bisect` | `runBisect` (`cmd/xpc/main.go:477-519`) | Stub implementation; prints placeholder |
| `explain` | `runExplain` (`cmd/xpc/main.go:521-537`) | Prints entry from the `errorExplanations` map |
| `version`, `help` | Inline | `0.1.0`, usage text |

Flags:
- `check`: `--format={agent,human,json,sarif}` (default `agent`), `--strict-conversions`, `--proof`, `--snapshot=<path>`.
- `snapshot`: `--output=<path>`, `--cluster=<name>`, `--diff=<a>,<b>`.
- `proof`: `--rule=<XPCxxx>` filter.
- `bisect`: `--rule=<code>`, `--good=<ref>`, `--bad=<ref>`.

`mergeSnapshotIntoWorld` (`cmd/xpc/main.go:542-595`) augments the local World with CRDs/XRDs/Functions/Providers/Schemas from a snapshot, deduping by GK or name.

`errorExplanations` (`cmd/xpc/main.go:597-737`) is a hardcoded map for XPC001–XPC011; the four new codes (XPC012–XPC014, plus R6c which reuses XPC006) are not in this map yet.

### 2. Go package layer

#### 2a. `pkg/types/types.go` (657 lines)

The shared domain model. Key exported types:

- `CostClass` + `Severity` enums (`types.go:4-21`).
- `SourceLocation`, `CRDVersion`, `ConversionInfo`, `CRDInfo` (with `IsXRD` flag, `StorageVersion()`, `ServesVersion()` at `types.go:46-76`).
- `CompositionInfo` (`types.go:78-86`), `PipelineStep` (`:95`), `ComposedResource` (`:104`), `PatchInfo`, `TransformInfo`.
- `FunctionInfo`, `ProviderInfo` (+ `Annotations` map, recently added at `types.go:139-145`), `ConfigurationInfo`.
- Argo types: `ArgoApplication` (`:166-196`), `ArgoSource` + renderer variants (`ArgoHelmSource`, `ArgoKustomizeSource`, `ArgoDirectorySource`, `ArgoPluginSource`), `ArgoSyncPolicy`/`ArgoSyncOptions`/`ArgoRetryPolicy`, `ArgoIgnoreDiff`, `ArgoHook`, `ArgoAppProject` (`:388-414`), `ArgoApplicationSet` (`:447-457`), `ArgoAppSetGenerator`, `SyncWaveEntry`.
- Enriched cross-resource refs (new on this branch at `types.go:547-603`): `MountRef`, `SARef`, `RBACBinding`, `RBACRule`, `ImmutableField`.
- `SchemaInfo` (content-addressed OpenAPI schema fragment).
- `World` (`types.go:605-631`): aggregate of all of the above plus `Schemas map[string]SchemaInfo` keyed by digest.
- `ObligationRef` (`types.go:633-642`) — ID/Category/Generator triple — and `Diagnostic` (`types.go:644-656`) with `Code`, `Severity`, `Source`, `Message`, `Detail`, `Fix`, `Related`, `Obligation`.

#### 2b. `pkg/loader/loader.go` (143 lines)

Parses `.yaml`/`.yml` files into `LoadedDocument{Source, APIVersion, Kind, Raw, RawNode}` (`loader.go:18-24`). `yaml.Node` is kept so source line/column can be recovered. Public API:

- `LoadDirectory(dir)` (`loader.go:28`) — recursive walk.
- `LoadFile(path)` (`:57`) — single file, multi-doc.
- `LoadReader(r, sourcePath)` (`:67`) — core parser; skips documents without `apiVersion`/`kind`.
- `LoadStdin()` (`:114`).
- `ClassifyDocument(doc)` (`:119-142`) — returns one of `"crd"`, `"xrd"`, `"composition"`, `"function"`, `"provider"`, `"configuration"`, `"argo-application"`, `"argo-appproject"`, `"argo-applicationset"`, `"resource"`.

#### 2c. `pkg/ir/`

Five files:
- `builder.go` (1025 lines). `Builder.Build(docs)` at `ir/builder.go:29-63` iterates docs, classifies, dispatches to `addCRD` / `addXRD` / `addComposition` / `addFunction` / `addProvider` / `addConfiguration` / `addArgoApplication` / `addArgoAppProject` / `addArgoApplicationSet` / `addResource`, then calls `EnrichTrajectoryData(b.world)` (line 61). Notable: `addProvider` was recently enhanced to extract metadata annotations (`builder.go:367-372`); `hashSchema` (`:976`) produces `sha256:<16-byte-truncated>` digests; `inferFunctionInputVersions` (`:991-1011`) maps well-known function names (e.g., `function-patch-and-transform`) to declared input API versions; `ParseSyncWave(annotations)` (`:1014-1024`) reads `argocd.argoproj.io/sync-wave`.
- `sexpr.go` (201 lines). `DigestWorld(w)` and `ToSExpr(w)`; deterministic serialization for content-addressing (`xpcir-version 1` header; sections for CRDs, XRDs, Schemas, Compositions, Functions, Providers, Configurations, Resources, Argo Apps).
- `trajectory_extract.go` (273 lines). `EnrichTrajectoryData(w)` walks `w.Resources`, dispatching on Kind. Pod/Deployment/StatefulSet/DaemonSet/ReplicaSet/Job → `extractFromPodSpec` (reads volumes for ConfigMap/Secret/projected mounts and envFrom, plus `serviceAccountName`). CronJob unwraps `jobTemplate.spec.template.spec`. RoleBinding/ClusterRoleBinding → `extractRBACBinding` (one binding per subject). Role/ClusterRole → `extractRBACRules`. Finally writes `w.ImmutableFields` from `ImmutableFieldRegistry()`.
- `trajectory_extract_test.go` (256 lines). Exercises empty world, Pod volumes, Deployment envFrom, CronJob nesting, projected volumes, RBAC bindings, RBAC rules.
- `immutable_registry.go` (27 lines). Static table: Service (`spec.clusterIP`, `spec.type`), PVC (`spec.storageClassName`, `spec.accessModes`), Job (`spec.selector`, `spec.template`), StatefulSet (`spec.serviceName`, `spec.volumeClaimTemplates`).

#### 2d. `pkg/trajectory/` (new package)

- `trajectory.go` (48 lines). Types: `Step{AppName, Wave, Delta, State}`, `Delta{Created, Updated, Deleted []ResourceKey}` with `Updated` always empty in current implementation (noted at `trajectory.go:24`), `State{Resources map[ResourceKey]ResourceInfo}`, `ResourceKey{APIVersion, Kind, Namespace, Name}`, `KeyOf(r)`.
- `simulate.go` (159 lines). `Simulate(w)` iterates `w.ArgoApps` sorted by name; `simulateApp` scopes resources to `app.Destination.Namespace` (cluster-scoped resources always pass), buckets by sync wave, emits one `Step` per wave with `Delta.Created` plus `Delta.Deleted` for resources annotated `argocd.argoproj.io/hook-delete-policy: HookSucceeded|HookFailed`. `State` accumulates across waves. PruneLast is documented as a future extension (comment at `simulate.go:62-71`).
- `trajectory_test.go` (145 lines). Empty-world, single-wave, multi-wave accumulation, hook-delete, namespace scoping.

#### 2e. `pkg/checker/` — the Go↔Shen bridge

- `bridge.go` (972 lines). Global state: `shenOnce sync.Once`, `shenCF kl.ControlFlow`, `shenErr error` (`bridge.go:38-42`). `initShen(kernelPath)` (`:46-106`) calls `shenfull.Init(&shenCF)`, walks upward for `kernel/check.shen`, chdirs into it (Shen's `read-file` is relative), redirects `*stoutput*` to `/dev/null` to suppress the bootstrap banner, evaluates `(load "check.shen")`, then restores stdout and cwd. Public API:
  - `Check(w, cfg)` (`:137-140`) — diagnostics only.
  - `CheckWithObligations(w, cfg)` (`:142-171`) — full `RunResult`.
  Pipeline inside `CheckWithObligations`: `enrichSyncWaves(w)` (`:177-238`, adds entries from resource annotations) → `resolvePatchTypes(w)` (`:240-298`, walks schemas using `schemas.ResolveFieldType` and appends `__resolved_types` transforms) → `trajectory.Simulate(w)` → `worldToShenObj(w, cfg.StrictConversions, trajectories)` (`:329-509`, deterministic sort + section builders `crdToObj` through `immutableFieldToObj` + stepToObj/deltaToObj at `:746-778`) → `kl.Call(&shenCF, checkWorldFunc, worldObj)` → `objToDiagnostics(result)` (`:814-838`) → `buildRunResult(diags)` (`:940-971`). Severity map at `:890-905` recognizes `"satisfied"` as a sentinel (filtered from visible diagnostics but counted for audit). `obligationRefForCode` (`:843-872`) is the hardcoded XPC code → ObligationRef table (XPC001→`C/version-coherence`, XPC002→`J/conversion-cost-opt-in`, etc., up through XPC014).
- `result.go` (27 lines). `RunResult{Diagnostics, TotalObligations, Satisfied, Violated, Unknown, ObligationIDs}` — shape preserved from the deleted obligation framework so callers (CLI, proof) keep working.
- `check_test.go` (281 lines). `loadFixture`, `checkFixture`, `findDiagByCode` helpers (`:11-42`); test cases exercising R1–R7, R12–R14.
- `bridge_serialization_test.go` (70 lines, new). `TestBridge_TrajectorySerialization` asserts that `(mount-refs …)`, `(trajectory …)`, `(step …)`, `(delta …)` appear in serialized output.

#### 2f. `pkg/audit/proof.go` (521 lines)

Merkle-tree audit proofs. Data structures:
- `Proof{Version, RootDigest, Metadata, Run, RuleSubtrees, ResourceSubtrees, Tree}` (`proof.go:20-44`).
- `ProofMetadata{IRDigest, SnapshotDigest, KernelVersion, RulesetVersion, RulesetDigest, Timestamp, SigningIdentity, Signature, Repo, Commit, Cluster}`.
- `RunSummary{TotalObligations, Satisfied, Violated, Unknown, ObligationIDs}` (`:48-54`).
- `Judgment{Status, RuleID, Resource, Message, ObligationID, Category, Generator, Digest}` (`:107-120`).

Constants: `KernelVersion = "0.1.0"` (`:123`), `RulesetVersion = "2026.04"` (`:126`). `Generate(diags, summary, irDigest, snapshotDigest)` at `:128-209` hashes each judgment via `hashJudgment`, groups by rule and resource, hardcodes `RuleSubtrees` keyed to `XPC001..XPC011` (`:179`), and builds the Merkle tree via `buildMerkleTree` (`:211-277`) including a leaf for `RunSummary` (`:220`), attesting obligation completeness. `Verify()`, `VerifyInclusion()`, `Save()`, `LoadProof()`, `Summary()`, `DiffProofs()` round out the API.

#### 2g. `pkg/snapshot/snapshot.go` (365 lines)

Content-addressed, signed cluster snapshots of the type environment (no resource instances). `Snapshot{Version, Digest, Timestamp, ClusterName, KubernetesVersion, CRDs, XRDs, Providers, Functions, Configurations, Compositions, ArgoTrackingMode, Schemas, SigningIdentity, Signature}` (`:19-65`). `ProviderStatus` / `FunctionStatus` wrap the typed info with `Version` + `Healthy`. API: `New(clusterName)`, `FromWorld(world, clusterName)`, `ToWorld()`, `Load(path)`, `Save(path)`, `ComputeDigest()`, `Verify()`, `Diff(a, b)`, `IsStale(ttl)` with default 15 min.

#### 2h. `pkg/report/reporter.go` (506 lines)

Six output formats via `Format` type (`reporter.go:16-26`): human, agent, json, lsp, junit, sarif. `Report(w, diags, format)` and `ReportStdout(diags, format)` dispatch to `reportHuman` (pretty with source excerpts + docs link to `https://xpc.dev/errors/{CODE}`), `reportAgent` (LLM-optimized one-field-per-line, extracts `xpc.dev/accept`/`xpc.dev/declassify` patterns from fix text at `:241-252`), `reportJSON`, `reportJUnit`, `reportSARIF` (2.1.0), `reportLSP` (grouped by file).

#### 2i. `pkg/schemas/fetcher.go` (159 lines)

In-memory + disk-backed cache of OpenAPI schemas in `~/.cache/xpc/schemas/`. `Cache` with `Get`, `Put`, `Digest`. `FieldType` enum + `ResolveFieldType(schema, fieldPath)` walks dotted paths through `properties`, handles `openAPIV3Schema` wrapper. `TypeAssignable(from, to)` allows `unknown → any` and `integer → number`; rejects other mismatches.

### 3. Shen kernel layer (`kernel/`)

17 files, ~1540 lines.

- **`prelude.shen`** (184 lines). Documents the fact shapes the Go bridge emits: `crd-fact`, `xrd-fact`, `composition-fact`, `function-fact`, `resource-fact`, `argo-app-fact`, `schema-fact`, plus the trajectory/RBAC/mount facts. Provides list utilities (`member`, `filter`, `flatten`, `find-first`, `count-if`), string utilities (`split-string`, `api-version->group`, `api-version->version`, `string-contains?`, `starts-with?`), judgment constructors (`make-judgment`, `make-error`, `make-warning`, `make-satisfied`), and `mark-rule` (`:108-113`) which emits a satisfied-marker judgment when a rule returns an empty violation list.
- **`check.shen`** (126 lines). `check-world` (`:55-97`) extracts every section by tag via `extract-section` (`:47-51`), then invokes every rule in sequence (`:79-93`):
  ```
  (mark-rule "XPC001" (check-r1 CRDs XRDs))
  (mark-rule "XPC002" (check-r2 Resources CRDs))
  (mark-rule "XPC003" (check-r3 Compositions XRDs))
  (mark-rule "XPC004" (check-r4 Compositions Functions))
  (mark-rule "XPC005" (check-r5 ResolvedPatches))
  (mark-rule "XPC006" (check-r6 ArgoApps Compositions XRDs Functions))
  (mark-rule "XPC006" (check-r6c ArgoApps CRDs))
  (mark-rule "XPC007" (check-r7 ArgoApps Compositions))
  (mark-rule "XPC008" (check-r8 Resources XRDs))
  (mark-rule "XPC009" (check-r9 Compositions Resources))
  (mark-rule "XPC010" (check-r10 ResolvedPatches))
  (mark-rule "XPC011" (check-r11 Resources Compositions Providers CRDs))
  (mark-rule "XPC012" (check-r12 Trajectory MountRefs))
  (mark-rule "XPC013" (check-r13 Trajectory ImmutableFields))
  (mark-rule "XPC014" (check-r14 Trajectory SARefs RBACBindings))
  ```
  Also provides `run-checker` for a stdin/stdout protocol (`:102-107`) — not used by the Go bridge but available.
- **Rule files** (one `r*.shen` per XPC code):

| File | Code | Invariant (as implemented) |
|---|---|---|
| `r1-versions.shen` (90 L) | XPC001 | Every CRD/XRD version `served`; exactly one CRD `storage`; ≥1 XRD `referenceable`. |
| `r2-conversion.shen` (64 L) | XPC002 | Non-storage write on a webhook-conversion CRD requires annotation `xpc.dev/accept-conversion-webhook: "true"`. |
| `r3-composition-resolves.shen` (61 L) | XPC003 | Composition `compositeTypeRef` names an existing XRD and uses a referenceable version. |
| `r4-pipeline-functions.shen` (76 L) | XPC004 | Each pipeline `functionRef` names an installed Function; `inputAPIVersion` is in `Function.InputVersions`. |
| `r5-patch-typecheck.shen` (37 L) | XPC005 | For each `resolved-patch` fact produced by Go, source type assignable to target type (hardcoded rules: exact match, unknown→any, integer→number). |
| `r6-wave-ordering.shen` (151 L) | XPC006 | R6a: wave(XRD) < wave(XR). R6b: wave(Function) < wave(Composition). R6d: wave(Composition) ≤ wave(XR). |
| `r6c-provider-wave.shen` (73 L) | XPC006 | wave(Provider) < wave(managed resource with matching CRD). |
| `r7-owner-refs.shen` (35 L) | XPC007 | Warn when ArgoApp `TrackingMode = label` and app contains a Composition. |
| `r8-v1v2-machinery.shen` (64 L) | XPC008 | Error if resource carries annotation `xpc.dev/v1-machinery-on-v2-xrd: "true"` (actual detection is delegated to the Go IR builder). |
| `r9-bootstrap.shen` (56 L) | XPC009 | Error if resource carries an acknowledged-gap annotation (detection delegated to Go). `check-r9-step` currently returns `[]`. |
| `r10-secret-taint.shen` (139 L) | XPC010 | Patch from a tainted source (hardcoded paths + suffix/containment on `password`/`secret`/`credential`/`token`/`apikey`) to non-secret sink. |
| `r11-api-deprecation.shen` (211 L) | XPC011 | Hardcoded v1alpha1 deprecations and provider version floors; semver comparison via `parse-semver` / `version-before?`. |
| `r12-no-dangling-mount.shen` (67 L) | XPC012 | Per trajectory step, if a ConfigMap/Secret is in `Delta.Deleted` but mounted non-optionally by a Pod-bearing owner in `State`, error. |
| `r13-no-immutable-change.shen` (58 L) | XPC013 | Trajectory-driven; produces nothing in current simulator because `Delta.Updated` is always empty. |
| `r14-no-rbac-regression.shen` (66 L) | XPC014 | If a Pod-bearing owner is present and pinned to an SA, error when a later step deletes an RBACBinding whose subject is that SA (or deletes its Role/ClusterRole). Conservative — any binding/role deletion touching an active SA fires. |

### 4. Go↔Shen embedding (`internal/shenfull/`)

16 Go files, ~1.4MB in aggregate. All except `init.go` are **auto-generated** Go implementations of Shen language components (do not edit by hand):

| File | Role | Approx size |
|---|---|---|
| `init.go` (52 L, hand-written) | `Init(*kl.ControlFlow)` calls 14 `*Main` loaders in order: TopLevel, Core, Sys, Sequent, Yacc, Reader, Prolog, Track, Load, Writer, Macros, Declarations, TStar, Types. Sets `ns2_1set` (defun) and `try_1catch` globals. | — |
| `core.go` | Core Shen evaluation | 131 KB |
| `prolog.go` | Prolog clause management/unification | 154 KB |
| `reader.go` | Lexer/parser for s-expressions | 214 KB |
| `sequent.go` | Sequent calculus / type system | 105 KB |
| `yacc.go` | Parser combinators | 79 KB |
| `t-star.go` | Type inference / constraint solving | 242 KB |
| `types.go` | Shen-side type representations | 101 KB |
| `load.go` | File loading / module system | 27 KB |
| `sys.go` | System primitives | 69 KB |
| `track.go` | Inference tracking | 28 KB |
| `macros.go` | Macro expansion | 46 KB |
| `writer.go` | Output formatting | 33 KB |
| `declarations.go` | Type declarations | 41 KB |
| `toplevel.go` | REPL / top-level forms | 24 KB |

Upstream dependency: `github.com/tiancaiamao/shen-go v0.0.0-20251114030759-7a6a67ac131d` (declared in `go.mod`). The prior in-tree evaluator `pkg/shen` has been removed.

Note: Shen source files are **not** embedded into the Go binary. They are loaded at runtime from `kernel/check.shen` via Shen's own `(load …)` primitive, which uses relative paths — hence the `os.Chdir` dance in `initShen` at `pkg/checker/bridge.go:74`.

### 5. Test fixtures (`testdata/fixtures/`)

Seven directories, three new on this branch:

| Fixture | Status | Drives |
|---|---|---|
| `basic/` (3 YAML: `composition.yaml`, `xrd.yaml`, `function.yaml`) | Existing | Baseline XRD/Composition/Function wiring (exercises R3, R4). |
| `patch-mismatch/` (`composition.yaml`) | Existing | String → integer patch mismatch (R5). |
| `webhook-conversion/` (`crd.yaml`, `bucket.yaml`) | Existing | Non-storage version write with webhook conversion, no opt-in annotation (R2). |
| `wave-ordering/` (`app.yaml`) | Existing | XRD and XR both at wave 0 (R6). |
| `provider-wave/` (`app.yaml`) | **New** | Provider at wave 3, Widget MR at wave 2 (R6c). |
| `dangling-mount/` (`app.yaml`) | **New** | ConfigMap with `HookSucceeded` delete policy, Pod with non-optional volume mount in same wave (R12). |
| `rbac-regression/` (`app.yaml`) | **New** | ServiceAccount + RoleBinding (`HookSucceeded`) + Pod pinning SA in same wave (R14). |

Fixtures are self-documenting: the expected XPC code is annotated in a comment inline in each YAML; there are no `expected.json`/`findings.yaml` files.

### 6. Documentation (`docs/`)

- **`docs/obligations.md`** (modified on this branch). Reference document for the 12-category obligation taxonomy (A–L). Each category has documented scope, one or more generators, and discharge-function contracts. All 14 kernel rules map to categories:

  | Category | Letter | Rules mapped |
  |---|---|---|
  | Schema obligations | A | partial R5, partial R8 |
  | Reference-resolution | B | R3, R4, partial R5 |
  | Version-coherence | C | R1, partial R8 |
  | AppProject constraints | D | (none) |
  | Sync-option interaction | E | (none) |
  | Trajectory-invariant | F | R6, R9, XPC012, XPC013, XPC014 |
  | Cross-Application | G | R7 |
  | Rendering | H | (none) |
  | Provider-capability | I | (none) |
  | Conversion-cost | J | R2 |
  | Secret-flow | K | R10 |
  | Deprecation/calendar | L | R11 |

  New-style obligation IDs follow `XPC.<Category>.<Generator>[.<Instance>]` (e.g., `XPC.B.comp-xrd-ref.billing-api`). Legacy codes XPC001–XPC014 remain as aliases.

- **`docs/adr/001-bounded-obligation-taxonomy.md`** (Accepted 2026-04-12, §3 superseded). Establishes the 12 fixed categories, the completeness claim, and the R1–R11 → category mapping. §3's architectural sketch (Go-side `for each generator: …` registry) is now explicitly marked superseded by ADR-002.

- **`docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md`** (new, Accepted 2026-04-17). Fixes the substrate division of labor:
  - **Go owns**: parsing (`pkg/loader`), typed IR (`pkg/types`, `pkg/ir`), enrichment (`resolvePatchTypes`, `enrichSyncWaves`), trajectory simulation (`pkg/trajectory`), the Shen bridge.
  - **Shen owns**: all rule logic. Files under `kernel/` are the canonical, executable rulebook.
  - **Interface**: `World → Shen s-expression` contract in `pkg/checker/bridge.go:worldToShenObj`. Adding a rule means adding a World section + a Shen predicate.
  - **No Go-side generators**: `pkg/obligation/` is removed. Taxonomy is documented, not compile-time enforced.
  - **Recipe** for adding an invariant (§4): pick required facts → extend IR or trajectory if needed → thread through `worldToShenObj` → write `kernel/rNN-<name>.shen` → add fixture + integration test.
  - **Explicit consequences**: `Delta.Updated` empty in Phase 2 (R13 framework-only); RBAC regression operates on declared state, not deployed state; renderer integration deferred (blocks categories G+H).

### 7. Workflow skills (`skills/`)

Three markdown files describing usage workflows:

- `skills/xpc-commit.md` — Pre-commit: run `xpc snapshot --output=.xpcsnap` then `xpc check --proof --snapshot=.xpcsnap`. Fix errors, summarize warnings in commit under `xpc warnings:`. "Regulated mode" mentions committing `.xpcproof` alongside manifests for SOC-2 trails.
- `skills/xpc-edit.md` — Real-time editing feedback: run `xpc check`, fix errors before responding, summarize warnings for user consent. Documents the per-code common-fix table (XPC001 through XPC011) and the agent-format output fields (`rule`, `severity`, `problem`, `source`, `fix`, `ack`, `docs`).
- `skills/xpc-review.md` — MR review: `xpc proof show <proof>` then `xpc proof diff <before> <after>`; report judgment counts (unchanged / newly satisfied / newly violated), rule flips, and drift between MR-environment proof and prod proof.

### 8. Deploy manifest (`deploy/presync-hook.yaml`)

93 lines. Argo CD PreSync Job + ServiceAccount + ClusterRole + ClusterRoleBinding in namespace `argocd`. Key fields:
- Hook annotation `argocd.argoproj.io/hook: PreSync`, delete policy `HookSucceeded`, `backoffLimit: 0`.
- Container image `ghcr.io/pyrex41/xpc:latest` running a shell script: `xpc snapshot --cluster=prod --output=/tmp/prod.xpcsnap .` then `xpc check --proof --snapshot=/tmp/prod.xpcsnap --format=agent .`.
- Env: `XPC_PROOF_STORE=s3://xpc-proofs` (not consumed in current code), `XPC_CLUSTER=prod`.
- ClusterRole grants read-only access to `customresourcedefinitions` (apiextensions.k8s.io), `compositeresourcedefinitions` + `compositions` (apiextensions.crossplane.io), `providers` + `functions` + `configurations` (pkg.crossplane.io), `applications` (argoproj.io).

## Code References

- `cmd/xpc/main.go:107-235` — `runCheck` entry-point orchestrating loader → IR → checker → report → audit.
- `cmd/xpc/main.go:597-737` — `errorExplanations` map for XPC001–XPC011.
- `pkg/types/types.go:547-603` — New enriched-ref types (`MountRef`, `SARef`, `RBACBinding`, `RBACRule`, `ImmutableField`).
- `pkg/types/types.go:605-631` — `World` aggregate.
- `pkg/ir/builder.go:61` — `EnrichTrajectoryData` call wired into `Build`.
- `pkg/ir/trajectory_extract.go:12-38` — Dispatch by Kind for enrichment.
- `pkg/ir/immutable_registry.go:7-26` — Static immutable-field registry.
- `pkg/trajectory/simulate.go:28-109` — `Simulate` + `simulateApp`.
- `pkg/checker/bridge.go:46-106` — `initShen` lifecycle.
- `pkg/checker/bridge.go:142-171` — `CheckWithObligations` end-to-end pipeline.
- `pkg/checker/bridge.go:329-509` — `worldToShenObj` serialization.
- `pkg/checker/bridge.go:814-872` — Judgment decoding + `obligationRefForCode` table.
- `pkg/checker/bridge.go:940-971` — `buildRunResult`.
- `pkg/audit/proof.go:128-209` — `Generate` with Merkle build including `RunSummary` leaf at line 220.
- `kernel/check.shen:55-97` — `check-world` dispatch.
- `kernel/prelude.shen:108-113` — `mark-rule` satisfied-marker behavior.
- `kernel/r6-wave-ordering.shen` — R6a/R6b/R6d; R6c split into its own file.
- `kernel/r12-no-dangling-mount.shen`, `kernel/r13-no-immutable-change.shen`, `kernel/r14-no-rbac-regression.shen` — Trajectory-driven rules.
- `internal/shenfull/init.go:21-52` — Shen module bootstrap order.
- `docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md` — Division of labor.
- `docs/obligations.md` — 12-category taxonomy mapping.

## Architecture Documentation

### End-to-end flow

```
YAML files
  ↓ pkg/loader         (recursive walk, yaml.Node preserved for source loc)
[]LoadedDocument
  ↓ pkg/ir.Builder.Build
  ↓   → addCRD/addXRD/addComposition/addFunction/addProvider/...
  ↓   → EnrichTrajectoryData (MountRefs, SARefs, RBACBindings, RBACRules, ImmutableFields)
types.World
  ↓ optional merge from pkg/snapshot (cmd/xpc/main.go mergeSnapshotIntoWorld)
  ↓ pkg/checker.CheckWithObligations
  ↓   → enrichSyncWaves (pull sync-wave annotations onto ArgoApps)
  ↓   → resolvePatchTypes (walk schemas, append __resolved_types transforms)
  ↓   → trajectory.Simulate → []trajectory.Step
  ↓   → worldToShenObj → kl.Obj s-expression
  ↓   → shenfull.Init (sync.Once), (load "check.shen")
  ↓   → kl.Call(check-world, worldObj)
  ↓   → objToDiagnostics (parse judgments, attach ObligationRef)
  ↓   → buildRunResult (partition visible vs satisfied, dedupe obligation IDs)
checker.RunResult
  ↓ pkg/report.ReportStdout (human | agent | json | lsp | junit | sarif)
  ↓ pkg/audit.Generate (optional, --proof) → .xpcproof
exit code (non-zero if error-severity diagnostics)
```

### Division of labor (ADR-002)

- **Go is the only side that does state-accumulating work.** Trajectory simulation, schema walking, annotation-driven enrichment, and all I/O live in Go.
- **Shen is the only side that emits judgments.** Every diagnostic comes from a Shen rule; `obligationRefForCode` in Go is metadata attached after the fact, not a second rule registry.
- **Facts shaped as s-expressions are the contract.** Adding a new fact means extending `worldToShenObj` with a new section tag and documenting it in `kernel/prelude.shen`.

### Obligation-awareness concretely

- `mark-rule` emits a synthetic `(judgment "XPCxxx" satisfied …)` when a rule runs and finds nothing. The Go severity decoder filters these from visible output but retains them for audit counts.
- `buildRunResult` counts distinct obligation IDs (from `Diagnostic.Obligation.ID` or fallback `"XPC." + Code`) and populates `ObligationIDs` sorted for determinism.
- `audit.Generate` hashes the `RunSummary` into a Merkle leaf (`proof.go:220`), committing both the set of evaluated obligations and the satisfied/violated counts into the root digest.

### Completeness caveats present in the implemented set

- Six taxonomy categories have zero rules: **A** (general schema), **D** (AppProject constraints), **E** (sync-option interactions), **H** (rendering), **I** (provider capability), and most of **F** beyond the 5 rules that made it.
- `audit.Generate` still hardcodes `RuleSubtrees` to XPC001..XPC011; XPC012/013/014 appear only in `ResourceSubtrees`.
- `buildRunResult` does not increment `TotalObligations`/`Satisfied`/`Violated`/`Unknown` counts — only `Diagnostics` and `ObligationIDs` are populated.
- `Delta.Updated` is always empty in the trajectory simulator, so R13 is framework-only.
- R8 and R9 are partly or fully annotation-passthrough (Shen reads an annotation set — or not set — by Go).
- R10 is a substring/keyword blocklist, not a taint lattice.
- R11 is a hardcoded table of ~5 v1alpha1 deprecations and 2 provider-version floors; no k8s-version or date awareness.

## Historical Context (from thoughts/)

- `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md` — Chronological recap of the substrate pivots plus a rule-by-rule audit of what the kernel actually implements versus what the taxonomy promises. Named the "Go simulates, Shen checks" direction before ADR-002 was written.

The 5-phase trajectory-invariants plan and its handoff are captured in git history (commits `91b0317`, `7d0f57d`, `07bc8b6`, `8e0b9e0`) and in ADR-002.

## Related Research

- `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md`

## Open Questions

Carried forward from the thoughts docs (not resolved on the current branch):

1. Should `pkg/checker/rules.go` and `pkg/checker/rules_test.go` (legacy pure-Go rule functions, orphaned since PR #3) be deleted on this branch? (Not present in current package tree — need to re-verify; the vision-recap doc flagged them as still on disk.)
2. What is the plan for the six zero-rule categories (A, D, E, H, I, and the unfilled portions of F)?
3. Should `audit.Generate` consume `RunResult.ObligationIDs` / `ObligationRef` and update its hardcoded `RuleSubtrees` list to include XPC012–XPC014?
4. Should `buildRunResult` populate `TotalObligations`/`Satisfied`/`Violated`/`Unknown` counts, or are they intentional stubs?
5. What does "done" mean for R8 (annotation passthrough), R9 (hollow), and R10 (substring blocklist)? Are these considered complete-as-is or open?
6. The Go module path has a trailing hyphen (`github.com/pyrex41/cross-validate-`) — is this intentional?
7. When will `Delta.Updated` be populated so R13 produces diagnostics?
8. Is the renderer-integration gap (blocking categories G+H) scoped?
