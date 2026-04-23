# xpc → Variant-Diff + Composition Execution: Multi-Session Implementation Plan

> Draft plan. Justification: `thoughts/shared/research/2026-04-22-inc6-coverage-gap.md`.
> Related prior plan: `thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md` (S1–S5, landed; R15–R21).

## Context

INC-6 (fg-synapse, 2026-04-22 postmortem) and a re-read of the charter docs
(`2026-04-17-vision-recap-after-phase1-cleanup.md`,
`2026-04-21-vision-status-after-r21.md`) made the scope drift explicit: xpc was
always supposed to be a **variant-aware CLI gate that simulates what will happen
across time**, not a single-tip static linter. R15–R21 added static coverage —
22 rules, 10 of 12 categories, ~77% paper coverage of fg-manifold MR history —
but left the dynamic invariants (F-category `no-dangling-mount`,
`no-immutable-change`, `no-rbac-regression`; R13; cross-variant diff) unbuilt.

`millstonehq/crossplane-plan` realizes the dynamic capability class on a live-
cluster substrate: variant diff, composition function execution, deletion-
centric output, PR-comment gate. This plan adds the same capability class to
xpc on xpc's own file-based substrate. INC-6 coverage falls out naturally.

## Scope

Four phases, self-contained, each leaving main green.

1. **P1 — Static floor (R23/R24/R25).** Three file-level rules mirroring the
   admission policy + AppSet hardening shipped in fg-manifold today. Fast ROI,
   reuses the existing bridge pattern, pays for itself before the capability
   layer lands.
2. **P2 — `xpc plan --base --head` skeleton + variant diff + R26.** Adds the
   two-ref runner, resource-identity delta computation, JSON + Markdown plan
   output, and the first delta-aware rule (`XPC.P.destructive-delete`). No
   composition execution yet — uses the World as produced by the existing
   Helm/AppSet pipeline.
3. **P3 — Composition function execution via `crossplane render`.** Wrap
   `crossplane render` the same way R18 wraps `helm template`: absent-binary
   sentinel, content-addressed cache, rendered MRs flow into `World.Resources`
   with `Provenance = "rendered:composition:<xr>"`. Enables R26 coverage of
   Crossplane-authored downstream resources.
4. **P4 — Trajectory-aware rules (separate follow-up plan).** `no-dangling-mount`,
   `no-immutable-change`, `no-rbac-regression`, R13 multi-snapshot. Not part
   of this plan's landing; flagged as the natural next plan cycle once P3 ships.

## What we're NOT doing

- **No live-cluster features.** No kubedock sidecar, no ArgoCD watch, no PR
  comment posting. xpc stays a local CLI. Markdown output is written to stdout
  / file; CI integrates by piping into `gh pr comment`, etc.
- **No vendor of crossplane-plan.** We're transplanting capabilities, not code.
- **No Crossplane library dependency in-process** for P3. We shell out to the
  existing `crossplane` CLI's `render` subcommand. Direct gRPC to function
  containers is a P3+ optimization, not a P3 requirement.
- **No replacement of R18.** Helm rendering stays. Composition rendering is
  additive.
- **No new obligation category.** R23 goes into S (Safety / state-preservation,
  new sub-bucket under existing), R24/R25 into E, R26 into a new **P** prefix
  (Plan/variant-delta) that's a reporting axis, not a taxonomy category.
- **No ADR rewrite for the `P` prefix yet.** Ship the capability; document the
  ADR once the shape is settled (pattern from ADR-003).

## Implementation approach (per phase)

Each phase ends with green tests, a mergeable PR, and a replay against the
3 fg-manifold tips (441fb679a / 2ca71f228 / 4dd584566) following the v4/v5
protocol.

---

## Phase 1 — Static floor (R23/R24/R25)

### P1.a — R24: AppSet finalizer without preserve

Smallest surface; the literal INC-6 trigger. No annotation bypass plumbing.

**IR side (Go):**
- Verify `pkg/types.ArgoApplicationSet` carries `spec.template.metadata.finalizers`
  and `spec.syncPolicy.preserveResourcesOnDeletion`. Add if absent.
- `pkg/ir/builder.go`: populate these fields during AppSet parse.
- `pkg/checker/bridge.go`: new World section `appset-finalizer-facts` emitting
  `(appset-finalizer-fact Name Finalizers PreserveOnDeletion Source)`.

**Kernel side (Shen):**
- `kernel/r24-appset-finalizer-without-preserve.shen` — for each AppSet, if
  `resources-finalizer.argocd.argoproj.io` ∈ Finalizers and PreserveOnDeletion ≠ true,
  emit `XPC.E.appset-finalizer-without-preserve` error.
- Wire into `kernel/check.shen` load list + extract-section block.

**Fixtures:**
- `testdata/fixtures/appset-finalizer-without-preserve/` (positive; triggers).
- `testdata/fixtures/appset-finalizer-with-preserve/` (negative; silent).
- `testdata/fixtures/appset-no-finalizer/` (negative; silent).

**Tests:** `TestR24_AppSetFinalizerWithoutPreserve_*` in `pkg/checker/check_test.go`.

**Docs:** Add `XPC.E.appset-finalizer-without-preserve` to `docs/obligations.md`;
add `xpc explain` entry.

### P1.b — R23: state-bearing Crossplane kinds require `deletionPolicy: Orphan`

Largest static surface. Introduces the bypass-annotation pattern that R-future
rules can reuse.

**IR side (Go):**
- New registry `pkg/ir/state_bearing_registry.go`: hand-curated kind allowlist
  from `crossplane-state-require-orphan.yaml`, frozen in source.
- Extract `spec.deletionPolicy` and the two bypass annotations during
  resource parse in `pkg/ir/builder.go`.
- New World section `crossplane-deletion-policy-facts` emitting
  `(cp-deletion-policy-fact GroupKind Name Namespace DeletionPolicy AllowDeleteAnn Source)`.

**Kernel side (Shen):**
- `kernel/r23-crossplane-state-needs-orphan.shen`.
- Kind allowlist hardcoded in Shen (source-of-truth stays in Go registry;
  kernel mirrors it — same pattern as the immutable registry).
- Bypass check: if annotation `xpc.io/allow-delete` = "true" OR
  `policy.facilitygrid.io/allow-delete` = "true", silent.
- Name carve-out: if name contains `alb-logs`, silent.

**Fixtures:** Cover positive (default DeletionPolicy), negative-Orphan,
negative-bypass-annotation (both primary and alias), negative-alb-logs.

**Tests:** `TestR23_*` in `pkg/checker/check_test.go`.

### P1.c — R25: configured AppSets should not enable auto-sync

Smallest of the three once R24's AppSet plumbing exists.

**IR side (Go):**
- Extend `appset-finalizer-facts` section or add `appset-autosync-facts`
  carrying `(appset-autosync-fact Name Automated Source)`.
- Kernel config: new field `prodAppSetNamePatterns []string` in the kernel
  config file, default `["-prod", "prod-"]`.

**Kernel side (Shen):**
- `kernel/r25-prod-appset-autosync.shen`. If any pattern substring-matches
  the AppSet name AND `Automated` is truthy, emit `XPC.E.prod-appset-autosync`.

**Replay P1:** expected signal on main —
R23: 0 diags (already-remediated via `3381604e1`); R24: ~5 diags on
`crossplane-platform-aws-*` AppSets; R25: 0 diags post-`a5f77a3b8`.

### P1 exit criteria

- Three rules, three rule files, three fixtures, three tests.
- Replay against the 3 fg-manifold tips shows counts matching prediction.
- Bypass-annotation plumbing reusable (document in a new `docs/patterns.md`
  section — first reuse is in P2/R26).

---

## Phase 2 — `xpc plan` + R26

### P2.a — Two-ref runner

**New CLI surface:** `xpc plan --base=<ref> --head=<ref> [path]`
- Flags accept git refs, worktree paths, or arbitrary directories (for
  hermetic tests).
- Implementation: `git worktree add` under `$TMPDIR/xpc-plan-<hash>/{base,head}`,
  run the existing `Check()` pipeline against each, capture two `*types.World`
  snapshots and their diagnostic sets.
- Cleanup registered with `defer`, panic-safe.

**New types:**
- `types.Plan { Base, Head VariantResult; Delta ResourceDelta; Diagnostics []Diagnostic }`
- `types.VariantResult { Ref string; World *types.World; Diagnostics []Diagnostic }`
- `types.ResourceDelta { Added, Removed, Modified []ResourceChange }`
- `types.ResourceChange { Identity ResourceIdentity; BaseSource, HeadSource string }`

**Identity key:** `(apiVersion, kind, namespace, name, app-name)` —
`app-name` is required to disambiguate two apps that happen to own a same-named
resource. Match semantic of `Provenance`.

### P2.b — Delta computation

**Pure function** `pkg/plan/diff.go`:
- `Diff(base, head *types.World) types.ResourceDelta`
- Builds two `map[ResourceIdentity]ResourceInfo`; set-difference for
  Added/Removed; deep-equal on `Raw` YAML for Modified.
- Determinism: sort keys. Unit-testable without any Shen/kernel involvement.

### P2.c — R26 destructive-delete detection

**New World section** surfacing the Delta into Shen:
`(plan-delta (added …) (removed …) (modified …))`. Present only when xpc
is invoked via `plan`; under `check`, section is empty.

**Kernel side:**
- `kernel/r26-destructive-delete.shen`.
- For each entry in `removed`: if `GroupKind` is in the R23 state-bearing
  allowlist AND the base source shows `deletionPolicy` ≠ `Orphan` (or absent),
  emit `XPC.P.destructive-delete` error.
- For each entry in `removed`: if kind is `argoproj.io/Application` and the
  base manifest carries `resources-finalizer.argocd.argoproj.io` finalizer
  without `preserveResourcesOnDeletion: true`, emit `XPC.P.cascade-risk` error.
- Bypass: respect `xpc.io/allow-delete` / `policy.facilitygrid.io/allow-delete`
  on the *base* side of the delta.

### P2.d — Plan output formats

**JSON** (`--format=json`): wrap the existing diag JSON in a `plan` envelope:

```json
{
  "base": "main",
  "head": "HEAD",
  "delta": { "added": 3, "removed": 2, "modified": 11 },
  "destructive": [ /* R26 diags with Identity + reason */ ],
  "diagnostics": { "base": [...], "head": [...] }
}
```

**Markdown** (`--format=markdown`): PR-comment-shaped output —

```
## xpc plan: main → HEAD

### ⚠ Destructive changes (2)

- `rds.aws.upbound.io/Cluster aurora-prod-cluster` would be removed.
  `deletionPolicy: Delete` (not Orphan). See INC-6.
- `ec2.aws.upbound.io/VPC fg-prod-vpc` would be removed.
  `deletionPolicy` not set (defaults to Delete).

### Other changes

- Added: 3 resources
- Modified: 11 resources
- Removed (non-destructive): 0 resources

### Diagnostics

- base: 0 errors, 41 info
- head: 0 errors, 41 info
```

**Exit code** under `plan`: non-zero iff destructive section is non-empty
(configurable via `--plan-severity-threshold=error|warn|none`).

### P2 exit criteria

- `xpc plan --base=HEAD~1 --head=HEAD /path/to/testdata/fixtures/plan-destructive`
  produces a stable JSON + Markdown output.
- Tests include a fixture where a state-bearing resource is removed across
  the two refs and R26 fires.
- Replay: pick two adjacent fg-manifold tips and run `xpc plan` across them.
  Expected: mostly empty delta; documented as baseline.

---

## Phase 3 — Composition function execution

### P3.a — `crossplane render` wrapper

Mirror `pkg/renderer/helm.go` structure. New file `pkg/renderer/composition.go`:

- `RenderComposition(xr XR, comp Composition, funcs []FunctionBinding, ctx) ([]Resource, error)`
- Shells out to `crossplane render <xr.yaml> <composition.yaml> <functions.yaml>`.
- Absent-binary sentinel: if `crossplane` is not on PATH, emit info diag
  `XPC.H.composition-render-skipped` and return empty. Same pattern as
  missing helm.
- Output is YAML stream; parse into `[]ResourceInfo` with
  `Provenance = "rendered:composition:<xr-name>"`.

### P3.b — Cache integration

Reuse `pkg/renderer/cache.go`'s two-tier SHA-256 scheme:
- Cache key: `sha256(xr.Raw || comp.Raw || sorted(func-image-list))`.
- Disk tier: `~/.cache/xpc/compositions/<hash>.yaml`.
- Memory tier: process-local `map[hash]*[]ResourceInfo`.

### P3.c — World integration

- Loader surfaces XR/Composition pairs that exist in the repo
  (standalone XR fixtures or claims resolved via XRD).
- Renderer pass runs during `Check()` after Helm rendering (R18 pipeline).
- Rendered MRs append to `World.Resources` — downstream rules (R15, R17, R23,
  R26) see them automatically.

### P3.d — New rule code

`XPC.H.composition-renders` error class matching `XPC.H.helm-renders`:
per-composition render failures emit here. No new per-resource rule in P3;
existing rules get broader signal automatically.

### P3 exit criteria

- `xpc check` on a fixture with `{xr.yaml, composition.yaml, function-binding.yaml}`
  produces downstream rendered MRs in the diag source.
- `xpc plan` across two refs where a composition change adds/removes a rendered
  MR produces correct R26 output.
- Replay v6: expected increase in `XPC.D.kind-whitelisted` (R15) as more
  rendered resources reach World.Resources; expected floor of
  `XPC.H.composition-renders` for the fg-manifold compositions that use
  functions we don't have image-pulled locally.

---

## Phase 4 — Trajectory-aware rules (out of scope here, flagged for next plan)

Named F-category invariants never implemented:

- **`no-dangling-mount`** — Pod references ConfigMap deleted mid-sync.
  Needs: multi-snapshot trajectory (R13 wake-up), rendered MR set (P3), and
  per-wave step state.
- **`no-immutable-change`** — Update to immutable field across trajectory.
  Needs: multi-snapshot diff on rendered MRs + immutable-field registry.
- **`no-rbac-regression`** — SA permissions shrink at a trajectory step.
  Needs: rendered RBAC MRs (P3 unlocks this) + trajectory replay.

These should be their own plan doc after P3 ships. They are the natural
consumer of P3's rendered-composition output and the natural validator that
the charter's "time" dimension is actually being used.

## Open questions before P1 starts

1. **Kernel config file location for R25's name-patterns.** `cmd/xpc/main.go`
   currently has no config file — flags only. Options: (a) new `xpc.yaml`
   sibling to repo root with optional override via `--config`, (b) extend the
   existing `--appset-fixture` pattern and add `--plan-config`, (c) inline
   via `--r25-patterns="-prod,prod-"`. I lean (a) — a real config file is
   overdue. OK to do at R25 time, not blocking R24/R23.
2. **Bypass annotation key location.** `xpc.io` is a fine default but we don't
   own the domain. Keep it as the string constant, no resolver, no webhook.
   Document in `docs/patterns.md`.
3. **Plan output exit-code policy.** Non-zero on destructive by default. Should
   R26 have a strict mode that also counts non-state-bearing deletions? I'd
   say no — that's the job of general diff inspection, not a safety rule.
4. **Does `xpc plan` handle AppSet-expansion state in both variants?** Yes,
   must — AppSet templates are one of the common change surfaces (a5f77a3b8
   is exactly that). The existing pipeline already does this per-tip; plan
   just runs the pipeline twice.
5. **Worktree cleanup on panic / SIGINT.** Use `defer os.RemoveAll` and
   install a signal handler that triggers cleanup. Not exotic.

## Sizing

Rough effort in sessions, same units as the fg-manifold coverage plan:

- P1.a (R24): ½ session.
- P1.b (R23): 1 session.
- P1.c (R25): ½ session.
- P2.a (two-ref runner): 1 session.
- P2.b (delta): ½ session.
- P2.c (R26): ½ session.
- P2.d (output formats): 1 session.
- P3.a + P3.b + P3.c (composition exec + cache + world): 2 sessions.
- P3.d (rule code): ½ session.

Total: ~7.5 sessions. P1 alone is ~2 sessions (the fastest-to-land chunk and
the one that directly pays off INC-6's static aspect). P2 is ~3 sessions and
delivers the first variant-aware capability. P3 is ~2.5 sessions and unlocks
dynamic rules.

## First-session deliverable (if approved)

Land P1.a (R24) end-to-end. Smallest surface, covers the literal INC-6 trigger,
shakes out the AppSet-facts plumbing that R25 reuses, no annotation bypass
plumbing (that's R23). Replay immediately after merge.
