# Rule Catalog

This page documents the invariants enforced by `xpc check` and `xpc plan`.

Each rule is described in terms of:

- **Inputs**: facts the rule consumes after YAML/rendering/enrichment.
- **Invariant**: the condition that must hold.
- **Failure mode**: what can happen if the invariant is violated.
- **Usual fix**: the change a manifest author normally makes.

The implementation may precompute joins in Go for performance. The invariant
statement is the stable contract.

## Static Check Rules

### R1: CRD/XRD Version Coherence

**Code**: `XPC001`

**Inputs**: CRD facts and XRD facts, including versions, `served`, `storage`,
and `referenceable`.

**Invariant**:

- Every CRD has exactly one storage version.
- Every declared CRD/XRD version is served.
- Every XRD has at least one referenceable version.

**Failure mode**: Kubernetes rejects the definition, Crossplane cannot resolve
the composite type, or tooling sees dead versions that should not be authored
against.

**Usual fix**: mark exactly one version as storage, mark live versions served,
and mark the intended XRD version referenceable.

### R2: Webhook Conversion Requires Acknowledgement

**Code**: `XPC002`

**Inputs**: concrete resources and CRDs, including storage version and
conversion strategy.

**Invariant**: a resource authored at a non-storage version must not rely on a
webhook conversion unless the author explicitly accepts that cost.

**Failure mode**: every read/write can invoke a conversion webhook. Under load
this can become a latency amplifier or a single point of failure for
controllers.

**Usual fix**: author the resource at the CRD storage version. If the webhook
conversion is intentionally accepted, add
`xpc.dev/accept-conversion-webhook: "true"`. Use `--strict-conversions` to
refuse webhook conversions entirely.

### R3: Composition Resolves To A Referenceable XRD

**Code**: `XPC003`

**Inputs**: Composition `compositeTypeRef` facts and XRD facts.

**Invariant**: every Composition references an existing XRD group/kind and a
referenceable version.

**Failure mode**: Crossplane cannot bind the Composition to a composite type,
so the XR reconciles without rendering the intended managed resources.

**Usual fix**: correct `spec.compositeTypeRef` or update the XRD so the target
version is referenceable.

### R4: Pipeline Function References Resolve

**Code**: `XPC004`

**Inputs**: Composition pipeline steps and Function facts, including accepted
input API versions.

**Invariant**: every pipeline step's `functionRef` names an installed Function,
and the step input version matches what that Function accepts.

**Failure mode**: Crossplane emits pipeline runtime errors, or the function
silently ignores fields because the input version is not what it expects.

**Usual fix**: install the Function resource, correct the `functionRef`, or
update the step input `apiVersion`.

### R5: Patch Type Compatibility

**Code**: `XPC005`

**Inputs**: resolved patch facts derived from Composition patches plus XRD/CRD
schemas.

**Invariant**: a patch source type must be assignable to the patch target type
after declared transforms are applied.

**Failure mode**: Crossplane patch evaluation fails at runtime, often with an
error far from the manifest line that caused it.

**Usual fix**: add a `convert` transform, change the source/target fields, or
correct the schema reference.

### R6: Sync-Wave Ordering

**Code**: `XPC006`

**Inputs**: Argo Application facts, sync-wave entries, XRDs, Compositions,
Functions, Providers, CRDs, and resources.

**Invariant**: Argo sync waves must respect Crossplane readiness dependencies:

- XRD before XR.
- Function before Composition that uses it.
- Provider before managed resources whose CRDs it supplies.
- Composition before XR that selects it.

**Failure mode**: Argo applies resources before their dependencies exist or are
healthy, producing transient or persistent sync failures.

**Usual fix**: add or adjust `argocd.argoproj.io/sync-wave` annotations so the
dependency has a lower wave than the dependent object. Some subrules are scoped
to cross-Application relationships to avoid flagging safe same-transaction
patterns.

### R7: Argo Label Tracking Conflicts With Crossplane

**Code**: `XPC007`

**Inputs**: Argo Application tracking mode and Composition/resource ownership
facts.

**Invariant**: Crossplane-managed resources should not be managed by Argo CD
using label-based tracking when Crossplane propagates those labels downstream.

**Failure mode**: Argo incorrectly decides it owns Crossplane-created resources
and may prune them or fight Crossplane for ownership.

**Usual fix**: use annotation tracking:
`argocd.argoproj.io/tracking-method: annotation`.

### R8: Crossplane v1/v2 Machinery Placement

**Code**: `XPC008`

**Inputs**: XRD facts and resource facts.

**Invariant**: resources targeting Crossplane v2-style XRDs must place
Crossplane machinery fields under `spec.crossplane`, not at the v1 top level.

**Failure mode**: fields such as `compositionRef`, `compositionSelector`, or
`publishConnectionDetailsTo` are silently ignored.

**Usual fix**: move machinery fields to the v2 location required by the XRD.

### R9: Bootstrap Required Resources

**Code**: `XPC009`

**Inputs**: Composition pipeline required resources and the World resource set.

**Invariant**: a required resource used during pipeline bootstrap must either
already exist or be produced by an earlier step.

**Failure mode**: the first reconcile of an XR fails because the pipeline asks
for a resource that cannot exist yet.

**Usual fix**: create the required resource earlier, reorder pipeline steps, or
mark an intentional gap with `xpc.dev/accept-bootstrap-gap: "true"`.

### R10: Secret Taint Does Not Leak To Plain Fields

**Code**: `XPC010`

**Inputs**: resolved patch facts and secret-taint metadata.

**Invariant**: credential material must flow only into secret-aware sinks.

**Failure mode**: passwords, keys, or connection details appear in ordinary
spec/status fields where they can be logged, displayed, or read too broadly.

**Usual fix**: route the value through a SecretRef/connection-secret field. If
the declassification is intentional, annotate it with `xpc.dev/declassify`.

### R11: API Deprecation Calendar

**Code**: `XPC011`

**Inputs**: resource, provider, Composition, and CRD facts plus the deprecation
catalog.

**Invariant**: manifests should not use APIs or provider capabilities that are
deprecated, removed, or past their supported window.

**Failure mode**: a configuration that works today stops applying after a
Kubernetes/provider upgrade.

**Usual fix**: migrate to the supported API version or provider capability
before the removal date.

### R12: No Dangling Mounts Across A Sync Trajectory

**Code**: `XPC012`

**Inputs**: `MountRef` facts and simulated trajectory state. The expensive
owner/target/state join is precomputed in Go as `r12-violation` facts.

**Invariant**: no live Pod-bearing workload may retain a non-optional mount of
a ConfigMap or Secret that is absent from the simulated cluster state.

**Failure mode**: during a sync, a workload starts or restarts while a required
ConfigMap/Secret has already been pruned or was never applied. The Pod fails to
start.

**Usual fix**: order deletion after the dependent workload is gone, keep the
target resource present, or mark the mount optional when absence is valid.

### R14: No RBAC Regression Across A Sync Trajectory

**Code**: `XPC014`

**Inputs**: `SARef`, `RBACBinding`, Role/ClusterRole, and simulated trajectory
state. The binding/state joins are precomputed in Go as `r14-violation` facts.

**Invariant**: while a Pod-bearing workload is live, at least one declared
binding for its ServiceAccount must also be live together with the referenced
Role/ClusterRole.

**Failure mode**: a workload survives a wave but loses the permissions it
declared through its ServiceAccount, producing runtime authorization failures.

**Usual fix**: keep the RoleBinding/ClusterRoleBinding and Role/ClusterRole
until the workload is gone, or reorder hooks/waves so permission teardown
happens last.

### R15: AppProject Kind Whitelist

**Code**: `XPC.D.kind-whitelisted`

**Inputs**: Argo Applications, AppProjects, CRD scopes, and resources grouped by
owning Application. The app/resource/project join is precomputed in Go.

**Invariant**: every resource kind managed by an Application must be allowed by
that Application's AppProject whitelist:

- cluster-scoped kinds in `clusterResourceWhitelist`.
- namespace-scoped kinds in `namespaceResourceWhitelist`.

Absent whitelist fields use Argo CD permit-all semantics; explicit empty lists
mean deny-all.

**Failure mode**: Argo CD refuses to sync the Application because the project
does not allow the resource kind.

**Usual fix**: add the missing `{group, kind}` entry to the appropriate
whitelist or move the resource to an Application managed by an appropriate
project.

### R16: Selector Resolved Paths Need Ignore Differences

**Code**: `XPC.E.selector-needs-ignore-diff`

**Inputs**: selector usage facts and flattened ignore-diff entries. Coverage is
precomputed in Go using group/kind buckets and wildcard semantics.

**Invariant**: if a Crossplane managed resource declares a `*Selector` field,
the sibling resolved path that Crossplane writes back into `spec` must be
covered by an Argo CD `ignoreDifferences` entry or a Crossplane field-manager
ignore entry.

**Failure mode**: Argo sees Crossplane's resolved ID as drift and repeatedly
tries to remove it; Crossplane writes it back again.

**Usual fix**: add `ignoreDifferences` to the owning Application/ApplicationSet
for the provider group/kind and resolved path, or use
`managedFieldsManagers: [crossplane]` where appropriate.

### R17: Resource Field Validity

**Code**: `XPC.A.resource-field-valid`

**Inputs**: manifest validation facts emitted by the Go schema walker.

**Invariant**: concrete resources must match the CRD/XRD OpenAPI schema for
known fields, required fields, enum values, and scalar types.

**Failure mode**: the API server or provider rejects the resource, or an
unknown field is silently dropped.

**Usual fix**: correct the field path, type, enum value, or required field in
the manifest.

### R18: Renderer Succeeds

**Codes**: `XPC.H.helm-renders`, `XPC.H.kustomize-renders`,
`XPC.H.composition-renders`

**Inputs**: render result facts for Helm, Kustomize, and Crossplane
Composition rendering.

**Invariant**: if render coverage is enabled, every declared source that xpc
claims to inspect must render successfully, or the missing coverage must be
reported.

**Failure mode**: downstream rules inspect the wrapper Application but not the
actual manifests Argo/Crossplane will apply.

**Usual fix**: install the missing renderer binary, fix the chart/overlay, pass
the appropriate `--*-bin` flag, configure `--helm-cache-dir` for remote charts,
or use `--skip-render` when reduced coverage is intentional.

### R19: Helm Values Match Values Schema

**Code**: `XPC.H.values-well-typed`

**Inputs**: merged Helm values and chart `values.schema.json` validation
issues.

**Invariant**: effective Helm values should satisfy the chart's declared JSON
schema.

**Failure mode**: Helm rendering fails, renders a default the author did not
intend, or produces invalid downstream resources.

**Usual fix**: correct values files, inline values, or chart schema; ensure
multi-source `$values` references resolve locally.

### R20: Render Determinism

**Code**: `XPC.H.render-deterministic`

**Inputs**: double-render comparison results.

**Invariant**: rendering the same source twice with the same inputs should
produce byte-identical manifests.

**Failure mode**: charts that call random/time functions or otherwise depend
on ambient state make CI and Argo diffs unstable.

**Usual fix**: remove non-deterministic template functions or explicitly pass
stable values.

### R21: Late-Init Fields Need Ignore Differences

**Code**: `XPC.E.late-init-needs-ignore-diff`

**Inputs**: late-init usage facts and flattened ignore-diff entries. Coverage
is precomputed in Go using the same wildcard semantics as R16.

**Invariant**: provider late-initialized `spec.forProvider.*` fields must be
covered by Argo `ignoreDifferences`, or the resource must opt out of late
initialization through management policies or provider-specific controls.

**Failure mode**: the provider writes observed cloud defaults back into
`spec`, Argo sees the write as drift, and syncs fight the provider.

**Usual fix**: add an ignore-diff entry for the field, use
`managedFieldsManagers: [crossplane]`, configure management policies to omit
`LateInitialize`, or use a provider-specific omit-late-init flag.

### R22: Server-Side Apply and Crossplane Management Policies

**Codes**:

- `XPC.E.ssa-managementpolicies-observe`
- `XPC.E.ssa-managementpolicies-partial`
- `XPC.E.ssa-managementpolicies-nondefault`

**Inputs**: owning Application sync options, resource management policies, and
resource identity facts.

**Invariant**: Argo CD server-side apply must not combine with Crossplane
management-policy modes in a way that causes Argo to field-manage data
Crossplane is supposed to observe or partially manage.

**Failure mode**: field ownership drifts between Argo, Crossplane, and the
provider; observe-only or partial-management resources can be mutated by Git
syncs that were intended only to declare shape.

**Usual fix**: disable ServerSideApply for the owning Application, broaden or
clarify Crossplane management policies, or intentionally configure the rule
mode (`--ssa-mp-mode=observe|partial|any`) for the repo's risk tolerance.

### R23: State-Bearing Managed Resources Must Orphan

**Code**: `XPC.S.crossplane-state-needs-orphan`

**Inputs**: Crossplane deletion-policy facts for state-bearing allowlisted
kinds plus bypass annotations and name carve-outs.

**Invariant**: every state-bearing Crossplane managed resource must declare
`spec.deletionPolicy: Orphan`, unless an explicit bypass opts out.

State-bearing defaults include Aurora, DocDB, MySQL, KMS keys, S3 buckets, and
VPCs.

**Failure mode**: deleting the Kubernetes CR can run a real destructive API
call against the external object.

**Usual fix**: add `spec.deletionPolicy: Orphan`. For intentional destruction,
add `xpc.io/allow-delete: "true"` or a configured alias such as
`policy.facilitygrid.io/allow-delete: "true"`.

### R24: ApplicationSet Finalizer Requires Preserve

**Code**: `XPC.E.appset-finalizer-without-preserve`

**Inputs**: ApplicationSet template finalizers and AppSet-level sync policy.

**Invariant**: an ApplicationSet that bakes
`resources-finalizer.argocd.argoproj.io` into generated Applications must set
`spec.syncPolicy.preserveResourcesOnDeletion: true`.

**Failure mode**: when a generator stops producing a parameter set or the
AppSet is deleted, Argo cascades deletion through every resource the generated
Application owns. This is the fg-synapse INC-6 shape.

**Usual fix**: set `preserveResourcesOnDeletion: true` on the ApplicationSet or
remove the cascading finalizer from the template.

### R25: Prod ApplicationSets Must Not Auto-Sync

**Code**: `XPC.E.prod-appset-autosync`

**Inputs**: ApplicationSet name, configured prod-name patterns, and template
sync policy.

**Invariant**: an ApplicationSet whose name matches a prod pattern must not set
`spec.template.spec.syncPolicy.automated`.

**Failure mode**: a destructive Git change, generator shrink, or filter bug can
apply to prod without a manual sync gate.

**Usual fix**: remove `automated` from the template for prod-named AppSets, or
split automation into a non-prod pattern if that is truly intended.

### R28: ProviderConfig Reference Resolves

**Code**: `XPC.B.providerconfig-resolves`

**Inputs**: managed-resource `spec.providerConfigRef.name`, the set of declared
ProviderConfig / ClusterProviderConfig names, and the allowed-provider-configs
allowlist.

**Invariant**: every managed resource's `providerConfigRef.name` resolves to a
declared (Cluster)ProviderConfig or an allowlisted name.

**Failure mode**: a typo names nothing, Crossplane cannot resolve the credential
binding, and the resource silently never reconciles while the deploy looks
healthy.

**Usual fix**: correct the reference to a declared ProviderConfig, or add the
intended name to the allowlist.

### R29: Fargate Claim Environment Label

**Code**: `XPC.E.fargate-claim-env-label`

**Inputs**: claims of policed kinds (FargateApp / FargateWorker / FargateService
by default), the required environment-label key, and the allowed value enum
(prod / preview / ops by default).

**Invariant**: every policed claim carries the environment label with a value in
the allowed enum.

**Failure mode**: a missing label hides the claim from blast-radius reasoning,
monitoring escalation, and account scoping; an invalid value silently misroutes
it.

**Usual fix**: add the environment label with a valid value to the claim
(usually in its Helm values file). Forward-looking: coverage depends on the scan
scope including the `deploy/facilitygrid/{prod,preview}/` values files.

### R30: ExternalSecret Store Resolves

**Code**: `XPC.K.externalsecret-store`

**Inputs**: ExternalSecret `spec.secretStoreRef.name` and the configured store
allowlist (`external-secret-stores.allowed-names` in `xpc.yaml`).

**Invariant**: every ExternalSecret references an allowlisted (Cluster)SecretStore.

**Failure mode**: a wrong or typo'd store name names a store that does not exist,
so external-secrets fails to sync (`SecretSyncedError`); the target Secret is
never created and every workload mounting it fails.

**Usual fix**: point `secretStoreRef.name` at a real store, or add it to the
allowlist.

### R31: forProvider Canonical Form

**Code**: `XPC.M.forprovider-canonical-form` (Category M, Tier-1 static)

**Inputs**: managed-resource `forProvider` fields registered as
provider-canonicalized, and their literal values.

**Invariant**: a registered provider-canonicalized field is written in canonical
form (e.g. ECS `spec.forProvider.taskDefinition` as `family:revision`, not a
bare family name).

**Failure mode**: a non-canonical literal makes desired `!=` observed forever;
upjet re-issues the external Update on every reconcile and the status write
conflicts with the poll loop — a self-sustaining reconcile storm (fg-manifold
MR !2232).

**Usual fix**: write the field in the canonical form the provider echoes back,
or omit it and let the provider populate it.

### R32: Observed/Desired Fixed Point

**Code**: `XPC.M.observed-desired-fixed-point` (Category M, Tier-3 dynamic)

**Inputs**: `spec.forProvider` leaves and the matching `status.atProvider` leaves
on status-bearing (live) managed resources.

**Invariant**: every `forProvider` leaf on a live resource converges to its
`status.atProvider` value.

**Failure mode**: a persistent desired `!=` observed divergence is the
reconcile-storm fingerprint captured from reality — the provider keeps
reconciling a value the cloud will never echo back.

**Usual fix**: align the desired value with what the provider reports (often the
canonical-form fix from R31). Only fires when status is present — i.e. a
`--from-cluster` snapshot merged into the World; silent on plain disk manifests.

### R33: Duplicate Env Key

**Code**: `XPC.M.duplicate-env-key` (Category M, Tier-2 heuristic)

**Inputs**: go-templating Compositions that build an ECS `containerDefinitions`
environment array.

**Invariant**: a container's environment array must not declare the same variable
name more than once.

**Failure mode**: AWS dedupes the env array on registration, so the desired
`containerDefinitions` never matches the stored (deduped) task def — a permanent
diff on the IMMUTABLE `container_definitions` field, so upjet hard-fails with
`ReconcileError`. The convergence-failure sibling of R31.

**Usual fix**: remove the duplicate environment entry from the Composition
template.

### R34: Computed Block Alias

**Code**: `XPC.M.computed-block-alias` (Category M, Tier-2 heuristic)

**Inputs**: go-templating Compositions that build an action which the provider
reads back as a full computed sub-block (registry:
`ComputedBlockAliasRegistry`). Seeded with elbv2 `LBListenerRule` and
`LBListener` forward actions.

**Invariant**: a `forward` action must be written in the canonical sub-block form
(`forward.targetGroup[].arn{,Ref,Selector}` + `weight` + explicit `order:`), not
the simple `targetGroupArn{,Ref,Selector}` scalar alias.

**Failure mode**: AWS always computes the full `action.forward{ stickiness{},
targetGroup[]{} }` block + `order: 1`, so the alias form leaves
`forProvider.action` permanently unequal to the read-back — upjet re-issues
`UpdateRule` every reconcile and the async status write 409-conflicts with the
poll loop → reconcile storm on `provider-aws-elbv2` (fg-manifold MR !2336). The
missing-computed-BLOCK sibling of R31 (non-canonical SCALAR).

**Usual fix**: emit the canonical `forward` block + explicit `order:`, and leave
`stickiness` unset (Optional+Computed — adding a disabled stickiness with a
non-zero duration re-introduces a new perpetual diff).

## Plan Rules

`xpc plan` compares two worlds. These rules cannot be expressed by a single
tip because the risk is in the transition.

### R26: Destructive Delete

**Code**: `XPC.P.destructive-delete`

**Inputs**: base/head resource identity delta, state-bearing kind registry,
base-side deletion policy, and delete bypass annotations.

**Invariant**: a state-bearing managed resource must not disappear from head
unless the base-side resource was already protected with
`deletionPolicy: Orphan` or an explicit delete bypass.

**Failure mode**: applying the PR deletes the CR and Crossplane deletes the
external database, bucket, key, or network object.

**Usual fix**: keep the resource, add `deletionPolicy: Orphan` before removal,
or add an explicit allow-delete annotation for intentional destruction.

### R27: Immutable Change

**Code**: `XPC.P.immutable-change`

**Inputs**: base/head modified resources and the immutable field registry.

**Invariant**: registered immutable scalar fields must not change in place.

**Failure mode**: Kubernetes, Crossplane, or the provider must delete and
recreate the underlying object. For state-bearing resources this can mean data
loss or disruptive replacement.

**Usual fix**: revert the immutable field change, create a new resource
identity and migrate intentionally, or add `xpc.io/allow-immutable-change:
"true"` on the head manifest when replacement is intended.

### Cascade Risk

**Code**: `XPC.P.cascade-risk`

**Inputs**: base/head Application delta, base-side Application finalizers, and
Application sync policy.

**Invariant**: an Argo CD Application with the cascading resources finalizer
must not be removed across a plan unless resources are preserved or an explicit
delete bypass exists.

**Failure mode**: removing the Application cascades DELETE through every
managed resource before Crossplane or Argo safeguards can intervene.

**Usual fix**: keep the Application, set
`spec.syncPolicy.preserveResourcesOnDeletion: true` before removal, remove the
cascading finalizer, or explicitly acknowledge intentional destruction.

## Retired / Historical

### R13: No Immutable Change In A Single Trajectory

**Code**: `XPC013` (retired)

The original single-tip trajectory version was retired because immutable field
change is naturally a two-world property. It is now modeled by
`XPC.P.immutable-change` in `xpc plan`.

## Adding Or Changing A Rule

1. Define the invariant in this catalogue.
2. Decide whether the required facts already exist in `World`.
3. If the rule needs high-cardinality joins, precompute compact facts in Go.
4. Keep Shen-side logic small and diagnostic-focused.
5. Add fixtures under `testdata/fixtures/`.
6. Add checker tests that assert both positive and negative cases.
7. Update `xpc explain` for user-facing codes.
