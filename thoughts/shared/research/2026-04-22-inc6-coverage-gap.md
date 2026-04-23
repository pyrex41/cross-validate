---
title: Re-scoping xpc around variant-diff + composition execution (INC-6 is the forcing function, not the whole story)
date: 2026-04-22 (rewritten 2026-04-23)
author: Reuben / Claude
status: superseding previous draft; plan doc at thoughts/shared/plans/2026-04-23-variant-diff-and-composition.md
category: architecture
inputs:
  - fg-synapse c0765e5 — INC-6 postmortem (ArgoCD delete cascade, SEV-2)
  - fg-manifold 3381604e1 / 7589728b5 / a5f77a3b8 — today's three structural fixes
  - https://github.com/millstonehq/crossplane-plan — worked example of the capability class
  - thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md
  - thoughts/shared/research/2026-04-21-vision-status-after-r21.md
---

## TL;DR

INC-6 is the forcing function for a charter-level re-scope, not a "write three more
static rules" task. The original vision docs (ADR-001, ADR-002, the target study)
describe xpc as a CLI gate that simulates what will happen across variants and time.
Current xpc is a fast static checker — 22 rules across 10 of 12 taxonomy categories,
all operating on a single tip. The named dynamic invariants (F-category:
`no-dangling-mount`, `no-immutable-change`, `no-rbac-regression`) have never been
implemented. R15–R21 added coverage and hardened the static layer, but did not
advance the dynamic substrate. That's the drift.

`millstonehq/crossplane-plan` is a worked example of the dynamic/variant-aware
capability class, realized on a live-cluster substrate. It diffs a PR against
baseline, executes Crossplane composition functions to materialize managed
resources, surfaces deletions as first-class output, and posts a plan back to
the PR. This is not an adjacent tool; it is a sibling implementation of xpc's
original charter.

Plan: add this capability layer to xpc on xpc's own substrate (files + process-local
execution, no live cluster). INC-6 falls out naturally. See the plan doc at
`thoughts/shared/plans/2026-04-23-variant-diff-and-composition.md` for phasing.

## Inputs

### 1. fg-synapse INC-6 postmortem

Root-cause chain:

1. AppSet template bakes `resources-finalizer.argocd.argoproj.io` into every generated
   Application, without `spec.syncPolicy.preserveResourcesOnDeletion: true`.
2. Any actor that sets `deletionTimestamp` on the Application triggers cascade DELETE.
3. State-bearing Crossplane CRs default to `deletionPolicy: Delete`, so the cascade
   runs real `DROP DATABASE` / AWS `DeleteCluster` calls.
4. Prod AppSets have auto-sync on, so a destructive git change lands without a human click.

Data survived by pure ordering luck (SG rule deleted before provider-sql could connect).

### 2. Today's fixes in fg-manifold

| Commit        | What it does                                                                              | Defense layer |
|---------------|-------------------------------------------------------------------------------------------|---------------|
| `3381604e1`   | `deletionPolicy: Orphan` on 17 state-bearing manifests (Aurora, DocDB, KMS, S3, VPC, mysql.sql.*) | Property (source-of-truth for R23's kind list) |
| `7589728b5`   | ValidatingAdmissionPolicy rejecting non-Orphan state kinds in prod                        | Runtime enforcement (out of xpc's scope — but authoritative kind list) |
| `a5f77a3b8`   | Drop `spec.template.spec.syncPolicy.automated` from 5 prod AppSets                        | Gate (informs R25)  |

### 3. millstonehq/crossplane-plan — capabilities worth transplanting

Re-read on mission axis, not rule axis. What it does:

1. **Variant diff is the unit of work.** Takes a PR, compares against baseline,
   outputs what-would-change. Not "lint this file"; "preview this change."
2. **Executes Crossplane composition functions to materialize XRs → managed
   resources.** Uses kubedock + the Crossplane function runtime. This is the
   piece that closes the static-approximation gap — composition functions are
   Turing-complete (Go/Python code speaking gRPC), so no amount of static
   analysis recovers the rendered MR set with fidelity.
3. **Deletion as first-class output.** "These resources would be removed"
   is a named report section, not an absence inferred from a full dump.
4. **`[Observe]` mode enforcement** — the preview itself is invariantly safe.
5. **Markdown plan output for PR comments.** Gate-shaped, human-readable.
6. **Debounce / batching** — runtime artifact; not transplantable.

What is NOT transplantable: the live-cluster substrate (in-cluster watch,
ArgoCD instance label scoping, kubedock sidecar). xpc's substrate is git.

## Charter alignment check

From `thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md`:

> "The pivots between Go-rules / obligation-framework / Shen-as-spec moved the
> rules from one substrate to another but did not add a single new dynamic
> invariant to the implemented set."

From the same doc's F-category audit:

> "The named rules in F that would meet the user's framing — `no-dangling-mount`,
> `no-immutable-change`, `no-rbac-regression` — are exactly the kind of
> invariants the user is asking about. They are named in `docs/obligations.md`
> and ADR-001 but have no generator and no Shen rule."

From `thoughts/shared/research/2026-04-21-vision-status-after-r21.md` open questions:

> "Categories D, E, F, G, I completeness. Is the intended next direction to fill in
> the remaining generators under existing categories (…multi-snapshot R13 for F),
> or is 22 rules across 10 categories considered 'enough' for now?"

The answer on 2026-04-23: **not enough.** The next direction is the dynamic /
variant-aware layer. Every static rule we add without this layer is shovel
sharpening.

## The capability layer

Three pieces, tightly coupled. None of them individually delivers the charter;
together they do.

### Piece 1 — `xpc plan --base=<ref> --head=<ref>` subcommand

- Unit of work: a pair of git refs (or worktrees, or directories).
- Runs the existing `check` pipeline against each.
- Computes a **resource-identity delta** on the rendered World:
  added / removed / modified, keyed on `(apiVersion, kind, namespace, name, app)`.
- Emits delta-aware diagnostics (see "new rule class" below).
- Outputs **JSON** (pipelines) + **Markdown** (PR comments) + the existing human/agent/SARIF formats where they still make sense.

This is the smallest viable vehicle for variant-aware reasoning. Without it
xpc is a one-tip tool.

### Piece 2 — composition function execution

- Wrap Crossplane's existing `crossplane render` CLI (it already runs
  composition functions against an XR + Composition + Function list, locally,
  and emits rendered managed resources). Same "absent-binary sentinel" pattern
  used for Helm. Optional: sidecar protocol for direct gRPC execution later.
- Surface rendered managed resources into `World.Resources` with
  `Provenance = "rendered:composition:<xr>"`, matching the existing
  `"rendered:helm:<app>"` convention from R18.
- This is the piece that unlocks category F / G / I dynamic rules. Without it,
  xpc sees claims, not rendered MRs, and the R13 invariants remain un-provable.

### Piece 3 — plan-output format

- Markdown with sections: `Destructive changes` (errors), `Added`, `Modified`,
  `Removed (non-destructive)`, `Diagnostics`.
- JSON shape mirrors the existing diag JSON but adds `delta` and `variants`
  envelopes.
- CI gate: non-zero exit if the `Destructive changes` section is non-empty
  (tuneable per-rule severity).

## INC-6 as a special case

With the capability layer in place, INC-6 coverage decomposes into a
floor-plus-ceiling:

**Static floor (unchanged from prior draft):**

- **R23 — state-bearing Crossplane kinds require `spec.deletionPolicy: Orphan`.**
  Code `XPC.S.crossplane-state-needs-orphan`. Kind allowlist copied from
  `crossplane-state-require-orphan.yaml`. Scope: all envs. Bypass
  `xpc.io/allow-delete: "true"` primary + `policy.facilitygrid.io/allow-delete`
  alias. Carve-out: resource name contains `alb-logs`.
- **R24 — AppSet cascading finalizer without `preserveResourcesOnDeletion`.**
  Code `XPC.E.appset-finalizer-without-preserve`.
- **R25 — configured AppSets should not enable auto-sync.** Code
  `XPC.E.prod-appset-autosync`. Name-match list in kernel config.

**Dynamic ceiling (new, the reason for this rewrite):**

- **R26 — destructive-delete detection across variants.** Code
  `XPC.P.destructive-delete`. Runs only under `xpc plan`. For every resource
  present on `--base` and absent on `--head`, if the kind is in R23's
  state-bearing allowlist and the base manifest's `deletionPolicy` is not
  `Orphan`, emit error. If the disappearing object is an Argo Application
  with `resources-finalizer.argocd.argoproj.io` but not
  `preserveResourcesOnDeletion: true`, emit error. This is the check that
  would have caught INC-6's impending destructive act in CI.

The static floor catches the *config weakness* on a single tip. The dynamic
ceiling catches the *destructive change* across variants. Both fire,
intentionally — they describe different properties.

## What this is not

- Not a call to vendor crossplane-plan or depend on it as a library.
- Not a call to require a live cluster. xpc stays file-based + process-local.
  `crossplane render` is a local binary; it does not need a cluster.
- Not a call to kill the static rule layer. R23/R24/R25 land first — they're
  immediately useful and pay for themselves before the capability layer does.

## Out-of-scope for this re-scope

- Trajectory invariants (`no-dangling-mount`, `no-immutable-change`,
  `no-rbac-regression`) — these are the *next* use of the capability layer,
  not part of landing the layer itself. Separate plan doc after R26 ships.
- Replacing Helm rendering with anything. R18 stays.
- Live-cluster features (kubedock sidecar, cluster state ingestion, PR comment
  posting). Out-of-process extensions; not core xpc.

## Decisions settled earlier (preserved)

1. R23 scope: all envs. No prod-only scoping.
2. R25: in. Name-match list in kernel config.
3. Bypass annotation: `xpc.io/allow-delete: "true"` primary;
   `policy.facilitygrid.io/allow-delete: "true"` aliased.

## Where implementation lives

See `thoughts/shared/plans/2026-04-23-variant-diff-and-composition.md` for the
phased plan. This doc is the justification; that doc is the path.
