# xpc — type checker for Argo CD + Crossplane

`xpc` is a static analyzer that catches the failure modes that happen when
Argo CD and Crossplane share a cluster. It runs offline at lint/CI time:
no operator to deploy, no cluster credentials, no controller in the data
path. Point it at a directory of YAML and it returns diagnostics tagged
with file/line, fix recipes, and stable diagnostic codes.

The single binary covers four jobs:

- **`xpc check`** — lint manifests against 30 rules across 14 obligation
  categories.
- **`xpc plan`** — diff two refs (or `.xpcsnap` files), flag destructive
  changes, post the result as a GitLab MR or GitHub PR comment.
- **`xpc snapshot`** — capture the type environment of a live cluster as
  a portable artifact (`.xpcsnap`).
- **`xpc bisect`** — find the commit that flipped a specific rule.

## Why this exists

Argo CD and Crossplane don't share a model of "who owns this field." Argo
diffs against the manifest and tries to revert anything that drifted;
Crossplane writes status, late-init fields, and resolved selectors after
its own controllers run. Without explicit `ignoreDifferences` coverage the
two fight each other forever. Without `deletionPolicy: Orphan` on
state-bearing resources, an Argo cascade delete runs a real destructive
call against AWS/Aurora/KMS/etc. — the [INC-6 failure
shape](docs/inc-6.md).

The other half of the project is bounded obligation accounting: every
diagnostic traces to one of 14 categories defined in
[`docs/obligations.md`](docs/obligations.md), so the tool can make a
defensible "everything in category X is checked" claim instead of being an
ad-hoc grab bag.

## Install

Build from source:

```sh
git clone https://lab.facilitygrid.net/facility-grid/cross-validate.git
cd cross-validate
go build -o xpc ./cmd/xpc
```

Or grab a prebuilt binary from a release (linux/amd64, linux/arm64,
darwin/arm64) — these are what CI consumes:

```sh
# inside the lab.facilitygrid.net network (Internal project — needs a token)
curl -fsSL --header "PRIVATE-TOKEN: $GITLAB_TOKEN" \
  https://lab.facilitygrid.net/facility-grid/cross-validate/-/releases/v0.2.4/downloads/xpc-linux-amd64 \
  -o xpc && chmod +x xpc
```

Releases are cut with `make release-gitlab` (see [Development](#development)).

Go 1.25+. No runtime deps for `xpc check` on already-rendered manifests.
Optional binaries on `$PATH` enable extra passes:

- `helm` — Helm chart rendering for `Application.spec.source.helm`
- `kustomize` — Kustomize render
- `crossplane` — Composition rendering (used by R20)

Absent binaries degrade to a single info-severity diagnostic per skipped
Application rather than failing the run.

## Quickstart

Lint a directory of Argo + Crossplane manifests:

```sh
./xpc check path/to/manifests
```

Run without external renderers (fastest, recommended for first try):

```sh
./xpc check --skip-render path/to/manifests
```

Sample diagnostic (`--format=human`):

```
XPC.S.crossplane-state-needs-orphan cluster-instance.yaml:1
  rule:     ClusterInstance/aurora-preview-cluster-instance-1 is a state-bearing
            Crossplane managed resource without deletionPolicy: Orphan
  severity: error
  problem:  spec.deletionPolicy is absent (Crossplane default is Delete). Group
            rds.aws.upbound.io, Kind ClusterInstance is in the state-bearing
            allowlist (Aurora, DocDB, MySQL, KMS, S3, VPC). Default Crossplane
            deletion will run a real destructive call against the external
            object. This is the INC-6 failure mode.
  fix:      Set spec.deletionPolicy: Orphan on this resource. If destruction is
            genuinely intended (e.g. throwaway test), add annotation
            xpc.io/allow-delete=true OR policy.facilitygrid.io/allow-delete=true
            to bypass.
  docs:     https://xpc.dev/errors/XPC.S.crossplane-state-needs-orphan
```

Look up any diagnostic code:

```sh
./xpc explain XPC.S.crossplane-state-needs-orphan
```

## Commands

| Command | Purpose |
|---------|---------|
| `xpc check <path>` | Run all rules against a manifest tree. Exit 1 if any error-severity diagnostic fires. |
| `xpc plan --base=<ref> --head=<ref> <path>` | Diff two refs (git refs, directories, or `.xpcsnap` files); flag destructive changes; optionally post to GitLab/GitHub. |
| `xpc snapshot --include-resources --output=<file> <path>` | Capture a type environment (and optionally live resource instances) as a portable `.xpcsnap` artifact. |
| `xpc bisect --rule=<code> --good=<ref> --bad=<ref>` | Run `xpc check` across a git range to find the commit that flipped a rule. |
| `xpc proof show <proof-file>` / `xpc proof diff` | Inspect machine-readable proof artifacts (`xpc check --proof`). |
| `xpc dump-ir <path>` | Print the intermediate representation `xpc` builds from your manifests — useful for debugging rule input. |
| `xpc skill install [target]` | Drop an embedded agent skill into `<target>/.agents` + `.claude/`. |
| `xpc explain <code>` | Print the docs for a diagnostic code. |
| `xpc verify <proof-file>` | Replay a proof and confirm it still holds. |

Top-level help: `./xpc --help`. Per-command flags: `./xpc <cmd> --help`.

## Output formats

`xpc check --format=<fmt>` (full table in
[`docs/ci-integration.md`](docs/ci-integration.md)):

| Format | When to use |
|--------|-------------|
| `agent` | Default. Dense, LLM-readable. |
| `human` | Local dev. Pretty-printed with source excerpts and fix hints. |
| `json` | Custom tooling. Raw diagnostic array. |
| `sarif` | GitLab SAST, GitHub Code Scanning. Auto-ingested as MR/PR annotations. |
| `junit` | Jenkins / CircleCI test reports. |
| `lsp` | Future editor plugins. |

Exit codes: `0` clean, `1` at least one error-severity diagnostic OR setup
failure. CI consumers should read the SARIF report to distinguish the two.

## What it checks

30 rules, grouped by the 14-category obligation taxonomy (see
[`docs/obligations.md`](docs/obligations.md) for the full mapping; ADRs
[001](docs/adr/001-bounded-obligation-taxonomy.md) and
[002](docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md) for
why it's structured this way). Highlights:

- **Schema** (A) — CRD field validity, patch source/target compatibility,
  v1/v2 machinery placement.
- **References** (B) — Composition → XRD, Pipeline step → Function, patch
  source → field, and `providerConfigRef` (R28) all resolve.
- **Versions** (C) — exactly one storage version, all served, XRD
  referenceable.
- **AppProject constraints** (D) — managed kinds pass the project
  whitelist (R15), source repo + destination allowed, sync windows.
- **Patches & secret flow** (R10–R12, R30) — secret taint propagation, API
  deprecation, dangling mounts, ExternalSecret store resolution.
  (Immutable-field enforcement moved to the plan-side R27; R13 is retired.)
- **RBAC drift** (R14) — managed RBAC roles don't lose permissions
  silently across waves.
- **Argo↔Crossplane drift** (R16, R21) — selectors / late-init fields
  covered by `ignoreDifferences` (including AppSet template propagation —
  fixed in `793e12a`).
- **Render correctness** (R17–R20) — Helm/Kustomize/Composition render
  cleanly and deterministically.
- **Server-side apply safety** (R22) — managementPolicies × SSA
  combinations that don't corrupt field ownership.
- **[INC-6](docs/inc-6.md) floor** (R23–R25) —
  `crossplane-state-needs-orphan`, `appset-finalizer-without-preserve`,
  `prod-appset-autosync`. Run alone with `--focus=inc6-floor`.
- **Env/label wiring** (E) — `fargate-claim-env-label` (R29) keeps Fargate
  claim env labels consistent.
- **Convergence** (M) — the reconcile-storm rules
  `forprovider-canonical-form` (R31), `observed-desired-fixed-point` (R32),
  and `duplicate-env-key` (R33): they catch non-canonical `forProvider`
  spec that upjet rewrites on every reconcile, driving an endless update
  loop.

## CI integration

`xpc` is designed to run in CI. The
[`docs/ci-integration.md`](docs/ci-integration.md) guide covers SARIF
ingestion for GitLab SAST and GitHub Code Scanning, exit-code semantics,
and how to wire `xpc plan --post-comment` to write MR/PR comments without
giving the tool a token. The
[`docs/preview-diffs.md`](docs/preview-diffs.md) doc covers the offline
preview-diff flow that replaces the in-cluster
[crossplane-plan](https://github.com/crossplane-contrib/crossplane-plan)
operator for CI use.

## Architecture

```
manifests/  →  loader  →  IR  →  bridge (Go)  →  kernel (Shen)  →  diagnostics
                                     ↓                ↓
                              precomputed       30 rule modules
                              fact tables       (kernel/r*.shen)
```

- **Loader** (`pkg/loader`) parses YAML, optionally runs Helm / Kustomize
  / Crossplane composition rendering.
- **IR** (`pkg/ir`) builds a typed World: CRDs, XRDs, Compositions,
  Applications, ApplicationSets (post-generator-expansion), managed
  resources.
- **Bridge** (`pkg/checker/bridge.go`) precomputes joined facts —
  selector-usage tables, ignore-diff coverage, trajectory enrichment —
  out of the kernel runtime.
- **Kernel** is a set of [Shen](https://shenlanguage.org/) rule modules
  (`kernel/r*.shen`) that consume precomputed facts and emit judgments.
  Shen is the canonical spec, not a performance layer; see
  [ADR-002](docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md).
- **Reporter** (`pkg/report`) serialises diagnostics to the requested
  output format.

Performance: the `dc8e900` perf commit hoists hot joins into the bridge
and gives the kernel a thin "format judgments" shim; the resulting
speedup on a 670-file fixture is ~3× wall-clock with bit-for-bit
identical diagnostics. Profile any run with `--profile-rules
--profile-out=<path>`. Demo:
[`thoughts/shared/demos/perf-precompute-rule-joins-demo.md`](thoughts/shared/demos/perf-precompute-rule-joins-demo.md).

## Project layout

```
cmd/xpc/            CLI entry + per-command dispatch
kernel/             Shen rule modules (r1–r33) + prelude + check
pkg/
  loader/           YAML + Helm/Kustomize/Composition rendering
  ir/               World construction, AppSet generator expansion
  checker/          Bridge: precomputation, kernel invocation, result
  trajectory/       Pre-sync simulation of Argo wave ordering
  plan/             plan-time diff, post-comment dispatch
  snapshot/         .xpcsnap capture + load
  report/           output formatters (human/agent/json/sarif/...)
  bisect/           git-range driver for `xpc bisect`
  schemas/          embedded CRD/XRD index
docs/               obligation taxonomy, CI integration, ADRs
testdata/fixtures/  ~50 named fixtures, one per rule shape
thoughts/           research notes, plans, executable demos
```

## Demos

Executable docs in `thoughts/shared/demos/` (each one re-runs and
self-verifies via `showboat verify`):

- [`perf-precompute-rule-joins-demo.md`](thoughts/shared/demos/perf-precompute-rule-joins-demo.md)
  — before/after benchmark of the bridge hoist.
- [`appset-ignorediff-r16-demo.md`](thoughts/shared/demos/appset-ignorediff-r16-demo.md)
  — R16/R21 AppSet template `ignoreDifferences` propagation.
- [`preview-diffs-demo.md`](thoughts/shared/demos/preview-diffs-demo.md)
  — end-to-end offline preview-diff flow for Crossplane MRs.

## Development

```sh
make test       # go test ./... -count=1
make lint       # go vet + gofmt
make build      # go build ./...
make release-gitlab  # build binaries + publish a GitLab release (tag must be pushed)
```

`make release-gitlab` reads the version from `cmd/xpc/main.go`, cross-compiles
linux/amd64, linux/arm64, and darwin/arm64, uploads them to the project's
generic Package Registry, and creates the matching GitLab release. Auth via
`GITLAB_TOKEN` or the `glab` CLI token; see
[`scripts/release-gitlab.sh`](scripts/release-gitlab.sh).

Run a single rule's tests:

```sh
go test ./pkg/checker/ -run TestR16 -count=1
```

The `replace` directive in `go.mod` points
[`tiancaiamao/shen-go`](https://github.com/tiancaiamao/shen-go) at the
[`pyrex41/shen-go`](https://github.com/pyrex41/shen-go) fork (v1.1.1), which
ships kernel 41.1 + a Go-native Shen reader + load cache. That fork drops
cold-check from ~2.5s to ~0.7s. We'll revert to upstream once the patch
lands there.

## License

TBD.
