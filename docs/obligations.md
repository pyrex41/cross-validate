# Obligation Taxonomy

This document defines the 12 obligation categories that xpc checks against.
Every diagnostic xpc produces traces back to a generator in one of these
categories. See [ADR-001](adr/001-bounded-obligation-taxonomy.md) for the
rationale.

## How it works

1. Each category has one or more **generators**.
2. A generator takes `(ClusterContext, World)` and emits a list of **obligations**.
3. Each obligation has a **discharge function** that proves or falsifies it.
4. A failing obligation produces a **diagnostic** with full provenance.

The obligation count is bounded by the input size times the taxonomy depth.
A typical cluster might produce 500-3000 obligations per run. All are
discharged automatically.

## Categories

### A: Schema obligations

**Scope**: `(CRD x field)` over the cluster context.

For every CRD in the cluster context and every field in its OpenAPI schema,
enumerate structural obligations: required fields are present, values match
declared types, enums are valid, formats are correct, immutable fields are
unchanged post-create.

**Generators**:
- `patch-source-type` -- patch source field resolves in XRD schema (absorbs part of R5)
- `patch-target-type` -- patch target field resolves in CRD schema (absorbs part of R5)
- `crossplane-machinery-placement` -- v1/v2 field placement (absorbs part of R8)

**Absorbs**: R5 (partial), R8 (partial)

### B: Reference-resolution obligations

**Scope**: Cross-references in the IR.

For every cross-reference in the World (Composition -> XRD, Pipeline step ->
Function, patch source -> field, patch target -> field), verify the referent
exists, is the right kind, and is version-compatible.

**Generators**:
- `comp-xrd-ref` -- Composition compositeTypeRef resolves to referenceable XRD (absorbs R3)
- `pipeline-fn-ref` -- Pipeline step functionRef resolves and input version matches (absorbs R4)
- `patch-compat` -- patch source/target types are compatible after transforms (absorbs part of R5)

**Absorbs**: R3, R4, R5 (partial)

### C: Version-coherence obligations

**Scope**: `(CRD x version)` over the cluster context.

For every CRD and XRD, verify version metadata is consistent: exactly one
storage version, all versions served, at least one referenceable version (XRDs).

**Generators**:
- `version-coherence` -- storage/served/referenceable checks (absorbs R1)
- `crossplane-machinery-version` -- v2 XRD version constraints (absorbs part of R8)

**Absorbs**: R1, R8 (partial)

### D: AppProject-constraint obligations

**Scope**: `(Application x AppProject)`.

For every Argo CD Application, verify it respects its AppProject's constraints:
source repo is in the allow-list, destination (server, namespace) is permitted,
managed resource kinds are whitelisted, sync windows are respected.

**Generators**:
- `source-repo-allowed` -- Application source repo in project sourceRepos
- `destination-allowed` -- Application destination in project destinations
- `kind-whitelisted` -- managed kinds pass project whitelist/blacklist (**implemented** — R15 / `XPC.D.kind-whitelisted`, direct-manifest pass; Helm/Kustomize rendering deferred to S4/S5)
- `sync-window-permitted` -- sync is allowed in current time window

**Absorbs**: (new -- no legacy rule)

### E: Sync-option interaction obligations

**Scope**: `(syncOption x resource-kind)` per Application.

For every sync option enabled on an Application, and every resource kind it
manages, verify the option's semantics are safe for that kind. Replace on a
Service changes clusterIP; ServerSideApply may conflict with field managers;
Prune without targets is a no-op.

**Generators**:
- `replace-immutable-safety` -- Replace doesn't touch immutable fields
- `ssa-field-manager-conflict` -- SSA field managers don't overlap
- `prune-target-exists` -- Prune has something to prune
- `createnamespace-not-colliding` -- CreateNamespace doesn't conflict

**Absorbs**: (new -- no legacy rule)

### F: Trajectory-invariant obligations

**Scope**: `(invariant x sync-step)` per Application.

Simulate the sync trajectory (wave ordering, hook phases, PruneLast) and
verify temporal invariants hold at every step.

**Generators**:
- `no-dangling-mount` → `XPC012` — no Pod references a pruned ConfigMap/Secret mid-sync *(implemented)*
- `no-immutable-change` → `XPC013` — no update touches an immutable field *(framework present; update detection pending follow-up — the simulator's `Delta.Updated` is empty until a multi-snapshot extension lands)*
- `no-rbac-regression` → `XPC014` — ServiceAccount permissions hold across steps *(implemented)*
- `trajectory-wave-order` → `XPC006` — dependency ordering, including R6a (XRD<XR), R6b (Function<Composition), R6c (Provider<MR), R6d (Composition≤XR) *(absorbs R6)*
- `trajectory-bootstrap` → `XPC009` — required resources exist at step 0 *(absorbs R9)*

**Absorbs**: R6, R9

### G: Cross-Application obligations

**Scope**: `(rendered resource x peer set)`.

Over the union of rendered manifests from all Applications, detect resource-key
collisions and label-tracking conflicts.

**Generators**:
- `no-duplicate-ownership` -- no two Applications manage the same resource
- `cross-app-label-tracking` -- Argo label tracking vs Crossplane propagation (absorbs R7)
- `no-namespace-overlap` -- namespace partitioning

**Absorbs**: R7

### H: Rendering obligations

**Scope**: `(Application x source)`.

For every Application source, verify the renderer is available, rendering
succeeds, and the output is well-formed YAML with valid apiVersion/kind.

**Generators**:
- `helm-renders` -- Helm template succeeds
- `kustomize-renders` -- Kustomize build succeeds
- `values-well-typed` -- value sources match chart schema
- `render-deterministic` -- same input produces same output

**Absorbs**: (new -- no legacy rule)

### I: Provider-capability obligations

**Scope**: `(managed-resource x provider-version)`.

For every managed resource, verify the fields it uses are available in the
installed provider version's CRD. Catches "field added in v0.40 but you're
running v0.38."

**Generators**:
- `field-available-in-version` -- MR field exists in provider CRD
- `field-not-deprecated` -- MR field not deprecated in installed version
- `controller-healthy` -- provider controller is healthy before MR creation

**Absorbs**: (new -- no legacy rule)

### J: Conversion-cost obligations

**Scope**: `(CRD x webhook)`.

For every resource written at a non-storage version where the CRD uses webhook
conversion, flag the conversion cost. This is a cost warning, not a type error
-- it doesn't block the sync but warns about latency and reliability risks.

**Generators**:
- `conversion-cost-opt-in` -- webhook conversion requires annotation ack (absorbs R2)

**Absorbs**: R2

### K: Secret-flow obligations

**Scope**: `(field x taint lattice)` per rendered manifest.

Information-flow typing for secret material. Marks fields as tainted at the
schema layer (connection details, credentials), propagates through patches
and pipelines, errors if tainted values reach untainted sinks.

**Generators**:
- `secret-source-sink` -- secret material flows only to secret sinks (absorbs R10)

**Absorbs**: R10

### L: Deprecation/calendar obligations

**Scope**: `(API x k8s version x date)`.

Forward-looking warnings: the configuration works today but will break at a
known future date. API version removals, provider version deprecations, CRD
versions marked not-served.

**Generators**:
- `api-deprecation-calendar` -- deprecated APIs warn in advance (absorbs R11)

**Absorbs**: R11

## Error codes

Legacy codes `XPC001`-`XPC014` remain as aliases. `XPC012`, `XPC013`, and
`XPC014` are the trajectory-invariant codes added in
[ADR-002](adr/002-shen-as-canonical-spec-and-trajectory-simulator.md):

- `XPC012` — `no-dangling-mount`
- `XPC013` — `no-immutable-change` (framework-only until update detection lands)
- `XPC014` — `no-rbac-regression`

New obligations use structured codes:

```
XPC.<Category>.<Generator>[.<Instance>]
```

Examples:
- `XPC.B.comp-xrd-ref.billing-api` -- Composition billing-api's XRD reference
- `XPC.F.no-dangling-mount.wave-2.inventory-config` -- ConfigMap deleted mid-sync
- `XPC.D.source-repo-allowed.payments-api` -- repo not in project allow-list

The `xpc explain` command documents at the **generator** level, not the
instance level. `xpc explain XPC003` resolves to the `comp-xrd-ref` generator.

## Adding a new check

1. Identify which category it belongs to.
2. If it fits an existing generator, add the obligation to that generator.
3. If it needs a new generator within an existing category, add the generator
   and register it.
4. If it doesn't fit any category, write an ADR proposing a 13th category.
   This should be rare and corresponds to modeling a genuinely new failure mode.
