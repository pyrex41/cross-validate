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
- `selector-needs-ignore-diff` -- selector field's resolved path suppressed by ignoreDifferences (**implemented** — R16 / `XPC.E.selector-needs-ignore-diff`, scalar-path first pass; array-indexed paths deferred to follow-up)
- `late-init-needs-ignore-diff` -- provider late-init fields suppressed by ignoreDifferences, managementPolicies omitting LateInitialize, or `omitLateInitialize` (**implemented** — R21 / `XPC.E.late-init-needs-ignore-diff`, scalar-path first pass; registry seeded from fg-manifold MRs !1048, !893, !1502)
- `appset-finalizer-without-preserve` -- ApplicationSet bakes `resources-finalizer.argocd.argoproj.io` into its template without setting `spec.syncPolicy.preserveResourcesOnDeletion: true` on the AppSet (**implemented** — R24 / `XPC.E.appset-finalizer-without-preserve`, the static floor for fg-synapse INC-6)
- `prod-appset-autosync` -- ApplicationSet whose name matches a prod pattern (`-prod`, `prod-`) enables `spec.template.spec.syncPolicy.automated` (**implemented** — R25 / `XPC.E.prod-appset-autosync`, static floor pairing with R24/R23; patterns hardcoded pending a kernel config file follow-up)

**Absorbs**: (new -- no legacy rule)

### F: Trajectory-invariant obligations

**Scope**: `(invariant x sync-step)` per Application.

Simulate the sync trajectory (wave ordering, hook phases, PruneLast) and
verify temporal invariants hold at every step.

**Generators**:
- `no-dangling-mount` → `XPC012` — no Pod references a pruned ConfigMap/Secret mid-sync *(implemented)*
- `no-immutable-change` → `XPC013` — **Retired** — subsumed by R27 / `XPC.P.immutable-change` (plan-mode, variant-axis). The trajectory-axis form required a simulator refactor with small payoff; the natural data shape for immutable-change is two Worlds, which is what R27 consumes.
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
- `helm-renders` -- Helm template succeeds (**implemented** — R18 / `XPC.H.helm-renders`; warning when helm binary absent, error on template/timeout failure)
- `kustomize-renders` -- Kustomize build succeeds (**implemented** — R18 / `XPC.H.kustomize-renders`; same absent-binary/timeout/failure severity ladder as helm-renders)
- `values-well-typed` -- value sources match chart schema (**implemented** — R19 / `XPC.H.values-well-typed`, reuses S3 `ValidateManifest` walker)
- `render-deterministic` -- same input produces same output (**implemented** — R20 / `XPC.H.render-deterministic`; warning-only, double-render byte-compare)

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

### S: Safety / state-preservation obligations

**Scope**: `(kind x deletionPolicy x bypass)` per Crossplane managed resource.

Catch configuration that would allow Crossplane's default destructive behavior
to reach "real" external state. State-bearing kinds (Aurora, DocDB, MySQL, KMS,
S3, VPC) default to `deletionPolicy: Delete` — the CR's deletion runs
`DROP DATABASE` / `DeleteCluster` / `DeleteKey` against the backing system.
The invariant: every such resource declares `spec.deletionPolicy: Orphan`,
unless an explicit bypass annotation opts it out.

**Generators**:
- `crossplane-state-needs-orphan` -- state-bearing Crossplane MR missing `deletionPolicy: Orphan` (**implemented** — R23 / `XPC.S.crossplane-state-needs-orphan`, kind allowlist mirrors fg-manifold's `crossplane-state-require-orphan` VAP; bypass `xpc.io/allow-delete` primary plus user-registered aliases via xpc.yaml `bypass-annotations.allow-delete.aliases`; default name carve-out `alb-logs`, extensible via xpc.yaml `name-carveouts.crossplane-state-needs-orphan`)

**Absorbs**: (new — static floor for fg-synapse INC-6)

### L: Deprecation/calendar obligations

**Scope**: `(API x k8s version x date)`.

Forward-looking warnings: the configuration works today but will break at a
known future date. API version removals, provider version deprecations, CRD
versions marked not-served.

**Generators**:
- `api-deprecation-calendar` -- deprecated APIs warn in advance (absorbs R11)

**Absorbs**: R11

### M: Convergence / steady-state obligations

**Scope**: `(managed-resource x normalization-rule)`.

Catch configuration whose control loop can never reach a fixed point. Some
upjet-generated provider fields are canonicalized by the cloud on read-back
(e.g. ECS `Service.spec.forProvider.taskDefinition` is echoed as
`family:revision`). Writing a non-canonical literal makes desired `!=` observed
forever: upjet re-issues the external Update on every reconcile and the status
write conflicts with the poll loop — a self-sustaining **reconcile storm**
(fg-manifold MR !2232). This is distinct from category E / late-init
(Argo-vs-Crossplane): here the fight is **upjet-vs-cloud**, so an Argo
`ignoreDifferences` entry does nothing. The invariant: every registered
forProvider field is a **fixed point** of the provider's read-back, OR the
field is excluded from the external Update via `managementPolicies`.

The category is checked at three escalating tiers, all under the same code
family, so coverage degrades gracefully with how much context is available:

- **Tier 1 — static (resource walk).** A registry of normalization-prone
  `(group, kind, field, detector)` rows (`pkg/ir/canonical_form_registry.go`,
  seeded from storm-fixing MRs exactly like the R21 late-init registry). Every
  concrete managed resource — a raw committed MR or a rendered one — is checked;
  a non-canonical value that upjet would actually push fires at **error**.
- **Tier 2 — heuristic (composition template scan).** In a GitOps repo the
  resource is produced by a Composition rendered at runtime, so it is absent
  from the resource set and has no live status. A textual scan of the unrendered
  go-templating body flags registered fields assigned to hardcoded non-canonical
  ARN literals at **warn** (a value computed entirely inside `{{ ... }}`, or one
  that mentions `atProvider`, is assumed resolved and not flagged).
- **Tier 3 — dynamic (live snapshot).** On a `--from-cluster` snapshot merged
  into the World, compare every `spec.forProvider.*` leaf against the matching
  `status.atProvider.*` leaf. A persistent divergence is the storm fingerprint
  captured from reality. A registered field is conclusive from one snapshot
  (**error**); the unregistered long tail is **warn** (confirm with a second
  snapshot, since one cannot distinguish a storm from a resource mid-update).

**Generators**:
- `forprovider-canonical-form` -- registered forProvider field holds a
  non-canonical literal, not excluded from Update (**implemented** — R31 /
  `XPC.M.forprovider-canonical-form`; Tier-1 resource walk at error, Tier-2
  composition-template scan at warn; registry seeded from fg-manifold MR !2232;
  bypass `xpc.io/allow-noncanonical`)
- `observed-desired-fixed-point` -- live `forProvider` leaf diverges from its
  `status.atProvider` counterpart (**implemented** — R32 /
  `XPC.M.observed-desired-fixed-point`; Tier-3, registry-aware severity; only
  fires on status-bearing resources from a `--from-cluster` snapshot)
- `duplicate-env-key` -- a go-templating Composition emits the same ECS
  `containerDefinitions` environment variable name more than once. AWS dedupes
  the env array on registration, so the desired task def never matches the stored
  one → a permanent diff on the *immutable* `container_definitions` → upjet
  hard-fails with `ReconcileError`. (**implemented** — R33 /
  `XPC.M.duplicate-env-key`, Tier-2 heuristic at warn; scoped to single-container
  compositions to avoid cross-container false positives; fg-manifold MR !2246)

**Absorbs**: (new — static + dynamic floor for the Crossplane reconcile-storm
failure mode; fg-manifold MR !2232)

## Error codes

Legacy codes `XPC001`-`XPC014` remain as aliases. `XPC012`, `XPC013`, and
`XPC014` are the trajectory-invariant codes added in
[ADR-002](adr/002-shen-as-canonical-spec-and-trajectory-simulator.md):

- `XPC012` — `no-dangling-mount`
- `XPC013` — `no-immutable-change` **Retired** (P4.d, 2026-04-23) — subsumed by R27 / `XPC.P.immutable-change` (plan-mode, variant-axis)
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
