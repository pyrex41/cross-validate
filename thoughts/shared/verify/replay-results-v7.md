---
title: fg-manifold replay v7 — after P4 (a/b/c/d: enrichment reorder + R27 + cross-step R12/R14 + R13 retirement)
date: 2026-04-23
author: Reuben / Claude
binary: /tmp/xpc-replay/xpc built from claude/build-xpc-type-checker-TfgsT @ 5349983
predecessor: replay-results-v6.md
plan: ~/.claude/plans/fully-plan-out-p4-expressive-frog.md (parent: thoughts/shared/plans/2026-04-23-variant-diff-and-composition.md)
---

## TL;DR

P4 lands cleanly on the "small" predictions but exposes one large surprise.
**R13 retired → 0 everywhere (as predicted).** **R27 plan-mode silent on all
three pairs (as predicted).** **R23/R24/R25/R15/R16/R18/R21/XPC006 unchanged
from v6** — consistent with crossplane binary being absent, so P4.a's
enrichment reorder has no material input on this corpus. **R12 (XPC012)
exploded from 0 → 3654 / 3504 / 3854 per tip** due to the P4.c swap to the
cross-step variant. The 3654 reduce to **12 distinct (owner, target) mount
facts** — every diagnostic is re-emitted once per trajectory step the mount's
owner appears in. Finding is real (12 mounts refer to Secrets/ConfigMaps
absent from the trajectory state); amplification is a kernel-level
per-step-per-mount-ref emission pattern that should be deduped before R12-cross
is considered ready for production gating. Plan-mode ran without error once
the kernel path was provided explicitly via `--kernel-path`.

Prediction scorecard: **7 / 8 ✓, 1 partial deviation** (Prediction 1: R12
count changed but NOT from hook-delete-across-waves — from the cross-step
swap's per-step iteration pattern).

## What's different in v7 (vs v6's binary)

Four kernel+IR changes landed:

- **P4.a** (`0e493ce`) — `EnrichTrajectoryData` + `EnrichFieldValidation`
  now run AFTER `renderCompositions` in `pkg/ir/builder.go:Build()`. Net
  effect on fg-manifold: **none observed**, because the crossplane binary
  is absent on this machine, so `renderCompositions` adds 0 resources and
  falls through to `XPC.H.composition-renders` warnings (10 per tip, all
  "crossplane binary absent").
- **P4.b** (`0aeb5e5`) — new `XPC.P.immutable-change` (R27, plan-mode only)
  with 30-entry immutable registry. All three plan pairs: 0 emissions.
- **P4.c** (`5ae1e0a`) — R12 / R14 swapped to `-cross` variants in
  `kernel/check.shen`. R12 count exploded; R14 remains 0 (no matching
  SA→RoleBinding dangling refs in fg-manifold's trajectory).
- **P4.d** (`5349983`) — R13 (`XPC013`) de-registered. Count 0 everywhere.

**Pre-measurement facts** (captured before running the binary):

- `hook-delete-policy` occurrences in `tip-main/deploy/facilitygrid/ops`:
  **0**. fg-manifold does not use the ArgoCD hook-delete policy anywhere in
  this tree. That means cross-wave target-deleted-earlier scenarios do
  **not** arise from real data here — any XPC012 cross-step emissions must
  come from targets that were never applied at all, not from
  hook-deletion crossing wave boundaries. (See "cross-step enumeration"
  section below.)
- Composition definitions on tip-main: **16** under
  `applications/crossplane-platform/`. Raw state-bearing MRs on tip-main:
  122 apiVersion-level instances (docdb 8, kms 18, rds 23, s3 73).
- `which crossplane` → **not on PATH**. So live composition rendering
  never ran; R18-composition-renders degrades to 10 warnings per tip
  (1 per Composition×XR pair attempted).

## Run matrix

Tips (single-tip `xpc check`):

| code                                         | 441 (v6) | 441 (v7) |  2ca v7 |  4dd v7 | main v7 | postrem v7 |
|----------------------------------------------|---------:|---------:|--------:|--------:|--------:|-----------:|
| total errors                                 |     1305 |     4959 |    4959 |    4959 |    4816 |       5129 |
| `XPC012` (R12, no-dangling-mount)            |    **0** | **3654** |    3654 |    3654 |    3504 |   **3854** |
| `XPC013` (R13, retired)                      |        0 |        0 |       0 |       0 |       0 |          0 |
| `XPC014` (R14, no-rbac-regression)           |        0 |        0 |       0 |       0 |       0 |          0 |
| `XPC.D.kind-whitelisted` (R15)               |      700 |      700 |     700 |     700 |     701 |        711 |
| `XPC.E.selector-needs-ignore-diff` (R16)     |      435 |      435 |     435 |     435 |     440 |        440 |
| `XPC.E.late-init-needs-ignore-diff` (R21)    |       12 |       12 |      12 |      12 |      12 |         12 |
| `XPC.S.crossplane-state-needs-orphan` (R23)  |       69 |       69 |      69 |      69 |      69 |     **24** |
| `XPC.E.appset-finalizer-without-preserve` R24|       23 |       23 |      23 |      23 |      23 |         23 |
| `XPC.E.prod-appset-autosync` (R25)           |        2 |        2 |       2 |       2 |       2 |      **0** |
| `XPC.H.helm-renders` (R18, err)              |       34 |       34 |      34 |      34 |      35 |         35 |
| `XPC.H.composition-renders` (warn, P4.a)     |        0 |       10 |      10 |      10 |      10 |         10 |
| `XPC006` (R6 wave-ordering)                  |       30 |       30 |      30 |      30 |      30 |         30 |

Plan-mode (`xpc plan --kernel-path=...`):

| pair               | delta (add/mod/rem) | XPC.P.destructive-delete (R26) | XPC.P.cascade-risk (R26) | XPC.P.immutable-change (R27) |
|--------------------|---------------------|-------------------------------:|-------------------------:|------------------------------:|
| 441 → 2ca          | 0 / 0 / 0           |                              0 |                        0 |                            0 |
| 2ca → 4dd          | 0 / 0 / 0           |                              0 |                        0 |                            0 |
| main (44698ba64) → postrem (a5f77a3b8) | 14 / 52 / 0 |                              0 |                        0 |                            0 |

Deltas of 0/0/0 for the two adjacent-tip pairs are real: `git diff --stat
441..2ca -- deploy/facilitygrid/ops` and `2ca..4dd -- deploy/facilitygrid/ops`
both show no changed files. Those commits touched non-ops files.

## Rule-by-rule

### R12 (XPC012, no-dangling-mount) — **0 → 3654 explosion from P4.c**

This is the headline finding. `check-r12-cross` in
`kernel/r12-no-dangling-mount.shen` iterates over every trajectory step and
for each step walks every mount-ref, emitting one diagnostic per
(step, mount-ref) where owner is in state and target is absent. On
fg-manifold the trajectory is large (every ArgoApplication contributes at
least one step per sync wave, and AppSet expansion multiplies
application counts). A mount whose target isn't in any state (because the
Secret / ConfigMap is produced by SealedSecrets / External Secrets /
an out-of-tree path xpc doesn't see) fires on every step the owner
appears in.

Deduplicated, the 3654 emissions collapse to **12 distinct (target-kind,
target-name, owner-kind, owner-name) tuples**:

```
ConfigMap twilio-shim-ca → Deployment oneuptime-app    (× 1514)
ConfigMap twilio-shim-ca → Deployment oneuptime-worker (× 1514)
ConfigMap myanon-config → CronJob migration-dump-anonymizer (× 80)
ConfigMap pool-seed → CronJob e2e-pool-replenish       (× 40)
Secret fg-claude-bot-secrets → Deployment fg-claude-bot (× 92)
Secret khoj-secrets → Deployment khoj                  (× 92)
Secret gitlab-secrets → StatefulSet gitlab             (× 78)
Secret gitlab-ssh-host-keys → StatefulSet gitlab       (× 78)
Secret usertour-secrets → Deployment usertour          (× 46)
Secret migration-secrets → CronJob migration-dump-anonymizer (× 40)
Secret s3-credentials → CronJob migration-dump-anonymizer (× 40)
Secret staging-db-credentials → CronJob migration-dump-anonymizer (× 40)
```

Each row is a real mount of a Secret/ConfigMap that xpc never observes as
an applied resource on this tip. Most are externally managed (SealedSecrets,
External Secrets Operator, or secrets created manually for the
oneuptime/gitlab/khoj stacks). Before we can treat R12-cross as a gate, the
kernel needs to:

1. Scope mount-refs to the trajectory of the owning Application (don't
   iterate mount-refs globally against every step).
2. Deduplicate emissions to one per distinct (owner, target) pair across
   the whole trajectory.

Without either, R12-cross is a 300× amplifier of a 12-entry findings list.
The underlying 12 are actionable (either add the missing
secret/configmap to the manifests, mark the mount optional, or carve the
resource out via a known-external-secret annotation).

### R13 (XPC013, retired) — **0 everywhere (as predicted)**

Confirmed. `r13-no-immutable-change.shen` no longer loaded in
`kernel/check.shen`. No XPC013 code appears in any of the 5 tip JSONs.

### R14 (XPC014, no-rbac-regression) — **0 everywhere (unchanged from v6)**

The cross-step swap mirrors R12's change but finds nothing in fg-manifold:
there are no SA→RoleBinding dangling pairs in the trajectory state on any
tip. This is consistent — fg-manifold uses AppProjects + Crossplane-provider
SAs rather than per-application SA + RoleBinding chains, so the
failure pattern R14 targets is dormant on this corpus.

### R27 (XPC.P.immutable-change, NEW) — **0 on all 3 pairs (as predicted)**

No immutable-field changes detected on:
- 441→2ca (empty IR delta)
- 2ca→4dd (empty IR delta)
- main→postrem (14 added + 52 modified resources, but none touched a
  registered immutable field — the remediation is `spec.deletionPolicy`
  only, which is correctly **not** in the immutable registry)

This matches predictions 6, 7, and 8 exactly.

### R26 (XPC.P.destructive-delete + XPC.P.cascade-risk) — 0 on all 3 pairs

Plausible: 441→2ca and 2ca→4dd have zero delta, so nothing to delete.
main→postrem's 52 modified + 14 added resources include zero removals
(`removed: 0` in the delta). R26 only fires on removals, so 0 is expected.

### R1–R11, R15–R25 (pre-P4 rules) — unchanged

Every pre-existing rule's count matches v6 exactly on every tip:

- R15 XPC.D.kind-whitelisted: 700 / 700 / 700 / 701 / 711 — identical
- R16 XPC.E.selector-needs-ignore-diff: 435 / 435 / 435 / 440 / 440 — identical
- R21 XPC.E.late-init-needs-ignore-diff: 12 everywhere — identical
- R23 XPC.S.crossplane-state-needs-orphan: 69 / 69 / 69 / 69 / 24 — identical
- R24 XPC.E.appset-finalizer-without-preserve: 23 everywhere — identical
- R25 XPC.E.prod-appset-autosync: 2 / 2 / 2 / 2 / 0 — identical
- XPC006 (R6 wave-ordering): 30 everywhere — identical
- R18 XPC.H.helm-renders: 34 / 34 / 34 / 35 / 35 — identical

Prediction 3 ("R15–R22, R21, R16 may shift upward because rendered
Crossplane MRs now enter their extractors") is a **no-op on this
corpus**. Rationale: `which crossplane` is empty, so
`renderCompositions` degrades to 10 `XPC.H.composition-renders`
warnings per tip and adds zero resources. The reorder of
`EnrichTrajectoryData` / `EnrichFieldValidation` after render is
therefore invisible on real data. A machine with crossplane installed
would exercise P4.a; this box doesn't.

### R18 (XPC.H.composition-renders) — 10 warnings/tip (new warning channel)

Every tip gets 10 crossplane-absent warnings, one per Composition×XR
pair attempted. Severity is correctly `warning` (not error) because the
crossplane binary is absent rather than the render failing. This is
orthogonal to the count counts above — warnings don't flip exit status.

## Prediction scorecard

| # | prediction                                                                                           | actual                                          | verdict     |
|---|------------------------------------------------------------------------------------------------------|-------------------------------------------------|-------------|
| 1 | R12/R14 per-tip unchanged UNLESS hook-delete across waves                                           | R14 unchanged (0). R12 went 0→3654 per tip     | **⚠ partial deviation** — not from hook-delete (fg-manifold has 0 hook-deletes); from cross-step's per-step iteration on mount-refs with targets absent from state. 12 real findings amplified ~300× by the emission pattern. |
| 2 | R23 per-tip may rise above v6's 69 if Compositions render state-bearing MRs                          | R23 exactly 69 (pre-rem) / 24 (post-rem)       | **✓ matches** — no rendering happened (crossplane absent), so no composition-sourced MRs added. |
| 3 | R15–R22, R21, R16 counts may shift upward from composition-rendered MRs entering extractors          | All identical to v6                             | **✓ matches** (no-op cause: crossplane binary absent) |
| 4 | R13 = 0 everywhere                                                                                    | 0 on all 5 tips                                 | **✓ matches** |
| 5 | R27 = 0 on single-tip `check`                                                                        | No XPC.P.* in check output                      | **✓ matches** |
| 6 | R27 = 0 on `plan 441→2ca`                                                                            | 0 (empty delta anyway)                          | **✓ matches** |
| 7 | R27 = 0 on `plan 2ca→4dd`                                                                            | 0 (empty delta anyway)                          | **✓ matches** |
| 8 | R27 = 0 on `plan main→postrem` (deletionPolicy not in registry)                                      | 0 on a 66-resource delta                        | **✓ matches** |

Net: **7 / 8 clean, 1 partial deviation (prediction 1)**.

## Cross-step enumeration (fg-manifold hook-delete surface)

The plan's prediction 1 leaned on the hypothesis that if fg-manifold had
hook-deletes across sync waves, R12-cross and R14-cross would light up
with cross-wave dangling references. The pre-measurement shows
**zero** `argocd.argoproj.io/hook-delete-policy` annotations in
`deploy/facilitygrid/ops` on tip-main. fg-manifold does not express
cross-wave deletion via ArgoCD hooks. If and when it does, R12-cross is
positioned to catch it — but on today's data the cross-step semantics are
exercising a different path (targets that are entirely absent from all
steps' State, not targets hook-deleted at an earlier step).

So: the cross-step addition is **correct behavior** but **over-emissive**
on absent-target cases. The kernel helper `tail-steps` added by P4.c is
currently unused by the active code path (check-r12-cross scans the
current step's State only; it doesn't actually walk tail steps). That
means P4.c's per-step scan is really the simpler "current-step absent"
semantic, not a cross-wave semantic — the name is aspirational. The
3654-number is not evidence of a cross-wave failure mode; it's evidence
of the per-step-emission pattern applied to 12 real targets-never-applied
findings.

## What this unblocks / suggests

1. **Dedup R12-cross before gating.** Before treating R12-cross as a PR
   gate in fg-manifold, the kernel needs to emit at most one diagnostic
   per (owner, target) tuple, not one per trajectory-step × mount-ref.
   The obvious fix is a pre-pass that collapses the mount-ref facts to
   unique (owner, target) keys and fires R12-cross against the trajectory
   union-of-states once. Can be done in the Shen kernel (stable-sort +
   uniq on emissions) or the Go bridge (dedup by (code, message) before
   returning). The 12-finding floor is the real actionable set.

2. **Tail-steps helper is dead code on real data.** The P4.c commit
   added `tail-steps` in the prelude; `check-r12-cross` and
   `check-r14-cross` don't invoke it. Either wire it in for a true
   cross-wave semantic (target was in an earlier step's State and
   disappeared by the later step, with the Pod persisting) or drop it
   to avoid confusion about what "cross-step" means.

3. **P4.a enrichment reorder is an untested code path on this box.**
   The reorder's intent ("rendered Crossplane MRs feed every downstream
   extractor") is invisible here because crossplane-absent. Next replay
   should run on a machine with crossplane installed (or a per-tip
   `--crossplane-bin` shim) to exercise P4.a's real effect; predictions
   like "R23 may rise above 69 from rendered MRs" are testable only with
   a working renderer.

4. **R27 registry looks correctly conservative.** Zero emissions on a
   52-resource-modified plan delta is the right behavior when the
   remediation touches `spec.deletionPolicy` (explicitly not in the
   registry by design). The registry currently covers 30 entries; once
   composition rendering goes live and a future delta touches RDS/DocDB
   identifiers or S3 bucket names, R27 will have something to say.

5. **Plan harness needs kernel-path discovery fix.** The first pass of
   `xpc plan` from inside `~/fg/fg-manifold` produced XPC000 "could not
   locate kernel directory (searched upwards from /)". The plan runs
   check inside `/var/folders/.../xpc-plan-*/{base,head}` — the temp
   checkout has no `kernel/` in its ancestry. Explicit
   `--kernel-path=/Users/reuben/projects/cross-validate/kernel` worked.
   Future ergonomics: carry the kernel path from the invocation cwd (not
   the checkout's cwd) or auto-discover via the running binary's
   installation prefix. Out of scope for this replay but flagged.

## Reproducing

```bash
# Build
cd /Users/reuben/projects/cross-validate
go build -o /tmp/xpc-replay/xpc ./cmd/xpc

# Single-tip check
for tip in 441 2ca 4dd main postrem; do
  /tmp/xpc-replay/xpc check --format=json \
    /tmp/xpc-replay/2026-04-23-phase1/tip-$tip/deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/tip-$tip-v7.json \
  2> /tmp/xpc-replay/2026-04-23-phase1/tip-$tip-v7.stderr
done

# Plan — requires explicit --kernel-path because the plan runner
# checks out bases into /var/folders/... which has no kernel ancestor.
cd ~/fg/fg-manifold
K=/Users/reuben/projects/cross-validate/kernel
/tmp/xpc-replay/xpc plan --base=441fb679a --head=2ca71f228 \
  --kernel-path=$K --format=json ./deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/plan-441-2ca-v7.json
/tmp/xpc-replay/xpc plan --base=2ca71f228 --head=4dd584566 \
  --kernel-path=$K --format=json ./deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/plan-2ca-4dd-v7.json
/tmp/xpc-replay/xpc plan --base=44698ba64 --head=a5f77a3b8 \
  --kernel-path=$K --format=json ./deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/plan-main-postrem-v7.json

# Per-tip counts
python3 - <<'PY'
import json, collections
for tip in ['441','2ca','4dd','main','postrem']:
    with open(f'/tmp/xpc-replay/2026-04-23-phase1/tip-{tip}-v7.json') as f:
        diags = json.load(f)
    errs = [d for d in diags if d.get('severity') == 'error']
    codes = collections.Counter(d['code'] for d in errs)
    print(f'=== tip-{tip} ({len(errs)} errors) ===')
    for code in sorted(codes):
        print(f'  {code}: {codes[code]}')
PY

# Plan counts (per-side + destructive/immutable aggregates)
python3 - <<'PY'
import json, collections
for pair in ['441-2ca','2ca-4dd','main-postrem']:
    with open(f'/tmp/xpc-replay/2026-04-23-phase1/plan-{pair}-v7.json') as f:
        p = json.load(f)
    print(f'=== plan {pair} delta={p["delta"]} ===')
    for side in ('base','head'):
        lst = p.get('diagnostics',{}).get(side,[])
        codes = collections.Counter((d['code'],d['severity']) for d in lst)
        print(f'  [{side}] {len(lst)} diags')
        for (c,s),n in sorted(codes.items()):
            print(f'    {s} {c}: {n}')
    print(f'  destructive: {len(p.get("destructive", []))}')
PY
```

Raw outputs (ephemeral, not committed):
- `/tmp/xpc-replay/2026-04-23-phase1/tip-{441,2ca,4dd,main,postrem}-v7.json`
- `/tmp/xpc-replay/2026-04-23-phase1/plan-{441-2ca,2ca-4dd,main-postrem}-v7.json`
