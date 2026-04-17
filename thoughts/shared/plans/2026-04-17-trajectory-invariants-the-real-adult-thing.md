# Trajectory Invariants — "The Real Adult Thing" Implementation Plan

## Overview

Add the first three genuine trajectory invariants to the xpc kernel — `no-dangling-mount`, `no-immutable-change`, `no-rbac-regression` — plus the missing `R6c provider-wave < MR-wave` from the existing R6 rule. To make these checkable at all, build a Go-side **trajectory simulator** that walks an Argo App's sync waves step by step over a synthesized cluster state, and feed each step's facts to the existing Shen kernel through a new `(trajectory …)` section of the World s-expression.

Stop pivoting substrate. Commit to "Go simulates, Shen checks" via ADR-002 superseding §3 of ADR-001.

## Current State Analysis

- The Shen kernel currently expresses 11 rules; only **R6 (sync-wave ordering)** is a genuine joint-dynamics invariant (audit in `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md`, follow-up section).
- `R6c (Provider wave < MR wave)` is named in the comment header of [`kernel/r6-wave-ordering.shen:1-11`](kernel/r6-wave-ordering.shen) but **not implemented** — only R6a/R6b/R6d exist.
- ADR-001 §F lists `no-dangling-mount`, `no-immutable-change`, `no-rbac-regression` as the named generators for category F. Zero of them have a Shen rule or a Go generator.
- `pkg/types/types.ResourceInfo` ([types.go:154](pkg/types/types.go)) carries only `APIVersion / Kind / Name / Namespace / Annotations / Labels / Source / Raw`. The `Raw map[string]interface{}` field carries the full parsed YAML — **enough to extract mount references, ServiceAccount refs, and Role/Binding rules without changing the loader**.
- `pkg/snapshot.Snapshot` ([snapshot.go:307](pkg/snapshot/snapshot.go)) captures CRDs/XRDs/Providers (with `Healthy`) but no rendered manifests, no RBAC, no mount graph.
- `pkg/checker/bridge.go` is the only place the Go side talks to Shen. `worldToShenObj` ([bridge.go:301](pkg/checker/bridge.go)) builds a flat tagged cons-list of facts; `enrichSyncWaves` and `resolvePatchTypes` are existing Go-side enrichment passes that demonstrate the pattern.
- `kernel/check.shen` ([check.shen:51](kernel/check.shen)) extracts each section by tag and calls `(check-rN …)` per rule; new sections + new rules slot in by extending one `let` binding and one `append` chain.
- The bridge calls Shen exactly once per check (`kl.Call(&shenCF, checkWorld, worldObj)` at [bridge.go:141](pkg/checker/bridge.go)). Trajectory simulation in this plan happens **before** that call; a single `(trajectory …)` section in the world carries all the steps.
- No `Makefile`. Verification uses `go build ./...`, `go test ./...`, and binary smoke tests against `testdata/fixtures/`.

### Key Discoveries:

- `ResourceInfo.Raw` is the escape hatch — we extract mount/SA/RBAC structure from there, no YAML parser changes needed ([types.go:162](pkg/types/types.go)).
- The "Go pre-resolves, Shen pattern-matches" pattern is already established by `resolvePatchTypes` stamping `__resolved_types` sentinel transforms onto patches ([bridge.go:214](pkg/checker/bridge.go)). Trajectory facts follow the same idiom.
- The Shen evaluator `shenfull.Init + (load "check.shen")` runs once via `sync.Once` ([bridge.go:38](pkg/checker/bridge.go)). Adding new `kernel/r*.shen` files is just adding `(load "rN.shen")` lines to `check.shen` and one new top-level rule call into `check-world`.
- The `obligationRefForCode` table at [bridge.go:600](pkg/checker/bridge.go) is where new XPC codes get their (Category, Generator) provenance attached. Updating it is the only Go-side change needed when a new Shen rule is added.

## Desired End State

After this plan:

- `xpc check` running against an Argo App that mounts a ConfigMap pruned at an earlier wave emits **`XPC012 no-dangling-mount`**.
- `xpc check` against an update that changes `Service.spec.clusterIP` (or any other field in the immutability registry) emits **`XPC013 no-immutable-change`**.
- `xpc check` against a sync that removes a `ClusterRoleBinding` whose `ClusterRole` is still required by a later-wave Pod's ServiceAccount emits **`XPC014 no-rbac-regression`**.
- `xpc check` against the existing wave-ordering fixture extended with a Provider/MR pair flags **`XPC006`** with the R6c sub-claim populated.
- `kernel/check.shen` loads four new rule files and `check-world` includes them in its judgment chain.
- `pkg/trajectory/` exists, is importable, has a public `Simulate(world *types.World) []Step` API, and is tested with golden trajectories for the existing fixtures.
- `docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md` exists and is `Accepted`. It supersedes ADR-001 §3 explicitly.
- `docs/obligations.md` has the four new generators marked as **implemented** under category F.
- `go build ./...` succeeds; `go test ./...` passes; `xpc check` against every existing fixture in `testdata/fixtures/` produces the same diagnostics as before (no regression on R1-R11) **plus** the new trajectory diagnostics where the new fixtures expose them.

## What We're NOT Doing

Explicitly out of scope, to be filed as separate tickets:

- **Renderer integration.** No `helm template`, no `kustomize build`. Trajectory simulation operates on the input manifests as parsed; if the user wants helm-rendered resources checked, they pre-render and feed the output to xpc. This blocks G's `no-duplicate-ownership` / `no-namespace-overlap` and all of category H — those wait.
- **Live cluster snapshots for trajectory state.** The simulator synthesizes the trajectory from the manifests only. RBAC regressions are detected against *declared* state, not deployed state. (ADR-002 records this trade-off.)
- **Provider CRD catalog (Category I).** No `field-available-in-version`, no `field-not-deprecated`, no `controller-healthy` checks beyond the Healthy flag the snapshot already carries.
- **AppProject constraint generators (Category D).** The Argo IR loads `AppProject`s already; we don't add rules against them.
- **Sync-option interaction generators (Category E).** Same — no rules.
- **Rewriting R8, R9, R10.** R8 stays a Go-side annotation passthrough; R9 stays hollow; R10 stays a substring blocklist. Each gets its own follow-up ticket.
- **Removing `pkg/checker/rules.go`** (the orphaned legacy Go rule functions). Already-known dead code; deletion is a separate housekeeping ticket.
- **The trailing hyphen in module name `github.com/pyrex41/cross-validate-`.** Cosmetic; not in this plan.

## Implementation Approach

The architecture chosen (committed in ADR-002, written in Phase 5):

```
YAML → loader → ir.Builder.Build → *World
                                     │
                       ┌─────────────┼──────────────┐
                       │             │              │
            enrichSyncWaves  resolvePatchTypes   Simulate(world) ← NEW
                       │             │              │
                       └─────────────┼──────────────┘
                                     ▼
                          worldToShenObj  ← extended with 4 new sections
                                     │
                              kl.Call(check-world, …)
                                     │
                          objToDiagnostics → []Diagnostic
```

Five phases. Each one is independently shippable and has its own success criteria. Phase boundaries are chosen so the build is green at every checkpoint and the next phase's tests can be written before its production code.

---

## Phase 1: IR Enrichment for Trajectory Data

### Overview

Pull mount references, ServiceAccount references, RBAC rules, and immutable-field metadata out of `ResourceInfo.Raw` into typed Go structs that downstream phases can consume without re-parsing YAML.

### Changes Required:

#### 1. New types in `pkg/types/types.go`

**File**: `pkg/types/types.go`
**Changes**: Add typed accompaniment structs to the existing IR.

```go
// MountRef records that a workload resource (Pod-bearing kind) mounts a
// ConfigMap or Secret as a volume or projected volume, or as envFrom.
type MountRef struct {
    OwnerKind      string         // Pod, Deployment, StatefulSet, DaemonSet, Job, CronJob
    OwnerName      string
    OwnerNamespace string
    TargetKind     string         // ConfigMap | Secret
    TargetName     string
    TargetNamespace string         // resolves to OwnerNamespace if not set
    MountKind      string         // volume | envFrom | projected
    Optional       bool           // true if the volume/envFrom marks the ref optional
    Source         SourceLocation
}

// SARef records that a workload resource pins to a ServiceAccount.
type SARef struct {
    OwnerKind, OwnerName, OwnerNamespace string
    SAName                               string
    SANamespace                          string // resolves to OwnerNamespace if not set
    Source                               SourceLocation
}

// RBACBinding records a (Cluster)RoleBinding subject → role edge.
type RBACBinding struct {
    BindingKind   string  // RoleBinding | ClusterRoleBinding
    BindingName   string
    BindingNamespace string  // empty for ClusterRoleBinding
    SubjectKind   string  // ServiceAccount | User | Group
    SubjectName   string
    SubjectNamespace string
    RoleKind      string  // Role | ClusterRole
    RoleName      string
    Source        SourceLocation
}

// RBACRule is a single (verbs, resources, apiGroups) entry inside a Role / ClusterRole.
type RBACRule struct {
    OwnerKind      string  // Role | ClusterRole
    OwnerName      string
    OwnerNamespace string  // empty for ClusterRole
    APIGroups      []string
    Resources      []string
    Verbs          []string
    ResourceNames  []string
    Source         SourceLocation
}

// ImmutableField is one entry in the registry of "field paths that must not change
// after create" per (Group, Kind). Populated from a static table; not extracted from YAML.
type ImmutableField struct {
    Group     string
    Kind      string
    FieldPath string  // dotted path, e.g. "spec.clusterIP"
    Reason    string  // human-readable explanation for diagnostics
}
```

Then extend `World`:

```go
type World struct {
    // ... existing fields ...
    MountRefs       []MountRef       `json:"mountRefs,omitempty"`
    SARefs          []SARef          `json:"saRefs,omitempty"`
    RBACBindings    []RBACBinding    `json:"rbacBindings,omitempty"`
    RBACRules       []RBACRule       `json:"rbacRules,omitempty"`
    ImmutableFields []ImmutableField `json:"-"` // populated from registry, not serialized
}
```

#### 2. New file `pkg/ir/trajectory_extract.go`

**File**: `pkg/ir/trajectory_extract.go` (new)
**Changes**: Walk `world.Resources`, extract MountRef/SARef from Pod-bearing kinds, extract RBACBinding/RBACRule from RBAC kinds. All extraction reads `ResourceInfo.Raw`; nothing reads YAML again.

Public surface: one function `EnrichTrajectoryData(w *types.World)` mutating in place. Called from `ir.Builder.Build` after the existing per-document dispatch.

Pod-bearing kinds to handle:
- `Pod` (look at `spec.volumes`, `spec.containers[].envFrom`, `spec.serviceAccountName`)
- `Deployment`, `StatefulSet`, `DaemonSet`, `ReplicaSet`, `Job` (look under `spec.template.spec.…`)
- `CronJob` (look under `spec.jobTemplate.spec.template.spec.…`)

Volume kinds to extract as MountRef:
- `volumes[].configMap` (Optional from `volumes[].configMap.optional`)
- `volumes[].secret` (Optional from `volumes[].secret.optional`)
- `volumes[].projected.sources[].configMap` / `secret` (each as one MountRef)
- `containers[].envFrom[].configMapRef` / `secretRef`
- `initContainers[].envFrom[]` (same)

RBAC kinds to extract from:
- `RoleBinding` / `ClusterRoleBinding` → walk `subjects[]` × `roleRef` → emit one `RBACBinding` per subject
- `Role` / `ClusterRole` → walk `rules[]` → emit one `RBACRule` per rule

#### 3. New file `pkg/ir/immutable_registry.go`

**File**: `pkg/ir/immutable_registry.go` (new)
**Changes**: Hardcoded table of well-known immutable Kubernetes field paths. Initially populated with a conservative set; expansion is a follow-up ticket, not part of this phase.

```go
package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// ImmutableFieldRegistry returns the static catalog of immutable field paths.
// Expand by appending to this slice — one entry per (Group, Kind, FieldPath).
func ImmutableFieldRegistry() []types.ImmutableField {
    return []types.ImmutableField{
        {Group: "", Kind: "Service", FieldPath: "spec.clusterIP",
            Reason: "Service ClusterIP is immutable after create; changing it requires recreate"},
        {Group: "", Kind: "Service", FieldPath: "spec.type",
            Reason: "Service type changes from/to ExternalName are not allowed in-place"},
        {Group: "", Kind: "PersistentVolumeClaim", FieldPath: "spec.storageClassName",
            Reason: "PVC StorageClassName is immutable after create"},
        {Group: "", Kind: "PersistentVolumeClaim", FieldPath: "spec.accessModes",
            Reason: "PVC AccessModes are immutable after create"},
        {Group: "batch", Kind: "Job", FieldPath: "spec.selector",
            Reason: "Job Selector is immutable after create"},
        {Group: "batch", Kind: "Job", FieldPath: "spec.template",
            Reason: "Job Template is immutable after create"},
        {Group: "apps", Kind: "StatefulSet", FieldPath: "spec.serviceName",
            Reason: "StatefulSet ServiceName is immutable after create"},
        {Group: "apps", Kind: "StatefulSet", FieldPath: "spec.volumeClaimTemplates",
            Reason: "StatefulSet VolumeClaimTemplates are immutable after create"},
    }
}
```

`EnrichTrajectoryData` populates `world.ImmutableFields = ImmutableFieldRegistry()` so the registry is always available downstream.

#### 4. Wire into `ir.Builder.Build`

**File**: `pkg/ir/builder.go`
**Changes**: After the per-document loop completes and before `Build` returns, call `EnrichTrajectoryData(w)`. One line. Documented as "extracts cross-resource references for trajectory analysis."

### Success Criteria:

#### Automated Verification:
- [ ] `go build ./...` succeeds
- [ ] New tests pass: `go test ./pkg/ir/... -run TestEnrichTrajectoryData`
- [ ] Existing tests still pass: `go test ./...`
- [ ] `go vet ./...` clean

#### Manual Verification:
- [ ] None for this phase — it's pure data extraction with unit-test coverage.

**Implementation Note**: After completing this phase and all automated verification passes, pause for confirmation before proceeding.

---

## Phase 2: Trajectory Simulator

### Overview

Build `pkg/trajectory/` — a self-contained Go package that takes a `*types.World` and produces a deterministic sequence of `Step`s, one per Argo sync wave, with a `Delta` describing what's created / updated / deleted at that step relative to the prior step's synthesized cluster state.

### Changes Required:

#### 1. New package `pkg/trajectory/`

**File**: `pkg/trajectory/trajectory.go` (new)
**Changes**: Public API.

```go
// Package trajectory simulates an Argo CD sync trajectory step-by-step.
//
// The simulator does NOT execute renderers. It operates on the resources
// already present in the World. Multi-source / Helm / Kustomize Applications
// see their `Sources` field reflected, but no actual templating happens.
package trajectory

import "github.com/pyrex41/cross-validate-/pkg/types"

// Step is a snapshot of the simulated cluster state at one wave.
type Step struct {
    AppName  string         // which Argo App this step belongs to
    Wave     int            // sync wave number; -inf < … < +inf
    Delta    Delta          // what changed in this step
    State    State          // the synthesized cluster state AT this step
}

// Delta is the set of resource keys that changed in a given step.
type Delta struct {
    Created []ResourceKey
    Updated []ResourceKey
    Deleted []ResourceKey
}

// State is the synthesized cluster contents AT a step.
// Indexed by stable key; values are the parsed resource (no rendering).
type State struct {
    Resources map[ResourceKey]types.ResourceInfo
}

// ResourceKey is the canonical (kind, namespace, name) tuple used as a
// stable handle for resources across steps.
type ResourceKey struct {
    APIVersion string
    Kind       string
    Namespace  string
    Name       string
}

// Simulate produces the full trajectory for a World.
// Returns one slice of Steps per Argo Application, ordered by App name then wave.
// If an App has no sync waves declared, all of its resources get wave 0.
func Simulate(w *types.World) []Step { /* ... */ }
```

#### 2. Simulation algorithm

**File**: `pkg/trajectory/simulate.go` (new)

For each `ArgoApplication` in `w.ArgoApps`:

1. Build a per-app resource set: every resource in `w.Resources` that the app *manages* (in this phase, scope-by-namespace from `app.Destination.Namespace`; if Destination is empty, all resources in the world). Document this as a known limitation; renderer integration is the proper fix.
2. Group those resources by their `wave` (from `app.SyncWaves` annotations, defaulting to 0).
3. Sort waves ascending. Group "PruneLast=true"-marked deletions to the highest wave + 1.
4. For each wave, in order:
   - Compute the previous wave's `State`.
   - Compute this wave's resource set.
   - `Created` = in-this-wave − in-previous-wave.
   - `Deleted` = in-previous-wave − in-this-wave (only if `app.SyncPolicy.SyncOptions.Prune` or `Automated.Prune` is true; otherwise empty).
   - `Updated` = present in both but `ResourceInfo.Raw` differs (deep-equal). In Phase 2 there are no real updates because we only have one snapshot per resource — keep the field for Phase 3 future use, leave empty here.
   - Emit `Step{AppName, Wave, Delta, State}`.
5. Record output trajectories per app for Phase 3 to serialize.

#### 3. Tests

**File**: `pkg/trajectory/trajectory_test.go` (new)

Three deterministic golden tests:
- Empty world → empty trajectory.
- `testdata/fixtures/wave-ordering/app.yaml` → produces N steps in expected order.
- A new `testdata/fixtures/dangling-mount/` (created in Phase 4) → produces a step where a ConfigMap is in `Deleted`.

### Success Criteria:

#### Automated Verification:
- [ ] `go build ./...` succeeds
- [ ] `go test ./pkg/trajectory/...` passes
- [ ] All existing tests still pass: `go test ./...`
- [ ] No new lint warnings: `go vet ./...`

#### Manual Verification:
- [ ] None — pure deterministic logic with golden tests.

**Implementation Note**: After completing this phase and all automated verification passes, pause for confirmation before proceeding.

---

## Phase 3: Bridge Extension — World → Shen Trajectory Sections

### Overview

Extend `worldToShenObj` to emit four new sections so the Shen kernel can pattern-match on them: `(trajectory …)`, `(mount-refs …)`, `(rbac-bindings …)`, `(rbac-rules …)`, `(immutable-fields …)`. Also call the trajectory simulator from `Check` before serialization.

### Changes Required:

#### 1. New conversion helpers in `pkg/checker/bridge.go`

**File**: `pkg/checker/bridge.go`
**Changes**: Add `mountRefToObj`, `saRefToObj`, `rbacBindingToObj`, `rbacRuleToObj`, `immutableFieldToObj`, `stepToObj`, `deltaToObj`, `trajectoryToObj`. Each follows the existing `*-fact` pattern.

```go
func mountRefToObj(m types.MountRef) kl.Obj {
    return makeList([]kl.Obj{
        sym("mount-ref-fact"),
        str(m.OwnerKind), str(m.OwnerName), str(m.OwnerNamespace),
        str(m.TargetKind), str(m.TargetName), str(m.TargetNamespace),
        str(m.MountKind), boolean(m.Optional),
        sourceToObj(m.Source),
    })
}
// ... similar for the others ...
```

The trajectory section is the only nested one:

```go
func trajectoryToObj(steps []trajectory.Step) kl.Obj {
    var stepObjs []kl.Obj
    for _, s := range steps {
        stepObjs = append(stepObjs, stepToObj(s))
    }
    return section("trajectory", stepObjs)
}

func stepToObj(s trajectory.Step) kl.Obj {
    return makeList([]kl.Obj{
        sym("step"),
        str(s.AppName), num(s.Wave),
        deltaToObj(s.Delta),
        stateKeysToObj(s.State),  // emit just the keys, not full Resources — those are already in the world
    })
}

func deltaToObj(d trajectory.Delta) kl.Obj {
    return makeList([]kl.Obj{
        sym("delta"),
        section("created", resourceKeyObjs(d.Created)),
        section("updated", resourceKeyObjs(d.Updated)),
        section("deleted", resourceKeyObjs(d.Deleted)),
    })
}
```

#### 2. Wire the simulator into `Check`

**File**: `pkg/checker/bridge.go`
**Changes**: In `CheckWithObligations`, between `resolvePatchTypes(w)` and `worldToShenObj`, add:

```go
trajectories := trajectory.Simulate(w)
```

Pass `trajectories` into `worldToShenObj` (extend its signature).

#### 3. Add the new sections to `worldToShenObj`

**File**: `pkg/checker/bridge.go`
**Changes**: Append to the `sections` slice ([bridge.go:418](pkg/checker/bridge.go)):

```go
sections := []kl.Obj{
    sym("world"),
    // ... existing sections ...
    section("mount-refs",       mountRefObjs),
    section("rbac-bindings",    rbacBindingObjs),
    section("rbac-rules",       rbacRuleObjs),
    section("immutable-fields", immutableFieldObjs),
    trajectoryToObj(trajectories),
}
```

#### 4. Update `obligationRefForCode` for new codes

**File**: `pkg/checker/bridge.go`
**Changes**: Extend the table at [bridge.go:600](pkg/checker/bridge.go) with three new entries:

```go
"XPC012": {"F", "no-dangling-mount"},
"XPC013": {"F", "no-immutable-change"},
"XPC014": {"F", "no-rbac-regression"},
```

(R6c re-uses XPC006; no new code needed.)

### Success Criteria:

#### Automated Verification:
- [ ] `go build ./...` succeeds
- [ ] `go test ./pkg/checker/...` passes — including a new `TestBridge_TrajectorySerialization` that builds a small fixture, runs `worldToShenObj`, and confirms the new sections are present in the output (use `kl.ObjString` for the assertion).
- [ ] All existing tests still pass: `go test ./...`
- [ ] `xpc check testdata/fixtures/basic` produces the same output as before: `go run ./cmd/xpc check testdata/fixtures/basic`

#### Manual Verification:
- [ ] None — pre-rule serialization additions; behavior change comes in Phase 4.

**Implementation Note**: After completing this phase and all automated verification passes, pause for confirmation before proceeding.

---

## Phase 4: Shen-Side Trajectory Rules

### Overview

Add four Shen rule files implementing the four target invariants. Update `kernel/check.shen` to load and dispatch them. Add per-rule fixtures.

### Changes Required:

#### 1. New rule file: `kernel/r6c-provider-wave.shen`

**File**: `kernel/r6c-provider-wave.shen` (new)
**Changes**: The missing R6c. For every Provider whose wave is declared in any app, and every managed-resource of a CRD that Provider serves, assert `wave(Provider) < wave(MR)`. Pattern follows `check-r6a-for-xrd` in [`kernel/r6-wave-ordering.shen:30`](kernel/r6-wave-ordering.shen).

The kernel emits XPC006 (same code as R6) with a sub-claim message — that matches the existing R6 absorption story; no new XPC code needed.

Wire into `kernel/check.shen`:

```
(load "r6c-provider-wave.shen")
...
R6c (check-r6c ArgoApps Providers Resources CRDs)
...
(append R6 (append R6c (append R7 ... )))
```

#### 2. New rule file: `kernel/r12-no-dangling-mount.shen`

**File**: `kernel/r12-no-dangling-mount.shen` (new)
**Changes**: For every `(step …)` in `(trajectory …)`:

- For every `(mount-ref-fact OwnerK OwnerN OwnerNs TgtK TgtN TgtNs MountK Opt Src)`:
  - If `(TgtK TgtN TgtNs)` appears in this step's `Deleted` set AND the owner is in this or any later step's `State` AND `Opt` is `false`: emit XPC012.
  - Otherwise no judgment.

```shen
\* r12-no-dangling-mount.shen — XPC012 *\

(define check-r12-step
  {(list A) --> (list (list A)) --> (list judgment)}
  [step AppName Wave [delta _ _ Deleted] _] MountRefs ->
    (flatten (map (/. M (check-r12-mount AppName Wave Deleted M)) MountRefs))
  _ _ -> [])

(define check-r12-mount
  ...
  (if (and (not Optional)
           (mount-target-in-deleted? Tgt Deleted))
      [(make-error "XPC012" Src
        (cn "ConfigMap/Secret " (cn TgtName " is pruned mid-sync but still mounted"))
        ...
        ...)]
      []))

(define check-r12
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  Trajectory MountRefs ->
    (flatten (map (/. S (check-r12-step S MountRefs)) Trajectory)))
```

#### 3. New rule file: `kernel/r13-no-immutable-change.shen`

**File**: `kernel/r13-no-immutable-change.shen` (new)
**Changes**: For every `(step …)` whose `Updated` set contains a resource key, look up that resource's `(Group, Kind)` against the `(immutable-fields …)` registry. If the registry has an entry for any `FieldPath` and the previous-step Raw vs current-step Raw differs at that path, emit XPC013.

In Phase 4, since `Delta.Updated` is always empty (Phase 2 stub), R13 will produce zero diagnostics on real input — but the rule, the load wiring, and the test fixture must all exist so Phase 5 / future tickets can populate `Updated` and have R13 immediately work. Document this gap inline.

#### 4. New rule file: `kernel/r14-no-rbac-regression.shen`

**File**: `kernel/r14-no-rbac-regression.shen` (new)
**Changes**: For every `(step …)`:

- For every Pod-bearing resource in the step's `State` that has a `(sa-ref-fact …)`:
  - Compute the resolved permission set as `union of RBACRules where some RBACBinding pins the SA to the Role`.
  - For each later step where any `RBACBinding` or `RBACRule` for that SA is in `Deleted`:
    - Emit XPC014 with the regression details.

This is the most complex of the three; allow this rule file to be the longest. Closure-style helpers acceptable.

#### 5. Update `kernel/check.shen`

**File**: `kernel/check.shen`
**Changes**: Add four `(load "rN.shen")` lines, four entries in the `let` binding chain, four entries in the closing `append` chain ([check.shen:65-79](kernel/check.shen)).

Also add the new `extract-section` calls for `mount-refs`, `rbac-bindings`, `rbac-rules`, `immutable-fields`, `trajectory` in the `let` block ([check.shen:54-63](kernel/check.shen)).

#### 6. New test fixtures

**Files**:
- `testdata/fixtures/dangling-mount/app.yaml` (new) — exercises XPC012 trigger
- `testdata/fixtures/rbac-regression/app.yaml` (new) — exercises XPC014 trigger
- `testdata/fixtures/provider-wave/app.yaml` (new) — exercises R6c sub-claim of XPC006

Each fixture is a single YAML file containing the smallest valid set of resources that triggers exactly one diagnostic from the new rule. Mirror the structure of `testdata/fixtures/wave-ordering/app.yaml`.

#### 7. New integration tests in `pkg/checker/check_test.go`

**File**: `pkg/checker/check_test.go`
**Changes**: Add `TestR6c_ProviderWave`, `TestR12_DanglingMount`, `TestR14_RbacRegression`. Each follows the `loadFixture` + `checkFixture` + `findDiagByCode` pattern already in the file ([check_test.go:11-40](pkg/checker/check_test.go)).

For R13, add `TestR13_RuleLoaded` that confirms the rule is loaded and produces zero diagnostics on the basic fixture (the actual semantic test waits for Phase 2 update-detection to be implemented in a follow-up ticket).

### Success Criteria:

#### Automated Verification:
- [ ] `go build ./...` succeeds
- [ ] `go test ./pkg/checker/... -run TestR6c_ProviderWave` passes
- [ ] `go test ./pkg/checker/... -run TestR12_DanglingMount` passes
- [ ] `go test ./pkg/checker/... -run TestR13_RuleLoaded` passes
- [ ] `go test ./pkg/checker/... -run TestR14_RbacRegression` passes
- [ ] All existing tests still pass: `go test ./...`
- [ ] `xpc check testdata/fixtures/basic` produces no XPC012/013/014: `go run ./cmd/xpc check testdata/fixtures/basic`
- [ ] `xpc check testdata/fixtures/wave-ordering` produces the same output as before (no regression).

#### Manual Verification:
- [ ] `go run ./cmd/xpc check testdata/fixtures/dangling-mount` prints a human-readable XPC012 diagnostic with the right ConfigMap/Pod names and a non-zero exit code.
- [ ] `go run ./cmd/xpc check testdata/fixtures/rbac-regression` prints a human-readable XPC014 diagnostic naming the SA and the role/binding that disappeared.
- [ ] `go run ./cmd/xpc check testdata/fixtures/provider-wave` prints an XPC006 with the new R6c sub-claim text.

**Implementation Note**: After completing this phase and all automated verification passes — including manual fixture runs — pause for confirmation before proceeding.

---

## Phase 5: ADR-002 + Documentation

### Overview

Make the architectural commitment explicit so the next pivot has to argue against an ADR rather than just reorganize. Update the obligations doc to reflect what's now actually implemented.

### Changes Required:

#### 1. New ADR: `docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md`

**File**: `docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md` (new)
**Changes**: One ADR. Sections: Status (Accepted), Context (recap of the three pivots and why we're committing now), Decision (Go owns IR + simulation; Shen owns rules; the World→Shen contract is the canonical spec; ADR-001 §3 is superseded), Consequences (no Go-side rule generators; new invariants land as Shen rules over enriched-World sections; trajectory simulation is intentionally Go-only because Shen has no good story for stateful step-by-step simulation).

Length: 1.5–2 pages. Mirror the structure of [`docs/adr/001-bounded-obligation-taxonomy.md`](docs/adr/001-bounded-obligation-taxonomy.md).

Explicitly mark "supersedes ADR-001 §3" in the header.

#### 2. Update `docs/obligations.md`

**File**: `docs/obligations.md`
**Changes**: Under category F, add the four implemented generators:
- `trajectory-wave-order` (mark XPC006 as also covering R6c)
- `no-dangling-mount` → XPC012
- `no-immutable-change` → XPC013 (mark as "framework present, update detection pending follow-up")
- `no-rbac-regression` → XPC014

Update the "Error codes" section to list XPC012/013/014.

#### 3. Update `docs/adr/001-bounded-obligation-taxonomy.md`

**File**: `docs/adr/001-bounded-obligation-taxonomy.md`
**Changes**: Add a "Status" addendum at the top: `**Status**: Accepted; §3 superseded by ADR-002 (2026-04-17).` No other text changes — historical record stays intact.

### Success Criteria:

#### Automated Verification:
- [ ] All tests still pass: `go test ./...`
- [ ] `go build ./...` succeeds
- [ ] No code changes in this phase.

#### Manual Verification:
- [ ] `docs/adr/002-…md` exists, is internally consistent, and explicitly references the supersession of ADR-001 §3.
- [ ] `docs/obligations.md` reflects the new XPC012/013/014 codes and the F-category generators.
- [ ] `docs/adr/001-…md` has the supersession note.

---

## Testing Strategy

### Unit Tests:

- `pkg/ir/trajectory_extract_test.go` — golden tests for MountRef extraction from each pod-bearing kind, RBACBinding/RBACRule extraction from each RBAC kind, optional-volume handling, namespace defaulting.
- `pkg/trajectory/trajectory_test.go` — golden trajectories for empty world, single-app single-wave, single-app multi-wave, prune-on/off, multi-app interleaving.
- `pkg/checker/bridge_serialization_test.go` (new) — round-trip a small World through `worldToShenObj` and assert the new sections are present in the serialized form (use `kl.ObjString`).

### Integration Tests:

- `pkg/checker/check_test.go` — one `TestR*` per new rule using the new fixtures.

### Manual Testing Steps:

1. `go run ./cmd/xpc check testdata/fixtures/dangling-mount` — verify human output names the right ConfigMap and the right Pod, and that the diagnostic carries `Obligation.Category=F, Generator=no-dangling-mount`.
2. `go run ./cmd/xpc check testdata/fixtures/rbac-regression` — verify the SA name and Role/RoleBinding names are in the message.
3. `go run ./cmd/xpc check testdata/fixtures/provider-wave` — verify the diagnostic mentions the Provider name and the MR wave.
4. `go run ./cmd/xpc check --proof check.xpcproof testdata/fixtures/dangling-mount` then `go run ./cmd/xpc proof show check.xpcproof` — confirm the new rule's leaf appears in the Merkle tree.
5. Re-run `go run ./cmd/xpc check testdata/fixtures/basic` — confirm no false positives from the new rules on a clean fixture.

## Performance Considerations

- The trajectory simulator is `O(apps × waves × resources)`. For typical clusters (≤ 50 apps, ≤ 10 waves, ≤ 1000 resources) this is fine. No premature optimization in this plan.
- The Shen kernel iterates per-step over per-resource lists. Same `O(apps × waves × resources)` for R12, multiplied by the mount-ref count for the inner loop. If a future user reports slowness, the natural fix is to index `Deleted` by `(Kind, Name, Namespace)` Go-side and feed an indexed structure to Shen — that's a Phase 3 follow-up, not in scope here.
- `worldToShenObj` already sorts every input slice for determinism. The new sections inherit this — sort `MountRefs`, `RBACBindings`, `RBACRules` by their natural keys before serialization.

## Migration Notes

- Snapshots written by the existing `xpc snapshot` command do not carry the new fields. Loading an old snapshot just produces an empty `MountRefs` / `RBACBindings` / `RBACRules` / trajectory — the rules will emit zero diagnostics, which is the safe default. No snapshot version bump needed.
- `audit.Generate` continues to use the legacy `XPC001..XPC011` `RuleSubtrees` keying. New codes XPC012/013/014 will not appear in `RuleSubtrees` — only in `ResourceSubtrees`. This is a known inconsistency carried forward from PR #3 and is not addressed in this plan (separate ticket: "extend audit.Generate to use ObligationIDs").
- The `Diagnostic.Obligation *ObligationRef` field is populated for the new codes via the table at `bridge.go:600`. Downstream consumers (SARIF, LSP, agent format) that read `d.Code` see new codes appear; consumers that read `d.Obligation` see the new (Category F, Generator …) pairs.

## References

- Vision recap and rule-by-rule audit: `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md`
- Prior project review: `thoughts/shared/research/2026-04-16-project-review-pr3.md`
- ADR being partially superseded: `docs/adr/001-bounded-obligation-taxonomy.md`
- Obligation taxonomy reference: `docs/obligations.md`
- Existing R6 (the only real trajectory invariant before this plan): `kernel/r6-wave-ordering.shen`
- Existing Go enrichment pattern: `pkg/checker/bridge.go:160` (`enrichSyncWaves`), `pkg/checker/bridge.go:214` (`resolvePatchTypes`)
- World → Shen serialization to extend: `pkg/checker/bridge.go:301` (`worldToShenObj`)
- Top-level Shen entry to extend: `kernel/check.shen:51` (`check-world`)
- Test fixture pattern to mirror: `testdata/fixtures/wave-ordering/app.yaml`
- Bridge integration test pattern to mirror: `pkg/checker/check_test.go:11` (`loadFixture`, `checkFixture`, `findDiagByCode`)
