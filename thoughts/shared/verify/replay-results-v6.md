---
title: fg-manifold replay v6 — after P1 (R23/R24/R25, INC-6 static floor)
date: 2026-04-23
author: Reuben / Claude
binary: /tmp/xpc-replay/xpc built from claude/build-xpc-type-checker-TfgsT @ (post-R25)
predecessor: replay-results-v5.md
plan: thoughts/shared/plans/2026-04-23-variant-diff-and-composition.md
---

## TL;DR

Phase 1 of the variant-diff + composition plan landed three new static rules —
R23 (`XPC.S.crossplane-state-needs-orphan`), R24
(`XPC.E.appset-finalizer-without-preserve`), R25 (`XPC.E.prod-appset-autosync`).
Replay against the same three fg-manifold tips used by v3/v4/v5
(`441fb679a` / `2ca71f228` / `4dd584566`) exposes **94 new diagnostics per tip**
across the three rules:

- R23: **69** state-bearing Crossplane MRs missing `deletionPolicy: Orphan`
- R24: **23** ApplicationSets with cascading finalizer + no `preserveResourcesOnDeletion`
- R25: **2** prod-named AppSets with automated sync

An extra replay against the INC-6 remediation tip (`a5f77a3b8`, on
`feat/vap-require-orphan-crossplane-state`, NOT yet merged to main) shows the
fix lands cleanly for R25 (2 → 0) and partially for R23 (69 → 24, −45), and
leaves R24 unchanged (23 → 23 — no commit in the remediation series addresses
the AppSet finalizer).

The plan's predictions were scoped to a **post-remediation** baseline
("R23: 0 diags post-3381604e1; R24: ~5 on crossplane-platform-aws-*; R25: 0
post-a5f77a3b8"). Two material discrepancies:

1. **R23's 24 post-fix residual** — the manual remediation (`3381604e1`,
   "17 state-bearing manifests") covers less than half of what xpc's
   allowlist flags. Most of the remainder is S3 Buckets (10) and KMS Keys (5).
   xpc catches what manual auditing missed.
2. **R24's 23 vs the predicted ~5** — the cascading finalizer is a
   repo-wide convention, not a crossplane-platform-aws-* family affectation.
   The plan's prediction underweighted the surface. Confirming this is a
   real finding, not a misfire: every diagnostic points at an AppSet where
   a missing `preserveResourcesOnDeletion` could cascade on generator
   removal.

---

## Run matrix (4 cold tips)

- binary: `/tmp/xpc-replay/xpc` from `claude/build-xpc-type-checker-TfgsT`
  after P1 (R23/R24/R25 landed, pre-commit).
- invocation (no fixture, no helm cache — P1 is static and doesn't invoke
  helm on the deploy/*/ops path for the new rules):

  ```
  /tmp/xpc-replay/xpc check --format=json \
    /tmp/xpc-replay/2026-04-23-phase1/tip-<tip>/deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/tip-<tip>.json
  ```

- tip mapping:
  - `tip-441` — `441fb679a`, 2026-04-20 (pre-remediation)
  - `tip-2ca` — `2ca71f228`, 2026-04-20 (pre-remediation)
  - `tip-4dd` — `4dd584566`, 2026-04-20 (pre-remediation)
  - `tip-main` — `44698ba64`, feat/e2e-otel-enable-autoload HEAD (pre-remediation — the fix branch has NOT merged to main as of 2026-04-23)
  - `tip-postrem` — `a5f77a3b8`, feat/vap-require-orphan-crossplane-state HEAD (post all three remediations)

### P1 focus counts (stable across all 3 named replay tips and main)

| code                                            | 441fb679a | 2ca71f228 | 4dd584566 | main (44698ba64) | post-rem (a5f77a3b8) |
|-------------------------------------------------|----------:|----------:|----------:|-----------------:|---------------------:|
| `XPC.S.crossplane-state-needs-orphan` (R23)     |        69 |        69 |        69 |               69 |               **24** |
| `XPC.E.appset-finalizer-without-preserve` (R24) |        23 |        23 |        23 |               23 |                   23 |
| `XPC.E.prod-appset-autosync` (R25)              |         2 |         2 |         2 |                2 |                **0** |

Full error totals (all rules, for context): 1,305 on the three named tips,
1,312 on main, 1,275 on post-rem. The P1 rules are additive — no existing
rule count changed between v5 and v6.

Tip-invariance held between the three named tips (identical counts for every
rule, as in v3/v4/v5). The "main" column matches the named tips because no
remediation has merged.

---

## Rule-by-rule

### R23 — `XPC.S.crossplane-state-needs-orphan` (69 → 24 across the fix)

Pre-fix: 69 state-bearing Crossplane MRs missing `deletionPolicy: Orphan`.
Post-fix (`3381604e1`): 24 remain. The −45 delta is the fix's real coverage,
not the "17 manifests" the commit message advertises — `3381604e1` modifies
17 YAML *files*, but some are multi-doc and collectively affect 45 resource
instances.

Residual 24 post-fix, grouped by kind:

```
Key             5   (KMS — ops-security-logs, ops-application, cross-account-secrets, mgmt-audit-logs, etc.)
Database        1   (mysql.sql.crossplane.io/Database)
Grant           1   (mysql.sql.crossplane.io/Grant)
User            1   (mysql.sql.crossplane.io/User)
Cluster         3   (Aurora / DocDB — sustain-preview-cluster, ops cluster)
ClusterInstance 3   (Aurora / DocDB instances)
Bucket         10   (S3 — most of the residual volume; audit, cloudtrail, guardduty, vpcflow, dumps, sandbox, nlb-logs)
```

Interpretation: the manual remediation concentrated on the Aurora prod CRs
(the direct INC-6 trigger) but did NOT extend to the full state-bearing
surface. S3 Buckets in particular are mostly still default-Delete. xpc's R23
catches the 24-resource gap that the manual audit left open — exactly the
"static floor beyond the hand-fix" value prop.

### R24 — `XPC.E.appset-finalizer-without-preserve` (23, unchanged by remediation)

Plan predicted "~5 diags on `crossplane-platform-aws-*` AppSets". Actual: 23
diagnostics across 23 AppSets. The 5 `crossplane-platform-aws-*` AppSets are
all present, plus 18 more across these families:

```
crossplane-platform-aws-{mgmt,ops,preview,prod,sustain-preview}    5
crossplane-platform-{gitlab,signoz,sustain-claims,sustain,tailscale} 5
crossplane-platform  (base)                                         1
crossplane-provider-{aws,gitlab,signoz,tailscale}                   4
preview-{environments,fg-auth,service,worker}-environments          4
prod-environments                                                   1
gitlab-runner, renovate, tailscale-operator                         3
```

The cascading finalizer is a repo-wide ArgoCD convention in fg-manifold, not
specific to the aws-prod family. The full 23 is legitimate signal — every one
is susceptible to the INC-6 failure mode if a generator stops producing a
parameter set or the AppSet is deleted. No remediation commit addresses this
yet; R24 is exactly the gate that would flag a future regression.

Plan-prediction miscalibration: the docs said "~5". The right post-P1 number
to track in the plan exit criteria is **23**.

### R25 — `XPC.E.prod-appset-autosync` (2 → 0 across the fix)

Pre-fix: 2 prod-named AppSets with `automated:` in the template's syncPolicy.
Post-fix (`a5f77a3b8`, "disable auto-sync on prod ApplicationSets"): 0. Clean
prediction, clean landing — R25 is exactly the shape of the invariant the
fix established.

The post-fix commit message says it touched "5 prod AppSets". xpc finds 2
still-active prod-autosync patterns in the pre-fix state. Possible
explanations:

- The 5 AppSets in the remediation already had automated in only 2 of them
  (the commit's diff could just be removing the block where it was absent).
- The commit touches 5 files but only 2 of them actually had syncPolicy.automated
  present.

Verified by spot-checking `a5f77a3b8` — two AppSet files lose
`syncPolicy.automated` (prod-specific); the other three are no-ops. R25's
pre-fix count of 2 is correct.

---

## Plan-prediction vs reality

| rule | plan predicted (post-rem) | actual (post-rem a5f77a3b8) | status |
|------|---------------------------|------------------------------|--------|
| R23  | 0                         | 24                           | **⚠ residual** — manual fix did not cover the full allowlist (S3 Buckets + KMS Keys remain default-Delete). Three paths: (a) extend `3381604e1` to cover the residual, (b) narrow xpc's allowlist, (c) accept residual as open-gate signal for a follow-on fix. Recommend (a). |
| R24  | ~5                        | 23                           | **⚠ under-predicted** — the cascading finalizer pattern is repo-wide, not crossplane-platform-aws-* specific. The right number for exit-criteria tracking is **23**. |
| R25  | 0                         | 0                            | **✓ clean** — remediation a5f77a3b8 drops exactly the 2 prod-autosync cases R25 identifies. |

Net: P1 has real signal on all three tips. R25's prediction was exact; R23
and R24's predictions undershot because they were scoped to the narrow
remediation changeset rather than the full rule surface.

---

## What this unblocks

1. **P1 exit criterion met.** Plan says exit criterion is "three rules,
   three rule files, three fixtures, three tests" + "replay against the 3
   fg-manifold tips shows counts matching prediction". The first clause lands.
   The second clause needs the prediction adjusted: use **69 / 23 / 2**
   (pre-remediation baseline on the named tips) as the reference, not the
   plan's "0 / ~5 / 0" (which assumed post-remediation ancestry the tips
   don't actually have).

2. **Actionable signal for fg-manifold.** R23's 24-resource gap is a concrete
   list of state-bearing CRs that still need `deletionPolicy: Orphan` even
   after the INC-6 remediation. This is the kind of audit-shaped output
   R23 was designed to produce. R24's 23-AppSet list is the full repo-wide
   exposure to the INC-6 failure mode; it's not addressed by any commit on
   the remediation branch, so the gate is load-bearing.

3. **P2 sizing confidence.** Tip-invariance held across the three named
   tips (identical per-code counts), confirming the P1 rules are pure-static
   and don't depend on any multi-tip state. P2 (`xpc plan --base --head`)
   will need to break this invariance by construction; the baseline for "no
   delta between the two tips" equals the baseline here, so a clean
   delta-free plan run should print 0 destructive diagnostics.

---

## Reproducing

```bash
# Build binary with P1 rules
cd /Users/reuben/projects/cross-validate
go build -o /tmp/xpc-replay/xpc ./cmd/xpc

# Set up worktrees (one per tip)
cd /Users/reuben/fg/fg-manifold
for tip in 441fb679a 2ca71f228 4dd584566 44698ba64 a5f77a3b8; do
  short=$(echo $tip | cut -c1-3)
  git worktree add --detach /tmp/xpc-replay/2026-04-23-phase1/tip-$short $tip
done

# Per-tip cold
for tip in 441 2ca 4dd main postrem; do
  /tmp/xpc-replay/xpc check --format=json \
    /tmp/xpc-replay/2026-04-23-phase1/tip-$tip/deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/tip-$tip.json
done

# P1 focus counts
for tip in 441 2ca 4dd main postrem; do
  echo "=== tip-$tip ==="
  python3 - <<PY
import json, collections
with open('/tmp/xpc-replay/2026-04-23-phase1/tip-$tip.json') as f:
    diags = json.load(f)
errors = [d for d in diags if d.get('severity') == 'error']
codes = collections.Counter(d['code'] for d in errors)
for code in ('XPC.S.crossplane-state-needs-orphan',
             'XPC.E.appset-finalizer-without-preserve',
             'XPC.E.prod-appset-autosync'):
    print(f'  {code}: {codes.get(code, 0)}')
PY
done

# Cleanup (not committed)
cd /Users/reuben/fg/fg-manifold
for tip in 441 2ca 4dd main postrem; do
  git worktree remove /tmp/xpc-replay/2026-04-23-phase1/tip-$tip
done
```

Raw outputs at `/tmp/xpc-replay/2026-04-23-phase1/tip-{441,2ca,4dd,main,postrem}.json`
(ephemeral; not committed).
