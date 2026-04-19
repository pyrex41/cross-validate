# xpc → fg-manifold Coverage: Multi-Session Implementation Plan

> Plan file lives at `/Users/reuben/.claude/plans/research-written-wiggly-nova.md`.
> Once approved, copy to `thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md` and sync with `humanlayer thoughts sync`.

## Context

xpc ships 14 rules (XPC001–XPC014) that catch structurally-broken Crossplane/Argo configurations — dangling references, bad wave orders, unacknowledged webhook conversions. Research in `thoughts/shared/research/2026-04-18-fg-manifold-target-study.md` mapped ~500 merged MRs in `~/fg/fg-manifold` and found xpc catches **roughly 0%** of the actual MR noise, which clusters into five buckets xpc currently has no rule for:

| Bucket | Share | xpc today |
|---|---|---|
| CRD field schema mismatches | ~40% | ❌ |
| Selector → resolved-ref drift | ~20% | ❌ |
| Late-init field drift | ~15% | ❌ |
| Helm-rendered claim opacity | (blocker) | ❌ |
| AppProject whitelist misses | ~2% | ❌ |

The machinery needed to close these gaps is already largely in place:

- `pkg/types.ArgoApplication` parses `IgnoreDifferences`, `Sources[].Helm`, `Sources[].Kustomize` into typed fields (`types.go:215, 248, 250`).
- `pkg/types.ArgoAppProject` parses `ClusterResourceWhitelist`/`NamespaceResourceWhitelist` as `[]ArgoGroupKind` (`types.go:425–431`).
- `pkg/schemas.ResolveFieldType` walks a dotted path through a stored OpenAPI schema (`fetcher.go:101`).
- `pkg/ir/immutable_registry.go` is the template for static, hand-curated lookup tables.
- `pkg/checker/bridge.go sortedSection[T]` (line 331) is the generic helper for adding new World sections.
- `kernel/check.shen` follows a boilerplate `(load "rN.shen") … extract-section … (mark-rule "XPC0NN" (check-rN …))` shape for every rule.

What is **not** in place: any execution of `helm template` or `kustomize build`, any (apiVersion, kind) → schema lookup helper, any extraction of `required`/`enum` from OpenAPI schemas, and any evaluation of ApplicationSet generators.

## Scope (what we're building, across 5 sessions)

Five self-contained sessions, each leaving main green with a new rule and fixture:

1. **S1: `XPC.D.kind-whitelisted`** — AppProject whitelist rule. Smallest scope; all IR present. Establishes the multi-phase pattern.
2. **S2: `XPC.E.selector-needs-ignore-diff`** — static selector registry + cross-reference against `spec.ignoreDifferences`. Also lays the mechanical pattern for S2b (late-init drift, same registry shape).
3. **S3: `XPC.A.resource-field-valid`** — walk every `ResourceInfo.Raw` against the matching CRD schema (unknown field, wrong type, missing required, wrong enum). Largest single-rule ROI in fg-manifold. Extends schema machinery with required/enum extraction and an `(apiVersion, kind) → schema` index.
4. **S4: `XPC.H.helm-renders` + `XPC.H.values-well-typed`** — Helm rendering. Shells out to `helm template`, merges rendered manifests into `World.Resources`. Unblocks coverage of fg-manifold's three `crossplane-{claim,fargateservice,workers}` charts.
5. **S5: `XPC.H.kustomize-renders`, `XPC.H.render-deterministic`, ApplicationSet generator expansion** — Kustomize rendering + `list`/`matrix`/`git` generator evaluation (static), `pullRequest`/`scmProvider` as stub-with-injected-fixture.

## What we're NOT doing

- **Live cluster discovery.** Snapshots stay as-is; we read schemas from disk only.
- **A new obligation category.** A2 goes into Category E (sync-option interaction) as the closest semantic fit; no ADR-013 needed.
- **Late-init field drift as a distinct rule this plan cycle.** The S2 selector registry shape is the blueprint — lifting it to a `late-init-registry` is a follow-up session.
- **Vendored Helm/Kustomize Go libraries.** S4/S5 shell out to the binaries on PATH. Vendoring can come later if reproducibility forces it.
- **Category I (provider-capability) generators** — deferred, smaller MR share.
- **Removing or renaming any existing XPC001–XPC014 code.** All new codes are additive.

## Implementation approach

Every session follows the ADR-002 recipe:

1. **IR side (Go).** Add a typed field to `World`, write an enrichment or registry, build a `cmp` + `toObj` pair, register in `worldToShenObj` via `sortedSection`.
2. **Kernel side (Shen).** Add a rule file under `kernel/`, `(load …)` it in `check.shen`, bind the section via `extract-section`, call through `mark-rule`.
3. **Fixture.** Add a directory under `testdata/fixtures/` plus a test in `pkg/checker/check_test.go` using the existing `loadFixture` + `findDiagByCode` helpers.
4. **Docs.** Add the generator to `docs/obligations.md`. Add an `xpc explain` entry in `cmd/xpc/main.go`.

The phase boundaries below mark session boundaries — each session ends with green tests and a mergeable PR.

---

## Session 1 — `XPC.D.kind-whitelisted` (AppProject whitelist)

### Overview
For every `ArgoApplication` in scope, every managed resource kind it produces must be in its `AppProject`'s `ClusterResourceWhitelist` (cluster-scoped) or `NamespaceResourceWhitelist` (namespaced). First pass handles direct-manifest Applications only; Helm/Kustomize rendering fills in later (S4/S5) at which point this rule automatically gains coverage.

### Changes

#### 1. New Shen rule
**File**: `kernel/r15-appproject-whitelist.shen` (new)
- Define `check-r15 Apps AppProjects Resources CRDs` emitting one judgment per (app, resource kind) pair where the kind is missing from the project's whitelist and not in its blacklist.
- Use `type-assignable?`-style helpers — keep it pure-functional over s-expression tuples.

#### 2. Wire rule into orchestrator
**File**: `kernel/check.shen`
- Add `(load "r15-appproject-whitelist.shen")` at line ~31.
- Add `AppProjects (extract-section argo-appprojects Sections)` to the let-binding (line ~65).
- Add `R15 (mark-rule "XPC.D.kind-whitelisted" (check-r15 ArgoApps AppProjects Resources CRDs))` and extend the trailing `append` chain.

#### 3. Expose AppProjects to Shen
**File**: `pkg/types/types.go`
- Confirm `World.ArgoAppProjects []ArgoAppProject` field exists (it does via research — verify line number).

**File**: `pkg/checker/bridge.go`
- Add `argoAppProjectCmp` and `argoAppProjectToObj` (`~line 450` neighborhood, next to `argoAppToObj`).
- Emit `(argo-appproject Name Namespace ClusterWhitelist NamespaceWhitelist …)` where each whitelist entry is `(group-kind Group Kind)`.
- Insert `sortedSection("argo-appprojects", w.ArgoAppProjects, argoAppProjectCmp, argoAppProjectToObj)` into the sections slice in `worldToShenObj` (~line 392).

#### 4. Builder
Verify `pkg/ir/builder.go addArgoAppProject` (~line 644) populates `ClusterResourceWhitelist`/`NamespaceResourceWhitelist` via `parseGroupKindList`. Add test if missing.

#### 5. Fixture
**Dir**: `testdata/fixtures/appproject-whitelist-miss/`
- `appproject.yaml` — AppProject with a small `clusterResourceWhitelist` that omits `sql.crossplane.io/*`.
- `app.yaml` — Application using that project.
- `resources.yaml` — a `Database.sql.crossplane.io/v1alpha1` manifest.

**File**: `pkg/checker/check_test.go`
- `TestR15_AppProjectWhitelist` asserts exactly one `XPC.D.kind-whitelisted` diagnostic.

#### 6. Diagnostics UX
**File**: `cmd/xpc/main.go`
- Add entry in `explain` subcommand for `XPC.D.kind-whitelisted`.

**File**: `docs/obligations.md`
- Under Category D, mark `kind-whitelisted` as *implemented*.

### Success criteria

#### Automated
- [ ] `make test` passes (all existing + new `TestR15_AppProjectWhitelist`)
- [ ] `make lint` passes
- [ ] `xpc check testdata/fixtures/appproject-whitelist-miss` exits non-zero with exactly one `XPC.D.kind-whitelisted` diagnostic
- [ ] `xpc explain XPC.D.kind-whitelisted` prints a non-empty explanation
- [ ] `xpc check testdata/fixtures/basic` still reports no diagnostics (no regressions)

#### Manual
- [ ] `xpc check ~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/` reports the `!1388` class of miss (sql.crossplane.io used from the `preview` AppProject without whitelist entry) — confirm on the actual target repo before merging.

**Pause** for human confirmation of the manual check before S2.

---

## Session 2 — `XPC.E.selector-needs-ignore-diff` (selector registry + cross-ref)

### Overview
Crossplane managed resources with a `*Selector` field resolve that selector on the cluster into a concrete `*id`/`*Ref` field. ArgoCD's diff engine then fights unless the Application declares an `ignoreDifferences` entry covering the resolved path. Rule emits one error per `(owner, selector, resolved path)` triple where no matching `ignoreDifferences` exists on the owning Application.

### Changes

#### 1. Static selector registry
**File**: `pkg/ir/selector_registry.go` (new; modeled line-for-line on `immutable_registry.go`)
- `SelectorMapping struct { Group, Kind, SelectorPath, ResolvedPath, Reason string }`.
- `SelectorRegistry() []types.SelectorMapping` returning ~30 hand-curated entries from the `provider-aws` / `provider-gitlab` family (the pairs shown in MRs !1344, !1341, !1250, !883, !890, !1366, !36, !1247, !1172, !1147).
- One entry per `(Kind, SelectorPath)`; `ResolvedPath` is the spec path that needs `ignoreDifferences`.

#### 2. Type
**File**: `pkg/types/types.go`
- Add `SelectorMapping` struct (alongside `ImmutableField`).
- Add `SelectorMappings []SelectorMapping` to `World` and `Applications`-keyed `SelectorUsages []SelectorUsage` computed in enrichment.

#### 3. Enrichment
**File**: `pkg/ir/trajectory_extract.go` (existing enrichment pass)
- Add a second loop over `World.Resources`: for each resource, consult `SelectorRegistry()` by `(Group, Kind)`; if the resource's `Raw` walks to `SelectorPath` (non-nil), emit a `SelectorUsage{Owner: ResourceKey, SelectorPath, ResolvedPath}`.
- Append registry itself to `w.SelectorMappings` (same pattern as `w.ImmutableFields`).

#### 4. Bridge
**File**: `pkg/checker/bridge.go`
- Add `sortedSection` entries for `selector-mappings` and `selector-usages`.
- Emit `(ignore-diff-entry AppName JSONPointer JQPathExpr)` facts extracted from `ArgoApplication.IgnoreDifferences` — this section is what the rule joins against. Add via a new `argoIgnoreDiffsToObj` helper.

#### 5. Shen rule
**File**: `kernel/r16-selector-needs-ignore-diff.shen`
- `check-r16 SelectorUsages IgnoreDiffEntries ArgoApps Resources` cross-joins usages with their owning app's ignore-diff entries; emits error if the resolved path isn't covered.
- Use `string-contains?` (already in prelude) for a forgiving path match to start; tighten to JQ-path equivalence in a follow-up.

#### 6. Orchestrator
**File**: `kernel/check.shen`
- `(load "r16-selector-needs-ignore-diff.shen")`
- Bind new sections; add `R16 (mark-rule "XPC.E.selector-needs-ignore-diff" (check-r16 …))`.

#### 7. Fixture
**Dir**: `testdata/fixtures/selector-drift/`
- `subnet.yaml` with `subnetSelector: { matchLabels: { … } }` but no `ignoreDifferences` on its Application.
- Positive control: `testdata/fixtures/selector-drift-ok/` with a matching `ignoreDifferences` entry — must produce no diagnostic.

#### 8. Docs
- `docs/obligations.md` Category E: add `selector-needs-ignore-diff` as an implemented generator.
- `cmd/xpc/main.go explain`: new entry.

### Success criteria

#### Automated
- [ ] `make test` passes; new `TestR16_SelectorDrift` covers both positive and negative fixtures
- [ ] Registry has ≥20 entries with godoc citations to the provider CRD doc fields
- [ ] `xpc check testdata/fixtures/selector-drift` exits non-zero with exactly one `XPC.E.selector-needs-ignore-diff`
- [ ] `xpc check testdata/fixtures/selector-drift-ok` exits zero

#### Manual
- [ ] Run against `~/fg/fg-manifold` ApplicationSets known to have correct `ignoreDifferences` — should produce **zero** false positives for those. Document any registry entries that fire incorrectly; tighten path-matching before merge.

**Pause** before S3.

---

## Session 3 — `XPC.A.resource-field-valid` (manifest vs CRD schema)

### Overview
For every `ResourceInfo.Raw` whose `(apiVersion, kind)` matches a known CRD, walk the manifest against the schema and emit one diagnostic per violation: unknown field, wrong type, missing required, wrong enum value. This is the single largest MR-noise closer.

### Changes

#### 1. Extend schema walker
**File**: `pkg/schemas/fetcher.go`
- Add `FieldFacts struct { Type FieldType; Required []string; Enum []interface{}; AdditionalProperties *bool }`.
- Add `ResolveFieldFacts(schema map, path string) FieldFacts` mirroring `ResolveFieldType` but preserving `required`, `enum`, `additionalProperties`.
- Add `ValidateManifest(schema map, manifest map) []FieldViolation` — recursive walker emitting `FieldViolation{Path, Kind: UnknownField|WrongType|MissingRequired|InvalidEnum, Expected, Got}`.
- Keep existing `ResolveFieldType` and `TypeAssignable` untouched (still used by R5).

#### 2. (apiVersion, kind) → schema index
**File**: `pkg/schemas/index.go` (new)
- `BuildSchemaIndex(World) map[SchemaKey]map[string]interface{}` where `SchemaKey{APIVersion, Kind}`.
- Consolidates the ad-hoc map construction that `resolvePatchTypes` does in `bridge.go:243–262`; refactor that caller to use the new index (pure cleanup, no behavior change).

#### 3. Enrichment
**File**: `pkg/ir/field_validation.go` (new enrichment pass, called from `Builder.Build` after CRDs are loaded but before bridge serialization)
- Build schema index once.
- Walk `World.Resources`; for each resource with a matching schema, call `ValidateManifest`.
- Store results as `[]ResourceFieldFact{Owner: ResourceKey, Path, Kind, Expected, Got, Source: SourceLocation}` on `World.ResourceFieldFacts`.

This pre-computation (Go-side) keeps the Shen rule trivial — it just emits a judgment per fact. Same split as R5 (Go resolves, Shen judges).

#### 4. Types
**File**: `pkg/types/types.go`
- Add `ResourceFieldFact` struct, `ResourceFieldFacts []ResourceFieldFact` on `World`.
- Add `ViolationKind` enum string constants.

#### 5. Bridge
**File**: `pkg/checker/bridge.go`
- `resourceFieldFactCmp`, `resourceFieldFactToObj`; `sortedSection("resource-field-facts", …)` in `worldToShenObj`.

#### 6. Shen rule
**File**: `kernel/r17-resource-field-valid.shen`
- `check-r17 Facts` maps one fact → one judgment with a per-kind message template.
- Keep it tiny — the smarts live in Go `ValidateManifest`.

#### 7. Orchestrator
**File**: `kernel/check.shen`
- `(load "r17-resource-field-valid.shen")`, `ResourceFieldFacts (extract-section resource-field-facts Sections)`, `R17 (mark-rule "XPC.A.resource-field-valid" (check-r17 ResourceFieldFacts))`.

#### 8. Fixture
**Dir**: `testdata/fixtures/resource-field-invalid/`
- `crd.yaml` — small hand-written CRD with one required field, one enum, one typed field.
- Four subdirs: `unknown-field/`, `wrong-type/`, `missing-required/`, `invalid-enum/`, each a single manifest exhibiting one violation.

**File**: `pkg/checker/check_test.go` — `TestR17_FieldValidation` runs all four subfixtures.

#### 9. Docs
- `docs/obligations.md` Category A: add `resource-field-valid` generator row.
- `cmd/xpc/main.go explain`.

### Success criteria

#### Automated
- [ ] `make test` passes with all four R17 subfixtures
- [ ] `go test ./pkg/schemas/…` covers `ResolveFieldFacts` and `ValidateManifest` unit tests (≥8 cases: happy path, unknown at depth 1/2/3, enum match/miss, required present/absent, additionalProperties allowed/forbidden)
- [ ] Refactored `bridge.resolvePatchTypes` uses `BuildSchemaIndex` and all existing R5 tests still pass
- [ ] `xpc check testdata/fixtures/resource-field-invalid/unknown-field` exits non-zero with `XPC.A.resource-field-valid`

#### Manual
- [ ] Run against `~/fg/fg-manifold` — inspect top 20 findings. Each must be a real bug (or a schema we haven't loaded) — document any CRD-load gaps, not false positives. If false-positive rate >5%, revisit walker strictness before merge.
- [ ] Replay historical MR `!1186` (LaunchTemplate.privateIpAddresses): checkout the pre-fix tree, run xpc, confirm the rule fires with the right path.

**Pause** before S4.

---

## Session 4 — `XPC.H.helm-renders` + `XPC.H.values-well-typed` (Helm rendering)

### Overview
Shell out to `helm template` for every `ArgoApplication.Source` with `Renderer == RendererHelm`. Merge the rendered manifests into `World.Resources`. Add two rules: one that fires when rendering fails, one that fires when values don't match the chart's `values.schema.json`.

### Changes

#### 1. Renderer abstraction
**File**: `pkg/renderer/renderer.go` (new)
- `type Renderer interface { Render(src types.ArgoSource, workdir string) ([]byte, error) }`.
- `type Source struct { ChartPath string; ValueFiles []string; ValuesInline map[string]interface{}; ReleaseName, Namespace string }`.
- `ResolveChart(src ArgoSource, cwd string) (chartPath string, err error)` — for fg-manifold's case, charts are co-located; resolve `source.path` relative to the cwd. (Remote-repo resolution is deferred.)

#### 2. Helm implementation
**File**: `pkg/renderer/helm.go` (new)
- `HelmRenderer{HelmBin string}.Render(...)` runs `helm template <release> <chart> --namespace <ns> -f values1 -f values2 --set a=b …`.
- Check `helm version` on first use; if absent, surface as a `XPC.H.helm-renders` severity=warning diagnostic pointing at the Application file, rather than crashing.
- Timeout 30s per invocation; kill on timeout with a clear diagnostic.

#### 3. Content-addressed render cache
**File**: `pkg/renderer/cache.go` (new)
- Key = SHA-256 of `(chart-dir-tree hash + sorted values bytes + helm version)`.
- Two-tier in-memory + `~/.cache/xpc/renders/` on disk.
- TTL 15 minutes, same as snapshot cache.

#### 4. Builder integration
**File**: `pkg/ir/builder.go`
- In `addArgoApplication`, after typed parsing, for each Helm source call `renderer.Render` (guarded by a `SkipRender bool` field on `Builder` so existing tests stay hermetic).
- Parse rendered YAML via the same `loader.LoadReader` path; tag each resulting `ResourceInfo.Source` with the Application's file+line so diagnostics still point at something an MR author can find.
- Merge into `World.Resources` with a provenance marker: `ResourceInfo.Provenance = "rendered:helm:<app-name>"`.

#### 5. Types
**File**: `pkg/types/types.go`
- Add `ResourceInfo.Provenance string` (default `"direct"`).
- Add `RenderResult struct { AppName, ChartPath string; Success bool; Error string; ValuesIssues []ValuesIssue }`.
- Add `World.RenderResults []RenderResult`.

#### 6. values.schema.json validation
**File**: `pkg/renderer/values_schema.go` (new)
- If the chart has a `values.schema.json`, run the merged values through it (reuse `ValidateManifest` from S3 — this is why A1 comes first).
- Emit `ValuesIssue{Path, Message}` per violation; carried back on `RenderResult.ValuesIssues`.

#### 7. Bridge
**File**: `pkg/checker/bridge.go`
- New `sortedSection("render-results", …)`.

#### 8. Shen rules
**Files**: `kernel/r18-helm-renders.shen`, `kernel/r19-values-well-typed.shen`
- `check-r18 RenderResults` → error per failed render.
- `check-r19 RenderResults` → error per values-schema issue.

#### 9. Orchestrator
**File**: `kernel/check.shen` — load, bind, `mark-rule` both.

#### 10. CLI flag
**File**: `cmd/xpc/main.go`
- Add `--skip-render` / `--helm-bin=<path>` flags on `check`.
- In `--skip-render` mode, emit an `info` diagnostic listing the skipped Applications (so CI runs without Helm on PATH know what they missed).

#### 11. Fixtures
**Dir**: `testdata/fixtures/helm-render-fail/` — chart with a template syntax error.
**Dir**: `testdata/fixtures/helm-values-mismatch/` — chart with `values.schema.json` requiring `replicas: integer`, values file passing `replicas: "three"`.
**Dir**: `testdata/fixtures/helm-render-ok/` — minimal chart that renders cleanly and produces one Deployment; asserts that Deployment appears in `World.Resources` with correct provenance.

#### 12. Docs
- `docs/obligations.md` Category H: mark `helm-renders` and `values-well-typed` as implemented.
- `cmd/xpc/main.go explain`: both codes.

### Success criteria

#### Automated
- [ ] `make test` passes with all three Helm fixtures; tests skip gracefully when `helm` is not on PATH (using `t.Skip`)
- [ ] `go test ./pkg/renderer/… -count=2` passes — catches cache non-determinism
- [ ] `xpc check --helm-bin=$(which helm) testdata/fixtures/helm-render-ok` exits zero and dumps the rendered Deployment via `xpc dump-ir`
- [ ] `xpc check --skip-render testdata/fixtures/helm-render-ok` exits zero but emits one `info` diagnostic flagging the skip

#### Manual
- [ ] Run `xpc check ~/fg/fg-manifold/deploy/facilitygrid/ops/applications/prod-services-backend` with Helm available; confirm rendered `XFargateService` claims appear in `dump-ir`.
- [ ] Confirm S3's `XPC.A.resource-field-valid` rule now fires on field errors *inside* rendered claims — proving the integration point works end to end. If it doesn't, debug in this session; don't merge a half-integrated renderer.
- [ ] Render cache hit rate on second run >95% (check `~/.cache/xpc/renders/` populated, runtime halves).

**Pause** before S5.

---

## Session 5 — Kustomize + ApplicationSet generator expansion + determinism

### Overview
Close out Category H with Kustomize, add the double-render determinism check, and teach the builder to expand static `ApplicationSet` generators (`list`, `matrix`, `git` with a filesystem `directories` target) into concrete Applications. `pullRequest` and `scmProvider` stubs accept an injected fixture via flag.

### Changes

#### 1. Kustomize renderer
**File**: `pkg/renderer/kustomize.go` (new)
- Parallels `helm.go`; shells `kustomize build <overlay>`.
- Cache-key includes overlay tree hash.

#### 2. `render-deterministic` generator
**File**: `kernel/r20-render-deterministic.shen`
- Render twice; byte-compare output.
- Non-determinism is a warning, not an error (fg-manifold may legitimately have `randAlphaNum` in templates; these are the ones to document).
- Go side computes double-render in `pkg/renderer/determinism.go`; facts surface as `DeterminismResults` on World.

#### 3. ApplicationSet expansion
**File**: `pkg/ir/appset_expand.go` (new)
- `ExpandAppSet(appset ArgoApplicationSet, ctx ExpansionContext) []ArgoApplication`.
- Support by `Kind`:
  - `list` — one Application per `ListElements` entry; template-substitute `{{ key }}` placeholders.
  - `git` (directories) — walk filesystem under `RepoURL`+`Path`, one Application per matching directory.
  - `matrix` — cartesian product of two child generators; supports `list × list`, `list × git`, and `list × pullRequest` (with injected PR list).
  - `merge` — deep-merge two generators by `MergeKeys`.
  - `pullRequest` / `scmProvider` — if `ExpansionContext.PRFixtures[appsetName]` provided, use it; else emit one `info` diagnostic `XPC.H.appset-unsupported-generator` pointing at the AppSet file.
- Expanded Applications feed back into the normal Application pipeline — the `XPC.D.kind-whitelisted` and `XPC.E.selector-needs-ignore-diff` rules from S1/S2 gain coverage automatically.

#### 4. Template substitution
**File**: `pkg/ir/appset_template.go` (new)
- Simple `{{ .Values.foo }}` style substitution over the AppSet template, using each generator element as values. No full Go template engine — escape the non-trivial fraction as an `info` diagnostic and move on.

#### 5. CLI
**File**: `cmd/xpc/main.go`
- `--appset-fixture=<file.yaml>` — YAML map `{ appset-name: [PR stubs...] }`.
- `--skip-appset-expand` — opt out if expansion is slow.

#### 6. Kustomize fixtures
`testdata/fixtures/kustomize-ok/`, `kustomize-render-fail/`.

#### 7. AppSet fixtures
`testdata/fixtures/appset-list/`, `appset-matrix/`, `appset-pullrequest/` (with a stub fixture file).

#### 8. Docs
- `docs/obligations.md` Category H: `kustomize-renders` and `render-deterministic` implemented.
- ADR-003 (new) — "ApplicationSet expansion as offline simulation" — documents the PR-stub fixture approach and the expansion contract.

### Success criteria

#### Automated
- [ ] `make test` passes with Kustomize fixtures
- [ ] `xpc check testdata/fixtures/appset-matrix` materializes the expected cartesian product (verify via `dump-ir` record count)
- [ ] Double-render determinism passes on `helm-render-ok` and `kustomize-ok` fixtures
- [ ] `xpc check --appset-fixture=…` on a `preview-environments`-shaped fixture produces the expected expanded Application count

#### Manual
- [ ] `xpc check --appset-fixture=…` on `~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/preview-environments.yaml` with a 2-PR fixture expands to the expected `4 repos × 2 PRs + static list = N` Applications. Spot-check a few rendered resources against the live cluster's current state.
- [ ] Full-repo run against `~/fg/fg-manifold` completes in <2 minutes with caching warm. If slower, profile in this session.
- [ ] Total coverage assessment: recompute the MR-bucket hit-rate table from the research doc. Target: ≥50% of the ~500-MR history would have been caught.

---

## Cross-cutting testing strategy

### Unit tests
- `pkg/schemas/`: `ResolveFieldFacts`, `ValidateManifest`, `BuildSchemaIndex` — table-driven over hand-rolled schemas.
- `pkg/renderer/`: `HelmRenderer.Render` (mocked via tiny chart), `KustomizeRenderer.Render`, cache key stability, cache hit path.
- `pkg/ir/selector_registry.go`: asserts the registry parses and that every entry's `ResolvedPath` is a plausible dotted path (regex).
- `pkg/ir/appset_expand.go`: each generator kind in isolation, plus matrix × list combination.

### Integration tests
- `pkg/checker/check_test.go`: one test per new fixture. Keep using `loadFixture` / `findDiagByCode` — don't fork the pattern.
- End-to-end fg-manifold replay (manual, per session) — one known MR from each pain category.

### Manual regression gating
After S5, run `xpc check` against three known-good branches of fg-manifold and record diagnostic counts. Any session that increases diagnostic count on a known-good branch needs root-causing before merge — a false-positive regression is as bad as a missed bug.

## Performance considerations

- **Rendering is the cost center.** Shelling `helm template` is ~200–500ms; on a 60-AppSet repo that's 30s+ uncached. The S4 content-addressed cache is what makes repeated runs viable. Budget: cold run ≤2 minutes, warm run ≤20s.
- **Schema index** (S3) is built once per `World`; don't rebuild per-rule.
- **ApplicationSet expansion** is O(generators × elements). Matrix × PR × list can explode; we cap expansion at 1000 Applications per AppSet with a clear `info` diagnostic if exceeded.
- **Content-addressed caches** (S4) share the filesystem dir pattern with existing `pkg/schemas.Cache` (`~/.cache/xpc/schemas/`) — use the same conventions for TTL checks and cleanup.

## Migration / rollout

- **Backwards compatibility**: all new codes are additive. No existing `XPCNNN` code changes semantics.
- **Audit proof version**: bump to v4 *once*, at the start of S3 (when the `World` shape changes enough that v3 proofs become incomparable). After that, each session adds a rule-subtree without a version bump.
- **CI integration docs**: after S5 lands, publish a minimal `gitlab-ci.yml` snippet for fg-manifold's team — `xpc check --format=sarif . > xpc.sarif` + GitLab SAST integration. This was the explicit "useful in CI" bar from the research doc.

## References

- Background research:
  - `thoughts/shared/research/2026-04-18-fg-manifold-target-study.md` — MR pain taxonomy
  - `thoughts/shared/research/2026-04-18-so-what-have-we-actually-built.md` — current xpc architecture tour
  - `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md` — post-cleanup vision
- ADRs:
  - `docs/adr/001-bounded-obligation-taxonomy.md` — 12-category taxonomy; S1/S2/S3/S4/S5 map to D/E/A/H/H respectively
  - `docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md` — the "new rule = Shen rule + new section + enrichment" recipe used by every session
- Key existing code referenced by this plan:
  - `pkg/ir/immutable_registry.go:7` — template for S2's selector registry
  - `pkg/ir/trajectory_extract.go:12` — enrichment pattern used by S2 and S3
  - `pkg/schemas/fetcher.go:101–131` — the walker S3 extends with facts
  - `pkg/checker/bridge.go:331` — `sortedSection` generic; every session uses it
  - `pkg/checker/bridge.go:242–298` — `resolvePatchTypes`, the existing schema-use pattern S3 generalizes
  - `pkg/types/types.go:192–481` — full Argo IR that S1/S2/S4/S5 lean on
  - `kernel/r5-patch-typecheck.shen` — minimal rule-shape every new rule mirrors
  - `kernel/check.shen` — orchestrator; each session edits lines ~16–31 (loads) and ~79–97 (rule calls)
