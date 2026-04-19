---
date: 2026-04-18T00:00:00Z
researcher: Reuben Brooks
git_commit: 0ecd1db3a8a14db3e91314f0f96393248d185784
branch: claude/phase1-cleanup
repository: pyrex41/cross-validate
topic: "fg-manifold as an xpc target: what they actually hit, and what xpc would have to catch"
tags: [research, external-target, fg-manifold, crossplane, argocd, gap-analysis]
status: complete
last_updated: 2026-04-18
last_updated_by: Reuben Brooks
---

# Research: fg-manifold as an xpc target

**Date**: 2026-04-18
**Subject repo**: `~/fg/fg-manifold` (GitLab: `lab.facilitygrid.net/facility-grid/fg-manifold`)
**xpc commit at time of research**: `0ecd1db`

## Research Question

Take a deep look at the user's own GitOps/ArgoCD/Crossplane repo, read the MR history for the kinds of issues the team has actually hit, and assess how far xpc is from being *useful in that context*.

## Summary

fg-manifold is a substantial GitOps monorepo: **one bootstrap Application** seeding **60+ ApplicationSets** on a `facilitygrid-ops` EKS cluster, driving Crossplane-managed AWS infrastructure across four AppProjects (`default-locked`, `ops`, `prod`, `preview`, `mgmt`) and four AWS accounts. Crossplane provider packages include AWS Upbound family (28 Providers), GitLab, SignOz, Tailscale, and provider-sql. Claims are rendered from three Helm charts (`crossplane-claim`, `crossplane-fargateservice`, `crossplane-workers`).

**Existing PR-time validation: essentially none for manifests.** `.gitlab-ci.yml` runs conventional-commit title validation and CODEOWNERS approval — no kubeval, kubeconform, conftest, kyverno, helm lint, or argo-cd lint. Every manifest-shape bug surfaces post-merge, at ArgoCD sync time, in production-adjacent clusters.

**The MR history over the last ~500 merges is dominated by a narrow set of pain categories**, in descending frequency:

1. **Crossplane CRD schema field mismatches** — wrong field names, wrong types, missing required fields, wrong `apiVersion` (!1186, !1394, !1350, !1179, !1335, !1416, !1499, !1497, !1508, !1111, !1469)
2. **Crossplane selector → resolved-reference drift** — `idSelector`/`nameSelector` resolves into spec, ArgoCD fights without `ignoreDifferences` (!1344, !1341, !1250, !883, !890, !1366)
3. **Late-init field drift** — AWS provider observes state, writes back, needs `ignoreDifferences` or `managementPolicies: Observe` (!1048, !1172, !1502, !1247, !1147, !893, !892)
4. **ApplicationSet / AppProject whitelist misses** — resource kind not in project whitelist (!1388, !1383)
5. **ServerSideApply × managementPolicies interactions** (!1191, !1189, !1188, !1090)
6. **Provider-package bugs requiring downgrade or swap** (!1506, !1466, !1316, !1311 — "use ECS v1beta1 to eliminate conversion webhook overhead" — literally XPC002 territory)
7. **External-name normalization** (!1395, !1091, !990, !538)

**How much of this does xpc catch today? Roughly none of it.** Every implemented rule (R1–R14 → XPC001–XPC014) targets either happy-path Crossplane reference resolution (R3/R4), Composition-patch typing (R5), Argo wave ordering (R6), or fabricated trajectory invariants (R12/R14). None of the 14 rules validates **raw manifest fields against the CRD schema** — the single largest family of bugs in this repo. The Category A generators `patch-source-type`/`patch-target-type` type-check the *patch* endpoint against schemas but don't run over a standalone managed-resource manifest.

**What xpc would need to be useful here**, ranked by blast radius:

- **A1. Manifest-field-against-CRD-schema rule (Category A, new generator)** — catches 40–50% of the MR history shown. The schema-typed-field machinery already exists in `pkg/schemas/ResolveFieldType` and `TypeAssignable`; it's used only for patches today. Extending it to validate every `ResourceInfo.Raw` against the corresponding CRD schema is a straight extension.
- **A2. Crossplane selector/resolved-reference rule (new category or Category E extension)** — for every resource referencing a `*Selector` field, the Application must have `ignoreDifferences` for the corresponding `*id`/`*ref` path. Table-driven; needs no renderer.
- **A3. AppProject whitelist check (Category D, unimplemented)** — enumerate resource kinds produced by an Application and verify they're in `clusterResourceWhitelist`/`namespaceResourceWhitelist`. Needs renderer integration to be complete; usable on direct-manifest Applications today.
- **B1. Helm rendering (Category H, unimplemented)** — fg-manifold renders claims through three Helm charts. Until xpc executes renderers, it sees zero of the workload resources these Apps actually produce. Hard blocker for claim-bearing Applications.
- **B2. ApplicationSet generator expansion** — the repo is entirely AppSet-driven (`list`, `matrix`, `pullRequest`). xpc has `types.ArgoApplicationSet` but doesn't evaluate generators into concrete Applications. Needed to scope checks over the realistic set of rendered apps.

Ballpark: a targeted ~2-month push on **A1 + A2 + A3** (no renderer required) would land xpc somewhere that catches ~50% of what fg-manifold MRs currently fix post-hoc. A full "tool of record" position still requires **B1 + B2** which is another quarter of work.

## Detailed Findings

### The subject repo

**Scale and layout**:

- One bootstrap Application at `deploy/facilitygrid/ops/bootstrap/eks-argocd/bootstrap-applicationsets.yaml` points at the ApplicationSet directory — that's the only hand-edited Application; everything else flows from there.
- 60+ ApplicationSets under `deploy/facilitygrid/ops/applicationsets/` covering ArgoCD itself, cert-manager, AWS Load Balancer Controller, buildkit, EBS CSI, Velero, Wazuh, multiple Crossplane providers and platforms, preview-environment matrix builds, and ~10 per-service production AppSets.
- Corresponding Application manifest sources under `deploy/facilitygrid/ops/applications/<name>/aws/us-east-2/facilitygrid-ops/manifests/`.
- Four AppProjects (`appproject-default-locked.yaml`, `-ops.yaml`, `-prod.yaml`, `-mgmt.yaml`, plus `preview-environments.yaml` embedding one inline) with `clusterResourceWhitelist` of 21–25 kinds each, biased toward AWS Upbound + `platform.facilitygrid.net/*` XRDs.
- Three Crossplane Helm charts at `lib/charts/crossplane-{claim,fargateservice,workers}/` rendering `XFargateService`/`XFargateWorker`/generic claims.
- 28 AWS Upbound family Providers at `applications/crossplane-provider-aws/.../manifests/provider.yaml`, pinned to v2.5.0, with DMS on a separate 1h poll interval.

**Sync-wave conventions** (from CLAUDE.md and AppSet files):
- Wave 1: bootstrap ApplicationSet
- Wave 2: Crossplane providers (`crossplane-provider-*`)
- Wave 7: Crossplane platforms (`crossplane-platform-aws-*`)
- Wave 10: preview environments

This is the classic R6c "Provider wave < MR wave" shape. xpc's existing XPC006 would catch a regression here.

**Multi-account model**: `ops`, `mgmt`, `prod`, `preview`, `sustain-preview` AWS accounts with cross-account IAM roles wired via Terraform bootstrap and `providerConfigRef` on each managed resource.

**No existing manifest validation at PR time.** `.gitlab-ci.yml` runs `validate-mr` (conventional commit title) and `codeowners-check` (approval gate). `lefthook.yml` has only commented examples. No helm lint, kubeconform, conftest, or kyverno CLI. ArgoCD is configured with `--enable-helm --load-restrictor=LoadRestrictionsNone` in `argocd-values.yaml:628` but no pre-sync policy enforcement. Every typed-field bug gets caught by ArgoCD at sync time — *on the production cluster*.

### Pain taxonomy (from ~500 merged MRs)

Citations are MR numbers (`!NNNN`) in `facility-grid/fg-manifold`. Descriptions are paraphrased from titles and bodies.

#### (1) CRD schema field mismatches — the biggest bucket

| MR | Issue |
|---|---|
| !1186 | `LaunchTemplate` CRD has `privateIpAddress` (string); MR wrote `privateIpAddresses` (list). SSA typed patch failed. |
| !1394 | provider-sql `Grant.memberOf` is `string`, `memberOfRef` is a single object. MR wrote arrays; all three `SyncFailed`. |
| !1350 | `ParameterGroup` missing required `name` field. |
| !1179 | SSM Parameter schema error in `egress-proxy-preview`. |
| !1111 | `KMS Alias` CRD has no `forProvider.name`; field was being set. |
| !1335 | XRD `required` list included `appKeySecretArn` and `dbUserSecretArn` even though they were optional at the claim layer — claims failed with "missing required field." |
| !1416 | `Extension` resources needed `forProvider.extension` field set. |
| !1497, !1499 | `SES ConfigurationSet.sendingOptions` — wrong syntax (scalar) and null panic. |
| !1508 | `providerConfigRef.kind` missing on v1beta1 SES ConfigurationSet. |
| !1469 | `EmailIdentity` apiVersion needed to be v1beta2 and use `external-name`. |

Every one of these is a **static manifest-versus-CRD-schema error**. xpc has the schemas in `World.Schemas` and a partial resolver in `pkg/schemas/ResolveFieldType`; it just doesn't run them over raw manifests. Would need a new Category A generator — call it `resource-field-valid` — that walks every `ResourceInfo.Raw` against the matching CRD schema and emits diagnostics for: unknown field, wrong type, missing required, wrong enum value.

#### (2) Crossplane selector drift

Pattern: a Crossplane managed resource declares `subnetSelector.matchLabels: {…}`. Crossplane resolves this against the live cluster state into a concrete `subnetIds`/`subnetIdRefs` field on the spec. ArgoCD's diff engine sees the resolved field as "added" and the original selector as "still specified but resolved state is different" — and fights forever.

| MR | Resource | Resolved field path |
|---|---|---|
| !1344 | ASG | `spec.forProvider.vpcZoneIdentifier` |
| !1341 | ASG | `spec.forProvider.launchTemplate.id` |
| !1250 | VPC Endpoint, ASG, RDS Proxy | `routeTableIds`, `subnetIds`, `dbProxyName`, `vpcZoneIdentifierRefs` |
| !883 | ASG | `spec.forProvider.launchTemplate.id`, `idRef` |
| !890 | LaunchTemplate | multiple late-init fields |
| !1366 | ElastiCache | `parameterGroupName` |
| !36 | GitLab Project | `spec.forProvider.*` |

Fix in each case is to add `ignoreDifferences` with a `jqPathExpressions` entry to the ApplicationSet.

**This is a perfect xpc rule and it's not in the current taxonomy**: "for every Crossplane resource with a `*Selector` field, the containing Application/ApplicationSet must have an `ignoreDifferences` entry covering the resolved `*id`/`*name`/`*Ref` field path." Purely static, no renderer required. The Upbound providers conventionally pair `fooSelector` ↔ `foo`/`fooRef` so a small lookup table handles the common cases.

#### (3) Late-init field drift

AWS provider observes real values (default security groups, cluster identifiers, etc.) and writes them back to `spec.forProvider.*`. Same fight-loop as (2). MRs !1048, !1172, !893, !892, !1502, !1247, !1147, !672.

Fix is the same shape (`ignoreDifferences`) or `managementPolicies: ["Observe"]`, or `omitLateInitialize` as in !1502.

Could live in the same rule as (2) with a separate emit message, or as a Category I (Provider-capability) generator "`late-init field on resource X requires Application-level suppression`."

#### (4) AppProject whitelist misses

- !1388: `postgresql.sql.crossplane.io` used by sustain-preview but missing from the `preview` AppProject's `clusterResourceWhitelist`/`namespaceResourceWhitelist`.
- !1383: `XFargateService` + `XFargateExternalService` XRDs weren't in ArgoCD's `resource.exclusions`, producing phantom diffs.

Classic Category D. The xpc model already has `ArgoAppProject` with `ClusterResourceWhitelist`/`NamespaceResourceWhitelist` fields (`pkg/types/types.go:413`). A generator "for every resource kind produced by an Application, the AppProject permits it" is a few dozen lines once ApplicationSet generator expansion exists.

#### (5) ServerSideApply × managementPolicies

- !1191, !1189, !1188: `ServerSideApply=false` needed on VPC endpoints and SG rules to break SSA fights.
- !1090, !1147, !1502, !1247: `managementPolicies` explicitly pinned to `["Observe"]` or omit `LateInitialize` to break fights.

Category E "sync-option interaction obligations." Defined in the taxonomy (`docs/obligations.md:81-96`), unimplemented.

#### (6) Provider-package bugs

- !1311: "use ECS v1beta1 to eliminate conversion webhook overhead" — they hit webhook conversion cost and deliberately downgraded. **xpc's XPC002 would warn about this** if the manifest used a non-storage version of a webhook-converting CRD.
- !1316: re-reverted ECS back to v1beta2 object syntax. A classic apiVersion-shape fight.
- !1506: `sesv2.aws.upbound.io/v1beta2 ConfigurationSet` panics in `provider-aws-sesv2 v2.5.0`; switched to `sesv2.aws.m.upbound.io/v1beta1` (monolith) which is stable. Pure provider-capability knowledge.
- !564, !544, !452: provider-signoz version bumps fixing drift. Known-bad-version table.

These would live in Category I (Provider-capability obligations), currently unimplemented.

#### (7) External-name normalization

- !1091: Crossplane strips `alias/` prefix from KMS aliases; writing `alias/foo` causes drift until user strips it.
- !990: Crossplane uses full ARN for SM secret external-names; short names drift.
- !1395: `crossplane.io/external-name` must be in `metadata.annotations`, not under `spec`.
- !538: SignOz dashboards need pinned external-names and normalized JSON.

Provider-specific semantic rules. Category I territory.

### Mapping to current xpc rules

| fg-manifold pain | xpc rule today | Hit rate |
|---|---|---|
| CRD schema field mismatch | None. R5 checks patches, not raw manifests. | 0% |
| Selector/resolved-ref drift | None. | 0% |
| Late-init drift | None. | 0% |
| AppProject whitelist miss | Category D defined, not implemented. | 0% |
| SSA × managementPolicies | Category E defined, not implemented. | 0% |
| Provider-package bug | Category I defined, not implemented; R11 does only deprecation-calendar. | ~5% (via R11) |
| External-name normalization | None. | 0% |
| Webhook conversion cost | R2 / XPC002 — *would* fire on v1beta1 manifest against v1beta2-storage CRD with webhook conversion. | **Partial hit** |
| Wave ordering (provider wave < MR wave) | R6c / XPC006 — fires today on !wave-ordering and !provider-wave. | **Full hit** — not currently a frequent MR category, likely because they already have the pattern right. |
| Composition → XRD reference | R3 / XPC003 — fires on broken reference. | **Full hit** — low MR count here too, pattern is well-trodden. |
| Pipeline function reference | R4 / XPC004 — fires on missing function or version mismatch. | **Full hit** — they use Crossplane functions, but the MR history shows few bugs here. |

**The pattern is clear**: xpc's rules catch the *structurally clean-and-wrong* cases (broken references, bad wave orders) that this team has already engineered their way around. The actual MR noise is the *structurally reasonable-looking-but-schema-invalid* cases that xpc has no rule for.

### Renderer reality check

Three of the most active applications route through Helm charts:

- `lib/charts/crossplane-claim/` — generic polymorphic claim (kind from values)
- `lib/charts/crossplane-fargateservice/` — `FargateService` claim; templates 20+ fields including subnet selectors, IAM ARNs, CloudMap config, secrets references
- `lib/charts/crossplane-workers/` — multi-worker claim loop (range over `.Values.workers` dict, one `FargateWorker` per entry)

Every preview-environment Application produced by the `preview-environments.yaml` ApplicationSet's 4-source matrix ends at `crossplane-fargateservice` or `crossplane-workers`. **Without Helm execution, xpc sees an Application pointing at a chart and nothing inside it.** For this repo specifically, that's most of the interesting workload surface.

Category H (Rendering obligations) in the taxonomy is where this belongs. ADR-001 lists four generators — `helm-renders`, `kustomize-renders`, `values-well-typed`, `render-deterministic` — all currently stubs.

The good news: fg-manifold uses stock `helm template` / `kustomize build`, no plugins, no custom renderers. Shelling out is tractable. The values files are co-located with the Application manifest under `applications/<name>/...`, so resolving them is a filesystem walk.

### ApplicationSet generator reality check

`preview-environments.yaml` uses:

```
generators:
  - list: […]                                           # static demo/QA
  - matrix:                                             # PR builds
      - list: [{repo: backend, …}, {repo: frontend, …}, …]
      - pullRequest:
          gitlab: {api, labels: [preview, preview-workers], …}
```

The matrix × pullRequest combination produces `N_repos × N_open_PRs` Applications. xpc's `types.ArgoApplicationSet` (`pkg/types/types.go:472`) carries the `Generators` field but never evaluates it. For static (`list`, `git directories`, `cluster`) generators expansion is local-deterministic; for `pullRequest` and `scmProvider` generators it requires either a GitLab API call or an injected fixture.

Without expansion, Category D (AppProject whitelist) and G (cross-Application ownership collisions) can't be evaluated correctly over this repo — the set of actual Applications isn't known from the static YAML.

### What ADR-002 implies for this work

ADR-002 says new invariants land as Shen rules over enriched-World sections. Mechanically:

1. **Rule A1 (`resource-field-valid`)**: extend `pkg/types` with a structured-schema form or reuse `SchemaInfo`. Thread into a new world section like `(resource-field-facts …)` emitted per `(kind, field-path, declared-type, actual-type)`. Shen rule `kernel/r15-resource-field-valid.shen` pattern-matches and emits XPC.A.resource-field-valid. Fixture: one manifest with a wrong field name, one with a wrong type, one with a missing required field.
2. **Rule A2 (`selector-needs-ignore-diff`)**: add an IR table of selector→resolved-field pairings in `pkg/ir/selector_registry.go` (analogous to `immutable_registry.go`). Enrichment emits `(selector-usage OwnerKind OwnerName SelectorPath ResolvedPath)` facts. Another section carries `(ignore-diff AppName Path)` from each Application's `spec.ignoreDifferences`. Shen rule cross-joins and fires when a selector usage has no matching ignore-diff.
3. **Rule D1 (`appproject-whitelist`)**: `(argo-appprojects …)` section (type already exists in Go). Shen rule: for each `(resource Kind)` ∈ scope of an Application, verify the Kind is in the project's whitelist.

A1 and A2 would close a huge chunk of the pain without writing any renderer code. D1 is partial-credit without renderer support but still catches the two MRs in that bucket.

## Code References — xpc side

- `pkg/types/types.go:413` — `ArgoAppProject` already carries whitelist fields
- `pkg/types/types.go:472` — `ArgoApplicationSet` carries `Generators` but not evaluated
- `pkg/schemas/fetcher.go:101` — `ResolveFieldType` walks JSONSchema; reusable for A1
- `pkg/schemas/fetcher.go:134` — `TypeAssignable`; reusable for A1
- `pkg/ir/immutable_registry.go:7` — pattern for a static registry; template for A2's selector registry
- `pkg/ir/trajectory_extract.go:12` — pattern for a second enrichment pass
- `pkg/checker/bridge.go:331` — `sortedSection` helper; new sections plug in here
- `kernel/check.shen` — orchestrator to wire new rules into
- `docs/obligations.md:31-95` — Category A, D, E, H, I definitions awaiting implementation

## Code References — fg-manifold side

- `~/fg/fg-manifold/CLAUDE.md` — team-authored summary of the exact drift / late-init / "another operation in progress" pain points
- `~/fg/fg-manifold/.gitlab-ci.yml` — demonstrates there is no pre-merge manifest validation
- `~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/appproject-*.yaml` — whitelist structure xpc D1 needs to consume
- `~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/crossplane-platform-aws-prod.yaml` — representative ignoreDifferences block for A2 to cross-reference
- `~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/preview-environments.yaml` — ApplicationSet with matrix + pullRequest generators, the B2 test case
- `~/fg/fg-manifold/lib/charts/crossplane-fargateservice/templates/fargateservice.yaml` — Helm-templated claim, the B1 test case

## Roadmap implications

Ranked by fg-manifold value per unit effort, high to low:

1. **A1 manifest-field-vs-CRD-schema** — largest single category of MR noise; machinery mostly in place; doesn't require renderers. Probably the single highest-ROI rule xpc could add.
2. **A2 selector/resolved-ref** — second-largest category; purely static; needs a small registry; no renderer. Writing the selector registry is the bulk of the work.
3. **D1 AppProject whitelist** — small-but-real category; existing types support it; direct-manifest-only first pass catches the two observed MRs.
4. **Late-init rule** — related to A2 mechanics; can share the registry shape.
5. **B2 ApplicationSet generator expansion** — unlocks Category D correctly, is a prerequisite for any rule over preview/PR-driven Apps.
6. **B1 Helm/Kustomize rendering** — non-negotiable for full coverage of claim-based workloads; largest single build task. Without it, all of the `preview-environments`, `prod-services`, `crossplane-*-platform` Applications are partially-opaque.
7. **Category I provider-capability table** — covers (6) and (7) but requires maintained knowledge (provider version → known bugs). Smaller recurring benefit; defer unless the team asks.

"3–6 months from useful" in [the previous assessment] translates concretely here as: **if A1 + A2 + D1 land within a quarter, xpc would catch roughly half the MR noise this team currently resolves after the fact**, and a checker-in-CI becomes worth setting up. Everything else (B1, B2) expands coverage toward the rest but is not required for a first useful bite.

## Related Research

- `thoughts/shared/research/2026-04-18-so-what-have-we-actually-built.md` — what xpc currently ships
- `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md` — post-cleanup vision
- `docs/adr/001-bounded-obligation-taxonomy.md` — 12-category taxonomy
- `docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md` — the "new rule = Shen rule + new section + enrichment" recipe that A1/A2/D1 all follow

## Open Questions

- **Renderer execution environment**: is `helm template` + `kustomize build` acceptable in xpc's CI-facing binary? These require the binaries on PATH. Alternative: vendor a Go Helm/Kustomize library. Trade-off is binary size vs. reproducibility.
- **Selector → resolved-field table maintenance**: provider-aws family publishes these pairings in the CRD field docs (`description: "...set by subnetSelector"`). Could be scraped once into a static registry, or derived at runtime from CRD schema comments if reliable.
- **ApplicationSet pullRequest generator at lint time**: fake the PR list, or require the caller to pass it in, or skip and lint only the list/matrix-static portions. Probably "skip with warning" is the pragmatic first pass.
- **xpc as pre-commit vs. CI-only**: fg-manifold's `lefthook.yml` is empty. A pre-commit lint that only scans the touched files is a cheap win if xpc can be scoped to a single Application subtree; today `xpc check <path>` takes a directory and loads everything.
