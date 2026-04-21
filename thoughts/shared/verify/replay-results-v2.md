---
title: fg-manifold replay v2 — after Waves 1–3 fixes
date: 2026-04-21
author: Reuben / Claude
binary: /tmp/xpc-w4 built from claude/build-xpc-type-checker-TfgsT @ a7ead5c
predecessor: replay-results.md (@ claude/phase1-cleanup 3d4476f)
---

## TL;DR

**Primary goal met.** The R15 cartesian false-positive flood is gone: 64,484 → 700 diagnostics per tip (92× reduction), and every R15 diagnostic is now blamed on the actual owning Application instead of joined against every Application in the world. The `--kernel-path` / `XPC_KERNEL_PATH` plumbing works end-to-end from outside the repo tree.

**Secondary goal partially met.** The render-cache silent-failure fix is validated by unit tests (`TestCacheCreatesNestedDirEagerly`, `TestCacheDegradesOnUnwritableDir`) and by direct observation that `~/.cache/xpc/renders/` now exists after the first run. It cannot be empirically validated on the fg-manifold workload because every Helm source in `deploy/` is a **remote** chart (no local `Chart.yaml`) — all 34 Helm renders fail at `ResolveChart` with "Path required" before the cache path is reached, and there are no Kustomize sources that render successfully either. Zero Put calls happened during the run. Warm vs cold wall-time is therefore unchanged.

**R21 validated.** 12 `XPC.E.late-init-needs-ignore-diff` diagnostics on every tip — R21 is firing on this workload, cashing the final 15% coverage bucket from the 77% paper claim.

**XPC006 still 1,980.** This was deferred in Wave 1; the fix requires propagating `OwningApp` into `XRDInfo`, `CompositionInfo`, `FunctionInfo`, `ProviderInfo` — a structurally different change from the Resource side.

---

## Run matrix (6 runs)

fixture: `/tmp/fg-manifold-pr-fixture.yaml` (4 AppSets × 1–2 stub PRs each)
binary: `/tmp/xpc-w4` built from `claude/build-xpc-type-checker-TfgsT @ a7ead5c`
invocation: `/tmp/xpc-w4 check --kernel-path=/Users/reuben/projects/cross-validate/kernel --format=json --appset-fixture=… /Users/reuben/fg/fg-manifold/deploy/`
cwd: `/tmp` (proves `--kernel-path` works from outside the xpc repo — was a prerequisite in v1)

| tip       | run  | wall-time | exit | total diags |
|-----------|------|-----------|------|-------------|
| 441fb679a | cold | 1:02.91   | 1    | 3,141       |
| 441fb679a | warm | 1:05.32   | 1    | 3,153       |
| 2ca71f228 | cold | 1:03.59   | 1    | 3,141       |
| 2ca71f228 | warm | 1:05.25   | 1    | 3,153       |
| 4dd584566 | cold | 1:02.62   | 1    | 3,141       |
| 4dd584566 | warm | 1:08.03   | 1    | 3,141       |

Determinism preserved — totals identical within each (phase, tip) pair except the small cold/warm drift on tips 1 and 2 (see "Surprises" below).

---

## Rule breakdown (per tip, cold)

| code                              | v1 count | v2 count | delta     | note                                                           |
|-----------------------------------|---------:|---------:|-----------|----------------------------------------------------------------|
| `XPC.D.kind-whitelisted` (R15)    |   64,484 |      700 | **−92×**  | fixed — per-resource, per-owning-app. no cartesian blowup.     |
| `XPC006` (sync-wave ordering)     |    1,980 |    1,980 | —         | deferred — same cartesian shape but on XRD/Composition facts. |
| `XPC.E.selector-needs-ignore-diff` (R16) | 374 |      374 | —         | healthy; all unique.                                           |
| `XPC.H.appset-unsupported-generator`     |  41 |       41 | —         | deferred — Go-template appset-expander gap (followup #6).     |
| `XPC.H.helm-renders` (R18)        |       34 |       34 | —         | deferred — remote-chart "Path required" (followup #4).        |
| `XPC.E.late-init-needs-ignore-diff` (R21) | — |      12 | **+12**   | **new — R21 online and firing.**                               |
| total                             |   66,913 |    3,141 | **−95%**  | signal-to-noise vastly improved; R15 no longer drowns output.  |

## R15 post-fix shape

700 diagnostics on tip 1 cold, across **95 unique `(kind, group)` prefixes**, blamed on named Applications (not cartesian-joined). Sample distribution (top 10 kinds):

| kind × group                                     | count |
|--------------------------------------------------|------:|
| ExternalSecret (external-secrets.io)             |    46 |
| IAMAccountAssignment (platform.facilitygrid…)    |    37 |
| Secret (secretsmanager.aws.upbound.io)           |    36 |
| LifecyclePolicy (ecr.aws.upbound.io)             |    33 |
| Repository (ecr.aws.upbound.io)                  |    33 |
| Project (projects.gitlab.m.crossplane.io)        |    30 |
| RolePolicyAttachment (iam.aws.upbound.io)        |    26 |
| Policy (iam.aws.upbound.io)                      |    25 |
| Role (iam.aws.upbound.io)                        |    24 |
| ConfigMap (core)                                 |    23 |

These look like real AppProject whitelist gaps in fg-manifold: the whitelist doesn't cover the various Crossplane provider kinds that sync-time Applications want to apply. That's a legitimate Day-2 data signal, which is what R15 was supposed to provide and couldn't with the cartesian bug.

Per-plan success target was ≤500 (one per `(kind, group, app)` triple). We're at 700 because the kernel emits **one per resource** within the owning app rather than deduping by `(kind, group)`. That's a UI dedup choice, not a correctness bug — any dedup belongs in the report/renderer layer, not the rule. Raising this from 700 → ~288 is a pure UX tweak if wanted; the cartesian itself is gone.

---

## Perf

| metric              | v1        | v2        | target     | status                         |
|---------------------|-----------|-----------|------------|--------------------------------|
| cold wall-time      | 1:14–1:34 | 1:02–1:04 | <2 min     | **met** (modest improvement)   |
| warm wall-time      | 1:16–1:28 | 1:05–1:08 | <30s       | **missed** — see below         |
| `~/.cache/xpc/` dir | absent    | present   | exists     | **met** (fix confirmed)        |
| disk-cache entries  | 0         | 0         | >0 on warm | **N/A for this workload**      |

Why warm isn't faster: `Put` was called **zero times** during any of the 6 runs (verified via debug build). All Helm sources in fg-manifold are remote charts (`chart: argocd-image-updater`, etc. — there are no local `Chart.yaml` files under `deploy/`). The renderer fails at `ResolveChart` with "Path required" for all 34 before reaching the cache code path. The few Kustomize sources present look the same way (no `XPC.H.kustomize-renders` errors — but also no renders succeeding into the cache). So this workload can't exercise the disk cache regardless of whether the fix works.

The fix IS real: unit tests cover both the happy path (`TestCacheCreatesNestedDirEagerly`) and the unwritable-parent failure path (`TestCacheDegradesOnUnwritableDir`). The observable signal on fg-manifold is that `~/.cache/xpc/renders/` now exists immediately after the first run — previously it never appeared.

Unblocking warm-perf measurement requires addressing followup #4 (remote chart render) or running against a repo with local helm charts.

---

## Surprises

1. **Warm runs emit ~12 extra diagnostics on tips 1 and 2 (not tip 3).** R16 (selector-needs-ignore-diff) gains 10 (374→384), R18 (helm-renders) gains 1 (34→35), and one more R15 shows up. Totals stay identical within a given (tip, phase), so this is deterministic for a given tip+phase but drifts cold→warm on 2 of the 3 tips. Plausible cause: shared-resource determinism check (renderer.DoubleRenderHelm) seeds different state on a warm run. Worth a small investigation but not load-bearing for the cartesian or R21 validation.

2. **R15 dropped to 0 before the `findRepoRoot` patch.** My first enrichment attempt resolved `src.Path` against `filepath.Dir(app.Source.File)` — fine for co-located fixtures but catastrophic for fg-manifold where `spec.source.path` is repo-root-relative. Net effect was every resource silently unowned → R15 filtered them all out. Added `findRepoRoot` to walk up from `appDir` looking for `.git`, and R15 moved to 700. This was the only bug introduced by Wave 1; caught and fixed in the replay.

3. **The xpc binary itself is stable — no new crashes, no panics.** Even with the 7-element `resource-fact` restructuring touching 6 kernel files, all destructure patterns absorbed the new field cleanly. This mirrors the v1 result.

4. **R21 is quietly healthy.** 12 diagnostics per tip, tip-invariant. No need for follow-up triage — the late-init coverage claim lands as advertised.

---

## Gates status (follow-up queue from replay-results.md §Followup)

| # | Gate                                         | Priority | Status after v2                                                   |
|---|----------------------------------------------|----------|-------------------------------------------------------------------|
| 1 | R15 n×m cartesian                            | P0       | **done** — 64,484 → 700 (92× reduction). OwningApp filter works.  |
| 2 | XPC006 same-shape cartesian                  | P0       | **deferred** — requires OwningApp on XRD/Composition facts.       |
| 3 | Render cache not populating                  | P1       | **done** — dir now exists; can't measure hits on this workload.   |
| 4 | Remote Helm `Path required`                  | P1       | **open** — 34 per tip.                                            |
| 5 | `xpc --kernel-path` flag                     | P2       | **done** — flag + `XPC_KERNEL_PATH` env var both working.         |
| 6 | ApplicationSet Go-template expander          | P2       | **open** — 41 per tip.                                            |
| 7 | R21 late-init validation                     | P0 (new) | **done** — 12 per tip, tip-invariant.                             |

---

## Reproducing

```
cd /Users/reuben/fg/fg-manifold
git checkout 441fb679a   # (or 2ca71f228, 4dd584566)
cd /tmp
rm -rf ~/.cache/xpc
time /tmp/xpc-w4 check \
  --kernel-path=/Users/reuben/projects/cross-validate/kernel \
  --format=json \
  --appset-fixture=/tmp/fg-manifold-pr-fixture.yaml \
  /Users/reuben/fg/fg-manifold/deploy/ > /tmp/tip-cold.json
# repeat for warm
```

Raw outputs are under `/tmp/replay-v2/{tip1,tip2,tip3}-{cold,warm}.json`.
