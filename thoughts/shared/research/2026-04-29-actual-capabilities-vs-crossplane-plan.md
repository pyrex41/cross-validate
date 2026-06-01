---
date: 2026-04-29T14:16:36Z
researcher: Reuben Brooks
git_commit: a4afa06d9335584b2e72e6d9559c23e6b8397fe9
branch: claude/standalone-xpc-cli
repository: cross-validate
topic: "What can cross-validate actually do, and how does it compare to millstonehq/crossplane-plan?"
tags: [research, capabilities, comparison, crossplane-plan, xpc, kernel, plan, snapshot, audit]
status: complete
last_updated: 2026-04-29
last_updated_by: Reuben Brooks
---

# Research: What cross-validate actually does, vs. crossplane-plan

**Date**: 2026-04-29T14:16:36Z
**Researcher**: Reuben Brooks
**Git Commit**: a4afa06d9335584b2e72e6d9559c23e6b8397fe9
**Branch**: claude/standalone-xpc-cli
**Repository**: cross-validate

## Research Question

What does this codebase ACTUALLY DO, end-to-end, and how does it compare and contrast to <https://github.com/millstonehq/crossplane-plan>?

## Summary

**cross-validate** is a single static-analysis CLI binary, `xpc`, that lints
ArgoCD + Crossplane manifests **offline, at lint/CI time**. It parses YAML,
optionally renders Helm/Kustomize/Crossplane Compositions, builds a typed
`World` IR, simulates the ArgoCD sync trajectory in Go, and hands the IR to an
**embedded Shen rule kernel** containing 25 numbered rules (R1–R25, plus R6c,
R26, R27) that emit structured judgments. It can also: snapshot a cluster's
type environment, generate Merkle-tree audit proofs, diff two git refs to
catch destructive plan-time changes, and `git bisect` the first commit where a
specific rule started or stopped firing.

**crossplane-plan** is a different kind of tool: a **Kubernetes operator**
that runs *in-cluster*, watches Crossplane Composite Resources via the K8s
watch API, recognizes PR previews from resource naming
(`pr-{number}-{base-name}`), uses the `crossplane-diff` library to render
expected resource trees, and posts Terraform-Cloud-style diff comments to
GitHub PRs. It depends on ArgoCD + Helm install + GitHub credentials and
operates on live cluster state.

The two tools sit on **opposite sides of the GitOps loop**:

| Axis | cross-validate (`xpc`) | crossplane-plan |
|---|---|---|
| Where it runs | Pre-merge / CI / pre-commit, single Go binary | In-cluster Kubernetes Deployment, leader-elected |
| What it consumes | YAML files on disk + optional `.xpcsnap` of cluster CRDs | Live K8s API watch events |
| What it produces | Diagnostics (SARIF / JSON / JUnit / Markdown / agent-format / LSP) + Merkle audit proofs | GitHub PR comments with rendered resource diffs |
| When it speaks | Before merge — blocks bad changes from landing | After merge to a preview branch — visualizes what will happen |
| Trust model | Deterministic, offline, no cluster access | Depends on cluster state + GitHub token in cluster Secret |
| Coverage scope | 25+ named static invariants over Crossplane/ArgoCD/Helm/Kustomize | Resource diff rendering only — no rule corpus |
| ArgoCD coupling | Reads ArgoCD Applications, AppSets, Projects, sync-waves; does not require ArgoCD installed | Requires ArgoCD installed and managing the resources |
| Resource naming | None | Strict `pr-{N}-{name}` convention |
| Languages | Go (24k LOC) + Shen (~25 rule files) + AOT-compiled shen-go runtime | Go + Helm chart |
| Distribution | `go install`, single binary; embedded kernel via `//go:embed` | Helm chart `millstone/crossplane-plan` |

In short: **cross-validate is a type-checker and policy linter for the IaC
authoring stage**; **crossplane-plan is a preview-comment bot for the deploy
stage.** They do not overlap in functionality. They could complement each
other in the same pipeline (xpc fails the PR before merge if invariants are
broken; crossplane-plan posts the resource-level diff comment alongside).

## Detailed Findings

### 1. The `xpc` CLI surface (`cmd/xpc/main.go`)

A single Go binary with the following subcommands, dispatched in
`cmd/xpc/main.go:54–81`:

| Subcommand | Purpose |
|---|---|
| `xpc check [paths…]` | Run all rules over manifests; emit diagnostics. The flagship verb. |
| `xpc dump-ir <path>` | Print the typed World as an s-expression — the exact input the kernel sees. |
| `xpc snapshot [path]` | Capture / diff a cluster type-environment snapshot (`.xpcsnap` JSON). |
| `xpc verify <proof>` | Recompute the Merkle root of a `.xpcproof` and confirm it matches. |
| `xpc proof show / diff` | Inspect a proof or diff two proofs (newly satisfied / newly violated / unchanged judgments). |
| `xpc bisect --rule=<code> --good=<ref>` | Binary-search the git history for the first commit where a rule changed firing state. |
| `xpc plan --base=<ref> --head=<ref>` | Two-ref delta — emits plan-mode-only `XPC.P.*` diagnostics for destructive deletes / immutable-field changes. |
| `xpc explain <code>` | Print embedded prose explanation of a rule (`cmd/xpc/main.go:1071–1468`). |
| `xpc version` / `xpc help` | Standard. |

Global env vars: `XPC_CPUPROFILE`, `XPC_TIMING`, `XPC_KERNEL_PATH`,
`XPC_CONFIG_PATH` (`cmd/xpc/main.go:41–46, 179–180, 679–680, 811–812`).

Global config: `xpc.yaml`, located by `--config > $XPC_CONFIG_PATH > upward
walk from cwd > exe-dir fallback`. Schema lives in `pkg/config/config.go:26`.
Knobs include `prod-patterns`, `immutable-fields`, `state-bearing-kinds`,
`bypass-annotations`, and `name-carveouts.crossplane-state-needs-orphan`.

Output formats from `xpc check --format=…`: `agent` (default — LLM-dense
key:value blocks), `human`, `json`, `sarif`, `junit`, `lsp`. SARIF is used as
the GitLab SAST artifact (`docs/ci-integration.md`).

### 2. The Shen rule kernel (`kernel/`)

The kernel is the canonical rule book per **ADR-002**
(`docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md`). 25 rule
files plus dispatcher and prelude:

**Versioning & conversion** — R1 (CRD/XRD served & storage flags), R2
(webhook conversion opt-in via `xpc.dev/accept-conversion-webhook`), R8
(v1/v2 machinery field placement), R11 (deprecation calendar — APIs,
provider package versions, unserved CRD versions).

**Crossplane composition wiring** — R3 (`compositeTypeRef` resolves to a
referenceable XRD version), R4 (pipeline `functionRef` resolves +
input-version compatibility), R5 (patch field-type assignability via
`pkg/schemas` resolved types), R9 (bootstrap-time gap, annotation-driven),
R10 (secret-taint propagation from credential-named source paths to
non-secret-sink destinations).

**ArgoCD wave ordering** — R6a (XRD before XR), R6b (Function before
Composition), R6c (Provider before MR), R6d (Composition ≤ XR wave), R7
(label tracking + Composition conflict warning).

**ArgoCD AppProject / AppSet governance** — R15 (resource kind in
`clusterResourceWhitelist` / `namespaceResourceWhitelist`), R24 (cascading
finalizer without `preserveResourcesOnDeletion`), R25 (production-pattern
AppSets must not have automated sync — INC-6).

**Argo↔Crossplane drift** — R16 (selector-resolved fields covered by
`ignoreDifferences`), R21 (late-init fields covered by `ignoreDifferences`),
R22 (Server-Side Apply + narrowed `managementPolicies` — three sub-codes
gated by `--ssa-mp-mode={observe,partial,any}`).

**Crossplane MR lifecycle safety** — R23 (state-bearing kinds — Aurora,
DocDB, MySQL, KMS, S3, VPC — must declare `deletionPolicy: Orphan`; this is
the static-analysis analog of `fg-manifold`'s VAP for INC-6).

**Rendering** — R17 (CRD-schema field validation: unknown-field, wrong-type,
missing-required, invalid-enum, computed by `pkg/schemas.ValidateManifest`),
R18 (Helm/Kustomize/Composition render success), R19 (Helm `values.yaml`
type-checked against `values.schema.json`), R20 (render determinism — double
render byte-compare; warning-only).

**Trajectory-based cross-step rules** — R12 (dangling ConfigMap/Secret mount
across waves), R14 (RBAC binding regression across waves). Both consume the
`(trajectory (step …))` section produced by `pkg/trajectory.Simulate`.

**Plan-mode-only rules** (`pkg/plan/`, ADR-004) — R26 produces
`XPC.P.destructive-delete` and `XPC.P.cascade-risk`; R27 produces
`XPC.P.immutable-change`. Letter `P` is permanently reserved for plan-mode.

Diagnostic codes follow `XPC.<Category>.<Generator>[.<Instance>]` per
**ADR-001** (bounded obligation taxonomy, A–L) — for example
`XPC.S.crossplane-state-needs-orphan` or `XPC.E.appset-finalizer-without-preserve`.
Legacy `XPC001`–`XPC014` numeric codes survive as aliases.

### 3. Go architecture (`pkg/`)

Thirteen packages plus `internal/shenfull` and `kernel/`. End-to-end flow:

```
loader.LoadDirectory  ─►  ir.Builder.Build  ─►  enrichTrajectory + enrichFieldValidation
                                              ▼
                                       (optional) snapshot.Load + mergeSnapshotIntoWorld
                                              ▼
                                       trajectory.Simulate → []Step
                                              ▼
                                       checker.CheckWithObligations
                                         (worldToShenObj → kl.Call check-world)
                                              ▼
                                       []types.Diagnostic
                                              ▼
                              report.ReportStdout(format)  +  audit.GenerateWithRulesetDigest (if --proof)
```

Key packages:

- `pkg/loader` — parallel YAML walker, GOMAXPROCS workers, skips `templates/`
  inside Helm chart dirs. Returns `LoadedDocument{Source, APIVersion, Kind, Raw, Node}`.
- `pkg/ir` — builds `*types.World`. `Builder.Build` classifies each doc,
  parses into typed structs, hashes CRD/XRD schemas, expands AppSet
  generators (offline only — `list`/`git`/`matrix`/`merge`; `pullRequest`/`scmProvider`
  use `--appset-fixture`), runs Helm/Kustomize/`crossplane render`, runs
  determinism double-renders, then `EnrichTrajectoryData` extracts mount-refs,
  SA-refs, RBAC bindings, selector usages, late-init usages, SSA/MP conflicts,
  and `EnrichFieldValidation` validates each resource against its CRD/XRD
  schema. (`pkg/ir/builder.go:85`, `pkg/ir/trajectory_extract.go:20`,
  `pkg/ir/field_validation.go:15`.)
- `pkg/types` — `World` aggregate (`pkg/types/types.go:894`) — fields like
  `CRDs, XRDs, Compositions, Functions, Providers, Resources, ArgoApps,
  ArgoProjects, ArgoAppSets, Schemas`, plus enriched slices `MountRefs,
  SARefs, RBACBindings, RBACRules, ResourceFieldFacts, RenderResults,
  SSAMPConflicts, CPDeletionPolicyFacts, LateInitUsages, SelectorUsages,
  DeterminismResults`, plus knobs `ProdPatterns, NameCarveouts, BypassKeys,
  ImmutableFields, StateBearingKinds, SSAMPMode`.
- `pkg/schemas` — `BuildSchemaIndex(world)`, `ValidateManifest(schema, raw)`,
  `ResolveFieldType` — used both for R17 field-validation facts and for
  R5 patch-type resolution before kernel call.
- `pkg/checker` — Shen bridge. `initShen` runs once per process via
  `sync.Once`; materializes `kernel.FS` (the `//go:embed *.shen` bundle) to a
  content-addressed `$TMPDIR/xpc-kernel-<digest16>/` and `(load "check.shen")`.
  `worldToShenObj` projects the entire World into a Shen s-expression list
  (every typed slice → tagged fact tuple, booleans → lowercase-dashed symbols
  to avoid Shen pattern-match variable capture). `objToDiagnostics` decodes
  the returned `(judgment …)` list to `[]types.Diagnostic`. `RunResult` also
  carries `TotalObligations` / `Satisfied` / `Violated` for the audit proof.
- `pkg/config` — typed `xpc.yaml` reader with overlay resolvers
  (`ResolveProdPatterns`, `ResolveImmutableFields`, `ResolveStateBearingKinds`,
  `ResolveAllowDeleteKeys`, `ResolveCrossplaneStateNeedsOrphanCarveouts`).
- `pkg/renderer` — `helm template` and `kustomize build` shellouts behind a
  two-tier (memory + disk) SHA-256 cache rooted at `~/.cache/xpc/renders/`,
  TTL 15 min. Also exposes `MergedValues` / `ValidateValues` against
  `values.schema.json` (R19 input). `DoubleRenderHelm` / `DoubleRenderKustomize`
  power R20 determinism.
- `pkg/snapshot` — `.xpcsnap` JSON file capturing CRDs, XRDs, Providers
  (Healthy=true), Functions, Configurations, Compositions, Argo tracking
  mode, and Schemas. SHA-256 content-addressed digest, optional signature.
  `FromWorld` / `ToWorld` / `Diff` / `IsStale(15min)`. Consumed by
  `xpc check --snapshot=` to supplement the on-disk manifest set with cluster
  facts not checked into git.
- `pkg/trajectory` — Simulates the ArgoCD sync as a sequence of waves. Each
  `Step{AppName, Wave, Delta, State}` records what was created/deleted at a
  wave and the cumulative resource set after. R6 / R9 / R12 / R14 consume it.
- `pkg/plan` — Two-ref delta. `Run` resolves `--base` / `--head` to git
  worktrees (or accepts pre-existing dirs for hermetic tests), runs
  `loader → ir → checker` on each side, computes `ResourceDelta{Added,
  Removed, Modified}` keyed by `(APIVersion, Kind, Namespace, Name, AppName)`,
  then runs `R26DestructiveDelete` and `R27ImmutableChange` Go-side over the
  delta. Markdown (default) or JSON output.
- `pkg/bisect` — `git rev-list good..bad` + binary-probe loop. Each midpoint
  materializes a detached worktree, calls `XPCCheckDetector` (which spawns
  `xpc check --skip-render --format=json`), tears the worktree down. Returns
  the first commit where the target rule's firing state matches `bad`.
  `ValidateRuleCode` regex: `^XPC(?:\d{3,}|\.[A-Z]\.[a-zA-Z0-9._-]+)$`.
- `pkg/audit` — Merkle proof system. `KnownRuleIDs` is a static inventory of
  every rule the kernel can emit; `Generate` builds per-rule and per-resource
  subtrees, plus a metadata leaf (IR digest, snapshot digest, kernel version,
  ruleset digest, timestamp) and a run-summary leaf (total/satisfied/violated
  counts + obligation IDs). `ComputeEmbeddedRulesetDigest` hashes the
  embedded `kernel.FS` bytes — so the proof binds a specific ruleset content,
  not just a version string. `LoadProof` / `Verify` / `DiffProofs` / `Summary`
  are the query API.
- `pkg/report` — `human, agent, json, sarif, junit, lsp`. `agent` is the
  default — dense LLM-readable structured-text blocks (`reporter.go:187`).

### 4. Shen embedding (`internal/shenfull` + `kernel/embed.go`)

`kernel/embed.go` is a 23-line file that exposes `kernel.FS` (a `//go:embed
*.shen` `embed.FS`) and `kernel.Read(name)`. The 25 rule files plus
`prelude.shen` and `check.shen` ship inside the `xpc` binary; runtime
materialization writes them to a content-addressed temp dir on first use
(`pkg/checker/bridge.go:128–187`) so the rest of the codebase can keep using
the standard Shen `(load "check.shen")` form.

`internal/shenfull/` is the **AOT-compiled shen-go runtime** (~88k lines of
Go across 19 files — this is the bytecode + bootstrap output of running
shen-go's compiler against the upstream Shen sources, not hand-written). It
provides 16 AOT-compiled modules registered in `internal/shenfull/loader.go`:
`TopLevelMain, CoreMain, SysMain, SequentMain, YaccMain, ReaderMain,
PrologMain, TrackMain, LoadMain, WriterMain, MacrosMain, DeclarationsMain,
TStarMain, TypesMain, DictMain, InitMain`. After all 16 modules register,
`installLoadCache()` hooks the parsed-form cache and `(shen.initialise)` runs
to populate runtime tables.

**Cold-start performance** is a known concern. `go.mod` documents this:

> shen-go v1.1.1 ships kernel 41.1 + a Go-native Shen reader + load cache
> that drop xpc cold-check from ~2.5s to ~0.7s.

Two mechanisms drive it:
- `internal/shenfull/load_cache.go` — a `sync.Mutex`-guarded
  `map[loadCacheKey]loadCacheEntry` that reuses parsed forms when a `.shen`
  file's `(size, modTime, sha256)` triple is unchanged. Hooks the kernel's
  `load` symbol via `kl.BindSymbolFunc(symload, kl.MakeNative(primCachedLoad, 1))`.
- `internal/shenfull/native_reader.go` — a Go-native parser for the subset
  of Shen syntax used in xpc's rule files; falls back to the in-Shen
  `read-file` parser combinator when the source isn't supported. The kernel's
  `process-sexprs` pass still runs on the parsed forms to preserve exact
  semantics.

### 5. Test-fixture corpus (`testdata/fixtures/`)

48 fixture directories — each a complete YAML scenario aimed at one or two
rules. Every rule has positive ("violation") and negative ("ok") fixtures
where the rule is non-trivially conditional. Sample naming:

- Rule-targeted positives: `webhook-conversion`, `wave-ordering`, `provider-wave`,
  `xpc006-no-cartesian`, `patch-mismatch`, `appproject-whitelist-miss`,
  `appproject-whitelist-multi`, `crossplane-state-needs-orphan`,
  `appset-finalizer-without-preserve`, `appset-finalizer-with-preserve`,
  `appset-no-finalizer`, `prod-appset-autosync`,
  `selector-drift`, `selector-drift-array`, `selector-drift-ok`,
  `late-init-drift`, `late-init-drift-ok`, `dangling-mount`,
  `dangling-mount-cross-wave`, `dangling-mount-cross-wave-ok`,
  `dangling-mount-dedup`, `rbac-regression`, `rbac-regression-cross-wave`,
  `rbac-regression-cross-wave-ok`, `rbac-regression-role-delete`,
  `resource-field-invalid`, `resource-field-valid-ok`,
  `ssa-mp-observe`, `ssa-mp-partial`, `ssa-mp-ok`.
- Render-targeted: `helm-render-ok`, `helm-render-fail`, `helm-values-mismatch`,
  `helm-values-ref`, `helm-values-ref-missing`, `kustomize-ok`,
  `kustomize-render-fail`, `composition-render-absent`.
- AppSet generators: `appset-list`, `appset-matrix`, `appset-pullrequest`.
- Plan-mode only: `plan-cascade-risk`, `plan-destructive`, `plan-destructive-orphan`,
  `plan-immutable-change`, `plan-immutable-change-bypass`, `plan-immutable-change-ok`.
- Smoke: `basic`.

### 6. Claude Code skills (`skills/`)

Three skills wire the CLI into Claude Code's workflow:

- `xpc-edit.md` — Run on every Crossplane YAML edit. Loop: edit → `xpc check`
  → fix all `error`-severity codes → respond. Reads the `fix:` field in
  agent-format output; uses `ack:` only on explicit user request.
- `xpc-commit.md` — Pre-MR. Run `xpc snapshot --output=.xpcsnap`, then
  `xpc check --proof --snapshot=.xpcsnap`. Block commit if errors remain.
- `xpc-review.md` — MR readiness via proof diff. Run `xpc proof show <p>` and
  `xpc proof diff <before> <after>` to count newly-satisfied / newly-violated
  judgments and surface MR-env vs. prod drift.

### 7. CI integration (`docs/ci-integration.md` + `docs/templates/gitlab-ci.yml`)

Drop-in GitLab job uses `golang:1.22`, installs xpc via `go install`, runs
`xpc check --format=sarif --helm-cache-dir=$XPC_CACHE_DIR . > xpc.sarif`, and
publishes via `artifacts.reports.sast: xpc.sarif`. Triggered on
`merge_request_event` and default-branch commits. `allow_failure: true`
default mirrors GitLab's bundled SAST convention. Exit codes are binary —
`0` = no error-severity findings (warnings allowed), `1` = at least one error
or any operational failure.

## Side-by-side: cross-validate vs crossplane-plan

### What each tool reads

- **cross-validate**: YAML files on disk under one or more given paths.
  Optionally a `.xpcsnap` cluster type-environment snapshot. Optionally an
  AppSet PR/SCM fixture. No live cluster access required. Helm + Kustomize +
  `crossplane` binaries are optional shellouts when rendering is enabled.
- **crossplane-plan**: Live Kubernetes API watch events on Crossplane XRs.
  Production cluster state via the K8s API. GitHub API for posting comments.
  No file-on-disk input mode.

### What each tool checks

- **cross-validate**: 25+ named static invariants — version coherence,
  composition wiring, type-checked patches, sync-wave ordering, AppProject
  whitelisting, AppSet finalizer/autosync hazards, RBAC regression across
  waves, mount-consistency across waves, secret-taint propagation,
  state-bearing-resource orphan policy, render success, render determinism,
  Helm values schema, late-init / selector drift, SSA+managementPolicies
  conflicts, plus plan-mode `XPC.P.*` destructive-delete / cascade-risk /
  immutable-change between two refs.
- **crossplane-plan**: No invariant corpus. The "check" is "render the
  composition and diff it" — managed-resource-level field-by-field diff that
  becomes a PR comment.

### What each tool produces

- **cross-validate**: One of six format-selectable diagnostic streams to
  stdout (default `agent`, also `human`, `json`, `sarif`, `junit`, `lsp`),
  plus optional `check.xpcproof` Merkle audit file, plus optional
  `.xpcsnap` capture file. Exit code 0/1.
- **crossplane-plan**: A formatted GitHub PR comment showing per-managed-
  resource adds/changes/removals.

### How each tool detects "this is a PR"

- **cross-validate**: Doesn't need to. It runs *as* the PR's CI job — it's
  invoked with the PR's working tree. `xpc plan --base=main --head=HEAD`
  produces the two-ref delta when explicit comparison is wanted.
- **crossplane-plan**: Pattern-matches resource names against `pr-{N}-*`
  (the PR detector is the only supported strategy in Phase 2; label /
  annotation strategies are roadmapped Phase 3).

### Trust boundary

- **cross-validate**: Pure file → diagnostic. The Merkle proof binds (IR
  bytes, snapshot bytes, ruleset bytes) to (judgment list, satisfied count)
  — anyone with the proof + the same kernel digest can independently verify
  the run. No secrets. No cluster credentials.
- **crossplane-plan**: Holds GitHub credentials in a cluster Secret
  (`github-creds`). Posts comments under that token. Requires leader
  election (HA) and write access to the live cluster.

### Operational dependencies

- **cross-validate**: Go 1.25, `helm` (optional, for R18/R19/R20), `kustomize`
  (optional), `crossplane` (optional, for composition-render). No K8s cluster.
  No GitHub. No ArgoCD installation — just consumes ArgoCD YAML.
- **crossplane-plan**: K8s cluster, Crossplane installed, ArgoCD installed
  and managing resources, Helm 3, GitHub repo + token, and resources
  must follow `pr-{N}-{base}` naming. Currently GitHub-only.

### Where each tool lives in the GitOps loop

```
  developer edits YAML
        │
        ▼
  ┌─────────────────────────┐
  │ xpc-edit (xpc check)    │ ── cross-validate, locally in editor / Claude
  └─────────────────────────┘
        │
        ▼
  developer commits + opens PR
        │
        ▼
  ┌─────────────────────────┐
  │ CI: xpc check --sarif    │ ── cross-validate, gates the PR
  │ + xpc plan --base=main   │
  └─────────────────────────┘
        │
        ▼  (PR merged or auto-deployed to preview)
        │
  ┌─────────────────────────┐
  │ ArgoCD applies to        │
  │ preview cluster          │
  └─────────────────────────┘
        │
        ▼
  ┌─────────────────────────┐
  │ crossplane-plan watches  │ ── posts diff comment back to PR
  │ XRs, renders composition │
  │ tree, posts to GitHub    │
  └─────────────────────────┘
```

The two are **complementary**, not competitive. xpc says "this set of
manifests is internally consistent and follows policy." crossplane-plan says
"this is what the cluster will actually create when it tries."

### Capabilities that exist in one and not the other

**Only in cross-validate:**
- Composition patch type checking (R5)
- Sync-wave ordering rules (R6 family)
- AppProject `clusterResourceWhitelist` enforcement (R15 / `XPC.D`)
- AppSet finalizer + preserve guard (R24, "the INC-6 rule")
- Production-named AppSet automated-sync guard (R25)
- State-bearing-MR orphan-deletion policy (R23 / `XPC.S`)
- SSA + managementPolicies conflict detection (R22 family)
- Selector / late-init `ignoreDifferences` coverage (R16, R21)
- Helm `values.schema.json` validation (R19)
- Render determinism check (R20 — double render byte-compare)
- Trajectory-based dangling-mount and RBAC-regression checks (R12, R14)
- Webhook-conversion explicit acknowledgment (R2)
- API deprecation calendar (R11)
- Merkle audit proof generation, verification, and proof-vs-proof diff
- Cluster-snapshot capture / diff / staleness checks
- Bisect-by-rule-code over git history
- ApplicationSet generator expansion (offline list/git/matrix/merge)
- Six diagnostic output formats including SARIF for GitLab SAST and LSP for
  editor integrations

**Only in crossplane-plan:**
- Live Kubernetes API integration (watch loops, leader election)
- Real composition rendering against live providers via `crossplane-diff`
- Auto-posted GitHub PR comments with rendered diffs
- ArgoCD ApplicationSet-driven preview environment lifecycle
- Helm-installable in-cluster operator
- Drift detection between PR-preview state and production resources

## Code References

- `cmd/xpc/main.go:54–81` — top-level subcommand dispatch
- `cmd/xpc/main.go:167–230` — `xpc check` flag parsing
- `cmd/xpc/main.go:1071–1468` — `xpc explain` embedded prose
- `pkg/checker/bridge.go:60` — `shenOnce.Do` kernel bootstrap
- `pkg/checker/bridge.go:128–187` — content-addressed kernel materialization
- `pkg/checker/bridge.go:282` — `kl.Call(check-world, worldObj)` entry
- `pkg/ir/builder.go:85` — `Builder.Build` entry
- `pkg/ir/trajectory_extract.go:20` — `EnrichTrajectoryData`
- `pkg/ir/field_validation.go:15` — `EnrichFieldValidation`
- `pkg/audit/proof.go:23` — `Proof` struct
- `pkg/audit/proof.go:135` — `KnownRuleIDs` static inventory
- `pkg/audit/proof.go:598` — `ComputeEmbeddedRulesetDigest`
- `pkg/plan/runner.go:65` — `plan.Run`
- `pkg/plan/r26.go:38` — destructive-delete logic
- `pkg/bisect/bisect.go:130` — bisect main algorithm
- `pkg/snapshot/snapshot.go:94` — content-addressed digest
- `kernel/check.shen:65–135` — `check-world` rule dispatcher
- `kernel/prelude.shen:9–36` — fact-schema vocabulary documentation
- `internal/shenfull/loader.go:23–67` — 16-module AOT bootstrap
- `internal/shenfull/load_cache.go:38–61` — load-cache hook
- `kernel/embed.go:12` — `//go:embed *.shen`
- `go.mod` — pinned `github.com/pyrex41/shen-go v1.1.1` fork (load-cache + native reader)

## Architecture Documentation

- **ADR-001** (`docs/adr/001-bounded-obligation-taxonomy.md`) — 12 fixed
  obligation categories A–L. New categories require a new ADR.
- **ADR-002** (`docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md`)
  — Substrate split: Shen owns rule semantics; Go owns parsing, IR,
  trajectory simulation, and the `worldToShenObj` bridge. Supersedes
  ADR-001 §3 (no Go-side rule registry).
- **ADR-003** (`docs/adr/003-appset-expansion.md`) — Offline AppSet
  expansion in `pkg/ir`; remote-API generators use a fixture file, never
  live API calls.
- **ADR-004** (`docs/adr/004-p-prefix-diagnostic-codes.md`) — Letter `P`
  reserved for plan-mode-only rules. `XPC.P.*` codes never appear from
  `xpc check`, only from `xpc plan --base --head`.

## Historical Context (from thoughts/)

- `thoughts/shared/research/2026-04-17-full-codebase-review.md` — earlier
  audit of the full codebase
- `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md`
  — vision recap
- `thoughts/shared/research/2026-04-18-fg-manifold-target-study.md` — study
  of the fg-manifold target repo that motivates many rules
- `thoughts/shared/research/2026-04-18-so-what-have-we-actually-built.md` —
  prior framing of the same "what do we actually do" question
- `thoughts/shared/research/2026-04-21-vision-status-after-r21.md` — status
  after R21 landing
- `thoughts/shared/research/2026-04-22-inc6-coverage-gap.md` — INC-6 / R23–R25
  coverage gap analysis
- `thoughts/shared/research/2026-04-28-perf-floor-and-aot-tradeoffs.md` —
  cold-start performance floor and AOT trade-offs

## Related Research

See the prior research files listed above. This document supersedes the
2026-04-18 "so what have we actually built" snapshot by adding the R22–R27
landings, the audit/proof system, the bisect verb, the AOT/load-cache
performance work, and explicit comparison to crossplane-plan.

## Open Questions

- `crossplane-plan` is GitHub-only and roadmaps GitLab/Bitbucket support.
  cross-validate's GitLab CI integration is a SARIF artifact and is already
  multi-VCS by virtue of being a CLI. There is no path that integrates
  cross-validate's diagnostics into a PR-comment style surface analogous to
  crossplane-plan's GitHub comment — the question of whether to do so (in
  GitLab MR Notes, etc.) is unaddressed in current docs.
- The plan-mode `xpc plan` produces two-world delta detection but does not
  render Crossplane composition trees against a live provider — the
  rendered-diff capability that crossplane-plan provides is not in scope and
  there is no kernel rule that imitates it.
- `crossplane-plan`'s preview-environment workflow assumes
  `managementPolicies: ["Observe"]` for read-only safety. cross-validate's R22
  `XPC.E.ssa-managementpolicies-observe` warns *against* the same combination
  in a different context (SSA + Observe-only is unsafe with ArgoCD's apply
  loop). Whether these two views can be reconciled in a single repo's
  policy is not addressed.
