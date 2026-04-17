---
date: 2026-04-17T22:36:12Z
researcher: Reuben Brooks
git_commit: c05cbf43a0a00377296d22906842a26796883759
branch: claude/phase1-cleanup
repository: cross-validate
topic: "Deferred simplify candidates — RunResult.Unknown, State.Resources shape, bridge.go sort-and-map duplication, ConfigMap/Secret literal sites"
tags: [research, codebase, simplify, audit-proofs, trajectory, bridge-serialization, type-constants]
status: complete
last_updated: 2026-04-17
last_updated_by: Reuben Brooks
---

# Research: Deferred simplify candidates — full code-surface map

**Date**: 2026-04-17T22:36:12Z
**Researcher**: Reuben Brooks
**Git Commit**: `c05cbf43a0a00377296d22906842a26796883759`
**Branch**: `claude/phase1-cleanup`
**Repository**: cross-validate

## Research Question

The /simplify pass on the `claude/phase1-cleanup` branch identified four findings that were deferred as "higher risk":

1. Removing `RunResult.Unknown` (wired into the audit proof hash format).
2. Reshaping `trajectory.State.Resources` to keys-only (efficiency win, but touches serializer + tests).
3. Unifying the five sort-and-map blocks in `bridge.go` (stylistic; no generics helper exists).
4. Introducing typed constants for `"ConfigMap"` / `"Secret"` (touches many files for marginal value).

Document the full surface area each of these would touch — what files, what lines, what tests, what cross-component coupling — so a future decision about whether to act has the full map.

## Summary

All four candidates have well-bounded code surfaces in this repository. No external consumers exist for any of them: there are no on-disk `.xpcproof` fixtures, no cross-version compatibility tests, no third-party packages importing this code, and no documents in `docs/` or `thoughts/` that pin the current shape.

- **`Unknown`** flows through exactly two read sites (`cmd/xpc/main.go:216`, `pkg/audit/proof.go:481`) and zero write sites — it is structurally dead. The proof's `Version` field is set to the constant `2` and no code branches on it.
- **`State.Resources`** is constructed in one place, mutated in three lines, and read in three places — and not one of those reads ever touches the `types.ResourceInfo` value (only the map key).
- **The "five sort-and-map blocks"** are actually thirteen blocks in `worldToShenObj` plus four named `*Less` helper functions; analogous patterns exist in `pkg/snapshot/snapshot.go` (five blocks in `ComputeDigest`) and `pkg/audit/proof.go` (`buildMerkleTree`). The repo declares `go 1.25` in `go.mod` and uses zero generics anywhere.
- **`"ConfigMap"` / `"Secret"`-class literals** appear at fewer than 30 Go-source sites total (excluding tests), distributed across `pkg/ir/trajectory_extract.go`, `pkg/ir/immutable_registry.go`, `pkg/loader/loader.go`, and `pkg/checker/bridge.go`. `pkg/types/types.go` defines no Kubernetes-kind typed constants today.

## Detailed Findings

### Item 1 — `RunResult.Unknown`

#### Field definitions

- `pkg/checker/result.go:20` — `Unknown int` on `RunResult`. No JSON tag (the struct is never marshaled).
- `pkg/audit/proof.go:52` — `Unknown int json:"unknown"` on `RunSummary`.

#### Read sites (exhaustive — only two exist)

- `cmd/xpc/main.go:216` — inside the `--proof` branch of `runCheck`, the field is copied into the `audit.RunSummary` literal: `Unknown: result.Unknown`.
- `pkg/audit/proof.go:481` — inside `hashRunSummary`, the field is the fourth integer in the SHA-256 hash format string `"%d|%d|%d|%d|"`.

#### Write sites (exhaustive — none in production paths)

- `pkg/checker/bridge.go` `buildRunResult` (lines 936–967) constructs `RunResult` and **never sets `Unknown`** — the field always carries Go's zero value of `0`.
- The two error-path early-returns in `CheckWithObligations` (`pkg/checker/bridge.go:145–150` and `:160–165`) also omit `Unknown`.
- `cmd/xpc/main.go:216` writes `result.Unknown` (always `0`) into `RunSummary.Unknown`.

#### Proof hash format and Merkle root coupling

- `hashRunSummary` (`pkg/audit/proof.go:479–489`): SHA-256 over the format string `"%d|%d|%d|%d|"` with `TotalObligations`, `Satisfied`, `Violated`, `Unknown` in that order, followed by the sorted `ObligationIDs` joined by `|`.
- Called from `buildMerkleTree` (`pkg/audit/proof.go:212–246`) at line 221, gated by `if p.Run != nil`. The result is appended as a leaf to the Merkle tree.
- The leaves slice (metadata leaf + run-summary leaf + per-rule subtree digests + per-resource subtree digests) feeds `computeMerkleRoot` (`pkg/audit/proof.go:249–277`), whose output becomes `p.RootDigest`.

#### Proof versioning surface

- `pkg/audit/proof.go:133` — `Generate` always sets `p.Version = 2`.
- No `switch p.Version` or `if p.Version == 1` exists anywhere in the repo.
- `pkg/audit/proof_test.go:31` asserts `p.Version != 2` as a failure condition.
- There is no v1-compat read path, no version-migration code, no `.xpcproof` fixture committed to the repo (`testdata/` contains zero `.xpcproof` files).

#### `Verify()` semantics

- `pkg/audit/proof.go:302–306`:
  ```go
  func (p *Proof) Verify() bool {
      saved := p.RootDigest
      p.buildMerkleTree()
      return p.RootDigest == saved
  }
  ```
- This is destructive — `buildMerkleTree` overwrites `p.RootDigest` in place. If verification passes the new value equals the saved value; if it fails, `p.RootDigest` holds the freshly computed (mismatching) value after the call.
- Called at `cmd/xpc/main.go:377` in `runVerify` and at `pkg/audit/proof_test.go:66` and `:213`.

#### Persistence

- `Save` (`pkg/audit/proof.go:280–286`): `json.MarshalIndent` then `os.WriteFile(path, data, 0o644)`.
- `LoadProof` (`pkg/audit/proof.go:289–299`): `os.ReadFile` then `json.Unmarshal` into a zero-value `Proof`.
- All fields including `Run` round-trip through standard JSON tags.
- The default output path written by `cmd/xpc/main.go:220` is `"check.xpcproof"` in the working directory.

#### External consumers

- Zero `.xpcproof` files in `testdata/` or anywhere in the repo.
- Zero cross-version compatibility tests.
- `deploy/presync-hook.yaml:40` invokes `xpc check --proof` in a pre-sync container but does not pin the internal hash format.
- `skills/xpc-commit.md` and `skills/xpc-review.md` reference the `.xpcproof` filename convention but not the hash format.

#### End-to-end flow for `xpc check --proof <path>`

1. `cmd/xpc/main.go:119` parses `--proof` → `generateProof = true`.
2. `cmd/xpc/main.go:192` calls `checker.CheckWithObligations(world, cfg)` → `RunResult` (with `Unknown == 0`).
3. `cmd/xpc/main.go:212–218` constructs `*audit.RunSummary{TotalObligations, Satisfied, Violated, Unknown: result.Unknown, ObligationIDs}`.
4. `cmd/xpc/main.go:219` calls `audit.Generate(diags, summary, irDigest, snapDigest)` → `*Proof`.
5. `cmd/xpc/main.go:221–224` calls `p.Save("check.xpcproof")`.

### Item 2 — `trajectory.State.Resources` shape and surface

#### Type definition (`pkg/trajectory/trajectory.go:26–37`)

- `State struct { Resources map[ResourceKey]types.ResourceInfo }`
- `ResourceKey struct { APIVersion, Kind, Namespace, Name string }`
- `KeyOf(r types.ResourceInfo) ResourceKey` — copies the four key fields.

#### Construction and writes (`pkg/trajectory/simulate.go`)

- Line 67 — `state := State{Resources: map[ResourceKey]types.ResourceInfo{}}` (one allocation per app, before the wave loop).
- Line 76 — `state.Resources[key] = r` — full `types.ResourceInfo` value written for every create.
- Line 83 — `delete(state.Resources, key)` — for hook-deletion entries.
- Lines 132–138 — `cloneState(s State) State`: `make(map[...], len(s.Resources))` then `for k, v := range s.Resources { copy.Resources[k] = v }`. Shallow copy — fields like `ResourceInfo.Annotations` (which is itself a map) share backing memory with the original.
- Line 97 — `cloneState(state)` is called once per wave step. For an app with W waves, W clones occur, with the N-th clone copying the cumulative state through wave N.

#### Read sites (exhaustive)

**Production code:**

- `pkg/checker/bridge.go:752–767` — `stepToObj`:
  ```go
  var stateKeys []trajectory.ResourceKey
  for k := range s.State.Resources {
      stateKeys = append(stateKeys, k)
  }
  ```
  The `range` ignores the value. `stateKeys` is then sorted via `sortResourceKeys` and serialized as `section("state", resourceKeyObjs(stateKeys))`.

**Tests:**

- `pkg/trajectory/trajectory_test.go:80` — `len(steps[1].State.Resources) != 2` (length only).
- `pkg/trajectory/trajectory_test.go:112–114` — `steps[0].State.Resources[ResourceKey{...}]`, with the value discarded via `_`.

No expression of the form `state.Resources[key].<Field>` exists anywhere in the repo.

#### Serialization to Shen kernel (call chain)

1. `worldToShenObj` (`pkg/checker/bridge.go:329`) calls `trajectoryToObj(trajectories)` at line 503, placing the result as the last element of the world's section list.
2. `trajectoryToObj` (`pkg/checker/bridge.go:769–775`) iterates steps, calling `stepToObj(s)` for each, and wraps as `section("trajectory", stepObjs)`.
3. `stepToObj` (`pkg/checker/bridge.go:752–767`) emits `(step AppName Wave (delta ...) (state rk1 rk2 ...))`. Each `rk` is a `(resource-key APIVersion Kind Namespace Name)` tuple from `resourceKeyToObj` (`pkg/checker/bridge.go:728–733`).

What reaches the kernel: only the four-field key tuples. The full `ResourceInfo` payload (annotations, source) is not duplicated — it lives in the top-level `(resources …)` section emitted by `resourceToObj`.

#### What the Shen kernel reads from `state`

- `kernel/prelude.shen`:
  - `state-keys` — pattern `[state | Keys] -> Keys`.
  - `key-in?` — pattern `[resource-key _ Kind Ns Name]`. The `APIVersion` slot is matched with `_` and never inspected.
- `kernel/r12-no-dangling-mount.shen` `check-r12-step` (lines 9–15) — destructures `[step AppName Wave Delta StateSec]`, calls `state-keys StateSec`, then `key-in? OK ON ONs State` to test owner presence.
- `kernel/r14-no-rbac-regression.shen` `check-r14-step` (lines 19–25) — destructures `[step AppName Wave Delta _]`. The state slot is bound to `_` and never read; R14 uses only `delta-deleted-keys`.
- `kernel/r13-no-immutable-change.shen` `check-r13-step` — also binds the state slot to `_` (it reads `delta-updated-keys`).

#### Tests exercising the State shape

| Test | File:Line | What it constructs / asserts |
|---|---|---|
| `TestSimulate_EmptyWorld` | `pkg/trajectory/trajectory_test.go:11` | Empty world → 0 steps |
| `TestSimulate_WaveOrderingFixture` | `pkg/trajectory/trajectory_test.go:19` | Loads `wave-ordering` fixture; checks `Delta.Created` non-empty |
| `TestSimulate_MultipleWavesAccumulateState` | `pkg/trajectory/trajectory_test.go:47` | Inline world, 2 resources at waves 0/1; asserts `len(steps[1].State.Resources) == 2` |
| `TestSimulate_HookDeletePolicyProducesDeleted` | `pkg/trajectory/trajectory_test.go:85` | Inline world, `HookSucceeded` policy; asserts key absent via map lookup |
| `TestSimulate_ScopeToDestinationNamespace` | `pkg/trajectory/trajectory_test.go:119` | Inline world, namespaced app; asserts `Delta.Created` length |
| `TestBridge_TrajectorySerialization` | `pkg/checker/bridge_serialization_test.go:12` | Builds a Step literal with `State.Resources` populated; asserts serialized string contains `"state"` and `"resource-key"` substrings |
| `TestR12_DanglingMount` | `pkg/checker/check_test.go:228` | End-to-end via `dangling-mount` fixture; asserts XPC012 fires |
| `TestR13_RuleLoaded` | `pkg/checker/check_test.go:242` | Asserts XPC013 does not fire on `basic` (Delta.Updated always nil) |
| `TestR14_RbacRegression` | `pkg/checker/check_test.go:253` | End-to-end via `rbac-regression` fixture; asserts XPC014 fires |

The Step-literal construction at `pkg/checker/bridge_serialization_test.go:46–49` is the single test site that builds a `State.Resources` map outside of `Simulate` itself.

### Item 3 — Sort-and-map duplication in `bridge.go`

#### Inventory of all sort-and-map blocks in `worldToShenObj`

All in `pkg/checker/bridge.go`. Each block follows the shape: `slice := append([]T(nil), w.X...); sort.Slice(slice, less); var objs []kl.Obj; for _, x := range slice { objs = append(objs, xToObj(x)) }`.

| Block lines | Type T | Comparator | toObj |
|---|---|---|---|
| 330–375 | `types.CRDInfo` | inline (Group, Kind) | `crdToObj` |
| 338–378 | `types.CRDInfo` (XRDs) | inline (Group, Kind) | `xrdToObj` |
| 346–382 | `types.CompositionInfo` | inline (Name) | `compositionToObj` |
| 349–385 | `types.FunctionInfo` | inline (Name) | `functionToObj` |
| 352–388 | `types.ProviderInfo` | inline (Name) | `providerToObj` |
| 355–391 | `types.ConfigurationInfo` | inline (Name) | `configToObj` |
| 358–397 | `types.ResourceInfo` | inline (Kind, Name) | `resourceToObj` |
| 366–401 | `types.ArgoApplication` | inline (Name) | `argoAppToObj` |
| 443–447 | `types.MountRef` | named `mountRefLess` | `mountRefToObj` |
| 450–454 | `types.SARef` | named `saRefLess` | `saRefToObj` |
| 457–461 | `types.RBACBinding` | named `rbacBindingLess` | `rbacBindingToObj` |
| 464–468 | `types.RBACRule` | named `rbacRuleLess` | `rbacRuleToObj` |
| 471–483 | `types.ImmutableField` | inline (Group, Kind, FieldPath) | `immutableFieldToObj` |

13 blocks total. The `patchObjs` block (`pkg/checker/bridge.go:405–441`) does not sort — it iterates within already-sorted `comps`.

#### Named `*Less` functions

All in `pkg/checker/bridge.go`:

- `mountRefLess` (lines 508–522): OwnerKind, OwnerName, TargetKind, TargetName, MountKind.
- `saRefLess` (lines 524–532): OwnerKind, OwnerName, SAName.
- `rbacBindingLess` (lines 534–545): BindingKind, BindingName, SubjectKind, SubjectName.
- `rbacRuleLess` (lines 547–558): OwnerKind, OwnerName, len(Verbs), `strings.Join(Verbs, ",")`.

Representative form (`mountRefLess`):
```go
func mountRefLess(a, b types.MountRef) bool {
    if a.OwnerKind != b.OwnerKind { return a.OwnerKind < b.OwnerKind }
    if a.OwnerName != b.OwnerName { return a.OwnerName < b.OwnerName }
    if a.TargetKind != b.TargetKind { return a.TargetKind < b.TargetKind }
    if a.TargetName != b.TargetName { return a.TargetName < b.TargetName }
    return a.MountKind < b.MountKind
}
```

#### Existing helpers in `bridge.go`

- `sym(s string) kl.Obj` — line 303.
- `str(s string) kl.Obj` — line 304.
- `num(n int) kl.Obj` — line 305.
- `makeList(items []kl.Obj) kl.Obj` — lines 314–320; right-folded cons list terminating with `kl.Nil`.
- `section(tag string, facts []kl.Obj) kl.Obj` — lines 323–325; wraps with a tag symbol.

#### Conversion functions inventory

All in `pkg/checker/bridge.go`:

| Lines | Function |
|---|---|
| 560–576 | `crdToObj` |
| 578–591 | `xrdToObj` |
| 593–616 | `compositionToObj` |
| 618–629 | `functionToObj` |
| 631–637 | `providerToObj` |
| 639–645 | `configToObj` |
| 647–663 | `resourceToObj` (sorts annotation keys inline) |
| 665–678 | `argoAppToObj` |
| 680–688 | `mountRefToObj` |
| 690–697 | `saRefToObj` |
| 699–708 | `rbacBindingToObj` |
| 709–719 | `rbacRuleToObj` |
| 721–726 | `immutableFieldToObj` |

#### Generics and ordered-stdlib usage

- `go.mod` declares `go 1.25` (generics available, `slices` and `cmp` available).
- Search for `[T any]`, `[T comparable]` across all Go files: **zero matches**.
- Search for `slices.SortFunc`, `slices.SortStableFunc`, `cmp.Or`, `stableMap`: **zero matches**.

#### Other sort-and-map patterns in the repo

- `pkg/trajectory/simulate.go:31–32` — sort `apps` by Name.
- `pkg/trajectory/simulate.go:140–150` — `sortKeys(keys []ResourceKey)` by Kind/Namespace/Name.
- `pkg/trajectory/simulate.go:64` — `sort.Ints(sorted)` on wave numbers.
- `pkg/audit/proof.go:225–230` — collect-then-sort rule IDs in `buildMerkleTree`.
- `pkg/audit/proof.go:234–239` — collect-then-sort resource keys in `buildMerkleTree`.
- `pkg/audit/proof.go:482–484` — sort `ObligationIDs` in `hashRunSummary`.
- `pkg/audit/proof.go:396` — sort rule IDs in `DiffProofs`.
- `pkg/snapshot/snapshot.go:93–163` — `ComputeDigest` has five copy-then-`sort.Slice` blocks (CRDs, XRDs, Providers, Functions, Compositions) each followed by `json.Marshal` + `h.Write`.
- `pkg/ir/sexpr.go:19–111` — `ToSExpr` iterates world slices in input order with no sort; the `w.Schemas` map iteration is non-deterministic per-call.
- `pkg/checker/bridge.go:648–656` (inside `resourceToObj`) — sorts annotation map keys inline.

#### Tests asserting section ordering

- `pkg/checker/bridge_serialization_test.go:12–69` — `TestBridge_TrajectorySerialization` asserts via `strings.Contains` only; no ordering assertion.
- `pkg/checker/check_test.go` — every test asserts on diagnostic `Code` fields, not on serialized section order.

No test asserts a specific section ordering anywhere in the repo.

### Item 4 — `"ConfigMap"` / `"Secret"` and related kind literals

#### Production Go source sites

**`pkg/ir/trajectory_extract.go`** — switch dispatch and struct field assignment:
- Line 18 — `case "Pod":`
- Line 20 — `case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job":`
- Line 24 — `case "CronJob":`
- Line 30 — `case "RoleBinding", "ClusterRoleBinding":`
- Line 32 — `case "Role", "ClusterRole":`
- Line 77 — `TargetKind: "ConfigMap"` (volume mount)
- Line 94 — `TargetKind: "Secret"` (volume mount)
- Line 117 — `TargetKind: "ConfigMap"` (projected volume)
- Line 134 — `TargetKind: "Secret"` (projected volume)
- Line 166 — `TargetKind: "ConfigMap"` (envFrom)
- Line 183 — `TargetKind: "Secret"` (envFrom)

**`pkg/ir/immutable_registry.go`** — registry entries:
- Line 17 — `Kind: "Job"` (`spec.selector`)
- Line 19 — `Kind: "Job"` (`spec.template`)
- Line 21 — `Kind: "StatefulSet"` (`spec.serviceName`)
- Line 23 — `Kind: "StatefulSet"` (`spec.volumeClaimTemplates`)

**`pkg/loader/loader.go`**:
- Line 123 — `case doc.Kind == "CompositeResourceDefinition":` (classification)

**`pkg/checker/bridge.go`** (inside `enrichSyncWaves`):
- Line 185 — `"CompositeResourceDefinition/" + xrd.Kind` (map-key concatenation)
- Line 189 — `if res.Kind == "CompositeResourceDefinition"`
- Line 194 — `Kind: "CompositeResourceDefinition"` (struct field)

#### Test files (Go)

- `pkg/trajectory/trajectory_test.go` — 7 `Kind:` literals across lines 56, 61, 93, 113, 127–129.
- `pkg/checker/bridge_serialization_test.go` — 10 literals across lines 18–47 (`Pod`, `ConfigMap`, `RoleBinding`, `ServiceAccount`, `Role`, `Secret`).
- `pkg/ir/trajectory_extract_test.go` — 14 literals across lines 32–252 (`Pod`, `ConfigMap`, `Secret`, `Deployment`, `CronJob`, `RoleBinding`, `Role`, `ServiceAccount`, `ClusterRole`).

#### Shen kernel sites

- `kernel/r3-composition-resolves.shen:17` — `"CompositeResourceDefinition"` (string concat in error message).
- `kernel/r6-wave-ordering.shen:33` — `"CompositeResourceDefinition"` (pattern-match arg to `find-wave`).
- `kernel/r6-wave-ordering.shen:46` — `"CompositeResourceDefinition "` (string concat in error message).
- `kernel/r6c-provider-wave.shen:52` — `(= Kind "CompositeResourceDefinition")` (equality test).
- `kernel/r14-no-rbac-regression.shen:57` — `"ServiceAccount"` (literal in fact pattern `[rbac-binding-fact _ _ _ "ServiceAccount" ...]`).

(Comment-only mentions in `r12`, `r13`, `r14` headers are not literals in code.)

#### Existing typed string constants in `pkg/types/types.go`

No Kubernetes-kind constants. The `Kind` fields on every struct are plain `string`. Existing typed string consts in the package:

- `CostClass` (lines 8–11): `None`, `Identity`, `Structural`, `Webhook`.
- `Severity` (lines 18–20): `error`, `warning`, `info`.
- `RendererKind` (lines 200–203): `helm`, `kustomize`, `directory`, `plugin`.
- `ArgoAppSetGeneratorKind` (lines 461–467): `list`, `cluster`, `git`, `matrix`, `merge`, `scmProvider`, `pullRequest`.

Comments at lines 551, 574, 577, 582, 584 document expected kind values for plain `string` fields (e.g., `// ConfigMap | Secret`, `// RoleBinding | ClusterRoleBinding`).

#### Test fixtures

Four fixture directories contain YAML manifests with natural `kind: ConfigMap` / `kind: Secret` etc. — these are real Kubernetes manifests, not Go literals:
- `testdata/fixtures/wave-ordering/app.yaml`
- `testdata/fixtures/dangling-mount/app.yaml`
- `testdata/fixtures/rbac-regression/app.yaml`
- `testdata/fixtures/provider-wave/app.yaml`

## Code References

- `pkg/checker/result.go:20` — `RunResult.Unknown int` (no JSON tag)
- `pkg/audit/proof.go:52` — `RunSummary.Unknown int json:"unknown"`
- `pkg/audit/proof.go:481` — hash format string `"%d|%d|%d|%d|"` with `Unknown` as 4th field
- `pkg/audit/proof.go:212–246` — `buildMerkleTree`; line 220–221 gates on `p.Run != nil`
- `pkg/audit/proof.go:302–306` — `Verify()` (destructive in-place recompute)
- `pkg/checker/bridge.go:936–967` — `buildRunResult` (does not write `Unknown`)
- `cmd/xpc/main.go:212–224` — `RunSummary` construction and `Save("check.xpcproof")`
- `pkg/trajectory/trajectory.go:26–37` — `State` and `ResourceKey` definitions
- `pkg/trajectory/simulate.go:67,76,83,97,132–138` — state mutation lifecycle
- `pkg/checker/bridge.go:752–767` — `stepToObj` (only consumer that reads `State.Resources`, keys only)
- `pkg/checker/bridge.go:329–509` — `worldToShenObj` (13 sort-and-map blocks)
- `pkg/checker/bridge.go:508–558` — four named `*Less` comparators
- `pkg/checker/bridge.go:303–325` — `sym`/`str`/`num`/`makeList`/`section` helpers
- `pkg/checker/bridge.go:560–726` — 13 conversion functions
- `pkg/snapshot/snapshot.go:93–163` — `ComputeDigest` (5 sort-and-map blocks)
- `pkg/audit/proof.go:212–246` — `buildMerkleTree` (2 sort-and-iterate blocks)
- `pkg/ir/trajectory_extract.go:18–32` and `:77–183` — kind switch dispatch and `TargetKind` literals
- `pkg/ir/immutable_registry.go:17–23` — `Kind` literals in registry entries
- `pkg/loader/loader.go:123` — `"CompositeResourceDefinition"` classification
- `pkg/checker/bridge.go:185,189,194` — `"CompositeResourceDefinition"` in `enrichSyncWaves`
- `kernel/r3-composition-resolves.shen:17`, `kernel/r6-wave-ordering.shen:33,46`, `kernel/r6c-provider-wave.shen:52`, `kernel/r14-no-rbac-regression.shen:57` — Shen-side kind literals

## Architecture Documentation

### Proof-format coupling

The audit proof's Merkle root is a function of:
1. The metadata leaf (`json.Marshal` of `ProofMetadata`).
2. The run-summary leaf when `Run != nil` (`hashRunSummary` over four integers + sorted obligation IDs).
3. Per-rule subtree digests (sorted by rule ID).
4. Per-resource subtree digests (sorted by resource key).

The hash format is private to `pkg/audit` — there is no documented external schema for the on-wire format, no v1/v2 compatibility shim, and no committed `.xpcproof` fixture. `Proof.Version = 2` is set unconditionally; no consumer in this repo branches on it.

### Trajectory state lifecycle

Per `pkg/trajectory/Simulate`:
- A single mutable `State` is created per app and threaded through the wave loop.
- After each wave's creates and hook-deletes apply, `cloneState` snapshots the current state into the `Step` being emitted.
- The shared mutable `state` continues to evolve; each `Step` carries an independent post-clone copy.
- The clone is shallow: top-level map is independent, but `ResourceInfo.Annotations` (a nested map) and other reference-typed fields share backing memory.

The downstream consumer (`stepToObj`) reads only the map keys and never inspects values. The Shen kernel sees only `(resource-key APIVersion Kind Namespace Name)` tuples.

### `worldToShenObj` shape

Each top-level world section is constructed by:
1. Copy the slice from `World.X` (so the `World` is not mutated).
2. Sort with a less function (inline closure or named `*Less`).
3. Convert each element via `xToObj` to a Shen `kl.Obj` cons-list.
4. Wrap with `section(tag, objs)` which prepends a symbol tag.

The 13 sections are assembled into a top-level `(world (crds …) (xrds …) … (trajectory …))` list. Determinism is guaranteed by the per-slice sorts; tests assert section presence by substring rather than ordering.

### Kind-string strategy

The codebase treats Kubernetes `Kind` as a plain `string` everywhere — both at the Go struct boundary (`types.go`) and in dispatch sites (`switch res.Kind { case "Pod": }`). The same kind strings appear unquoted on the Shen side (e.g., `(= Kind "CompositeResourceDefinition")` in `r6c-provider-wave.shen`). There is no central declaration of expected kinds.

The closest analogues that *are* typed are domain enums that don't correspond to Kubernetes kinds (`CostClass`, `Severity`, `RendererKind`, `ArgoAppSetGeneratorKind`).

## Historical Context (from thoughts/)

- `thoughts/shared/research/2026-04-17-full-codebase-review.md` — Documents `RunResult` shape including `Unknown` (line 120), `RunSummary` shape (line 129), the Merkle `Generate` flow (line 294), and notes that `buildRunResult` count fields are stub zeros (line 347). Documents `simulate.go` algorithm (line 111). Documents `worldToShenObj` (line ~329 reference) and the trajectory serialization test (line 122). References `ConfigMap`/`Secret` only as plain-English rule descriptions (lines 186, 228).
- `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md` — Notes `RunResult` was preserved verbatim from the deleted obligation framework so the API surface is unchanged (line 81). Flags as open debt that `audit.Generate` only knows `XPC001`–`XPC011` in its Merkle `RuleSubtrees` (line 346). Mentions `worldToShenObj` sorts every input slice before serialization for determinism (line 331).
- `docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md` (line 59) — "The sync trajectory — waves, hook phases, state after each step — is computed in `pkg/trajectory/Simulate` and serialized into the `(trajectory …)` section." No discussion of state shape or performance.

**Topics with zero historical discussion** in `thoughts/`:
- Go generics use in this project.
- Helper-function design strategy for `worldToShenObj` sort-and-map blocks.
- Typed constants for Kubernetes resource kinds.
- Stringly-typed code as a critique target.
- Per-wave state-clone performance.
- Proof-format versioning beyond setting `Version = 2`.

## Related Research

- `thoughts/shared/research/2026-04-17-full-codebase-review.md` — full post-pivot codebase snapshot
- `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md` — substrate-pivot history and rule-by-rule audit

## Open Questions

(Not recommendations — these are loose ends a future session would need to chase.)

1. **Proof-format compatibility intent**: `Proof.Version = 2` is set unconditionally with no v1 read path. Is the project tracking proof-format compatibility against external consumers (CI, deployed pre-sync hooks) outside this repo, or is the `.xpcproof` format treated as an internal current-version-only artifact? No on-disk fixtures or compatibility tests exist to answer this.
2. **`audit.Generate` rule allow-list**: The hardcoded `allRules` list in `pkg/audit/proof.go:179–180` enumerates only `XPC001`–`XPC011`. Diagnostics with codes `XPC012`–`XPC014` produced today would land in `ResourceSubtrees` but not get a dedicated `RuleSubtree` entry. This was flagged as open debt in the vision-recap doc.
3. **`Delta.Updated` vs R13 dormancy**: `simulate.go:102` always emits `Updated: nil`; `r13-no-immutable-change.shen` defines the rule but cannot fire until the simulator populates updates. Both pieces ship together by design today.
4. **`pkg/ir/sexpr.go` non-determinism**: `ToSExpr` iterates `w.Schemas` (a map) without sorting, which would make `xpc dump-ir` output non-deterministic for fixtures with schemas. Whether `dump-ir` is treated as a stable surface is unclear.
