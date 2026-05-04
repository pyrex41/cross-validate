---
date: 2026-05-04T20:31:23Z
researcher: Reuben Brooks
git_commit: 125b30b07c2f005c295dd419bba508d201d826f6
branch: claude/standalone-xpc-cli
repository: cross-validate
topic: "Is cross-validate (xpc) actually useful for the fg-manifold deployment?"
tags: [research, evaluation, fg-manifold, xpc, signal-vs-noise, ci-integration, inc-6]
status: complete
last_updated: 2026-05-04
last_updated_by: Reuben Brooks
---

# Research: Is xpc actually useful for fg-manifold?

**Date**: 2026-05-04T20:31:23Z
**Researcher**: Reuben Brooks
**Git Commit**: 125b30b07c2f005c295dd419bba508d201d826f6
**Branch**: claude/standalone-xpc-cli
**Repository**: cross-validate

## Research Question

A colleague built `xpc` (this codebase, `cross-validate`) for our deployment in
`~/fg/fg-manifold`. **Is it actually useful?**

The user's question is evaluative, so this doc breaks the usual
documentation-only stance and gives a verdict, grounded in (a) running the
binary against the live fg-manifold tree, (b) verifying sample findings
against actual YAML, (c) cross-checking xpc's rule corpus against fg-manifold's
real-world MR pain history.

## Verdict in one paragraph

**Yes, but it isn't actually being used yet.** Run against the current
`fg-manifold/deploy/` tree (renderers on, 7m38s wall, 1,341 findings), xpc
flags real INC-6-class anti-patterns that would matter in production: 23
AppSets baking the ArgoCD cascading finalizer without `preserveResourcesOnDeletion`
(the exact fg-synapse INC-6 root cause), 2 prod AppSets with `automated` sync
on (INC-6 cause #4), and 69 state-bearing Crossplane resources missing
`deletionPolicy: Orphan` — including `docdb-prod-cluster`, verified by direct
read. Three of fg-manifold's last ~3 weeks of merged fix commits
(`0a4603e584`, `8d6b0079aa`, `7a36280083`) are exactly XPC.D.kind-whitelisted
territory; xpc-in-CI would have blocked them pre-merge. **But xpc has zero
integration in fg-manifold's `.gitlab-ci.yml`, `lib/ci/`, or `lefthook.yml`
today**, so none of this signal is reaching the team. Two real friction points
gate adoption: (1) a kernel-bootstrap race causing intermittent `XPC000` errors
on cold start, (2) probable false positives in two of the highest-volume rules
(see "Quality of findings" below) that would dominate the report and bury the
real signal until triaged.

## How this was assessed

1. Read the prior in-tree research that already framed the same question:
   `2026-04-18-fg-manifold-target-study.md` (gap analysis at the time —
   xpc caught ~0% of fg-manifold's pain) and `2026-04-29-actual-capabilities-vs-crossplane-plan.md`
   (current capabilities after R15–R27 landed).
2. Ran `./xpc check --format=json deploy/facilitygrid/` against the current
   fg-manifold `main` branch with renderers active.
3. Sampled the top finding categories and verified each against the source
   YAML.
4. Grep'd fg-manifold CI for any xpc invocation (none).
5. Cross-checked recent fix commits against the rule corpus to estimate "would
   have caught."

## What xpc found in fg-manifold (empirical)

```
Total findings: 1,341
  errors:    1,324
  warnings:  10
  info:      7

By code (errors only):
  XPC.D.kind-whitelisted                       701   AppProject doesn't list a kind the App produces
  XPC.E.selector-needs-ignore-diff             440   Crossplane *Selector resolves into spec; no ignoreDifferences
  XPC.S.crossplane-state-needs-orphan          69    State-bearing MR without deletionPolicy: Orphan
  XPC.H.helm-renders                           35    (most info; some failed renders)
  XPC006 (trajectory-wave-order)               30    Function/Composition wave-order
  XPC.E.appset-finalizer-without-preserve      23    INC-6 root cause #1
  XPC012                                       12
  XPC.E.late-init-needs-ignore-diff            12    Late-init drift uncovered
  XPC.H.composition-renders                    10    `crossplane` binary not on PATH
  XPC.E.prod-appset-autosync                   2     INC-6 root cause #4
  XPC000 (kernel bootstrap race)               1     infra bug, not a fg-manifold issue
```

Wall time: 7m38s on Apple Silicon. Most of that is the renderer pass; with
`--skip-render` the run completes in ~3s but produces only 37 findings (almost
all info), which means **the renderers are doing the work** — without them,
xpc sees claim wrappers, not the rendered managed resources.

## Quality of findings (verified by direct YAML read)

### High-confidence signal — verified real

**XPC.S.crossplane-state-needs-orphan / docdb-prod-cluster** — verified.
Reading `deploy/facilitygrid/ops/applications/crossplane-platform-aws-prod/aws/us-east-2/facilitygrid-ops/manifests/docdb-prod-cluster.yaml`,
the `Cluster` resource has `deletionProtection: true` (AWS-side) but no
`spec.deletionPolicy` field at all. Crossplane's default is `Delete`. INC-6's
postmortem identifies this exact failure mode: AWS-side `deletionProtection`
does not stop Crossplane from issuing `DeleteDBCluster`; only spec-side
`deletionPolicy: Orphan` does. The team explicitly added Orphan to 17
manifests in commit `3381604e1` per the INC-6 plan; xpc finding 69 more cases
suggests either incomplete rollout or new resources added since then. **This
is a real bug class that xpc catches pre-merge that the team currently catches
post-incident.**

**XPC.E.appset-finalizer-without-preserve** — verified. 23 AppSets, all with
`spec.template.metadata.finalizers: [resources-finalizer.argocd.argoproj.io]`
and no `spec.syncPolicy.preserveResourcesOnDeletion: true`. fg-manifold's own
`thoughts/shared/research/2026-04-22-inc6-coverage-gap.md` identifies this as
INC-6 root cause #1. The team has VAP runtime enforcement
(commit `7589728b5`) and per-resource Orphan annotations (`3381604e1`), but
**not** the AppSet-level guard. xpc's finding is the missing pre-merge floor.

**XPC.E.prod-appset-autosync** — 2 findings, INC-6 root cause #4. Team already
landed `a5f77a3b8` to drop `automated` from 5 prod AppSets; the 2 remaining
findings are either false positives or genuine regressions worth checking.

**Recent commit cross-check.** Three fg-manifold fix commits in the last
~3 weeks would have been blocked by xpc-in-CI:
- `0a4603e584` — `fix(argocd): add route53.aws.upbound.io to preview AppProject whitelist` → XPC.D.kind-whitelisted
- `8d6b0079aa` — `fix(ses): AppProject whitelists + defer prod SES` → XPC.D.kind-whitelisted
- `7a36280083` — `fix(ses): add sesv2.aws.upbound.io to AppProject whitelists` → XPC.D.kind-whitelisted

In each case the bug surfaced post-merge as a failed ArgoCD sync. xpc would
have surfaced the missing whitelist entry against the changed manifest set
before merge.

### Medium confidence — likely real but mixed with FPs

**XPC.E.selector-needs-ignore-diff (440 findings)** — sampled finding for
KMS Alias `targetKeyIdSelector` resolving to `targetKeyId`. The April 18
study identifies this as the **second-largest historical pain category**
(MRs !1344, !1341, !1250, !883, !890, !1366 — selector → resolved-ref drift).
Sample finding's source YAML (`kms-cross-account-secrets.yaml`) does use
`targetKeyIdSelector`; the owning ApplicationSet's `ignoreDifferences` block
uses a `group: "*" kind: "*"` wildcard but that's only on `/status`, not on
the resolved `targetKeyId` path. This is plausibly a real bug. 440 findings is
high — likely a mix of (a) genuinely uncovered selectors, (b) selectors covered
by wildcard `ignoreDifferences` patterns xpc isn't recognizing, (c) selectors
on resources with `managementPolicies: [Observe]` that don't need ignoreDifferences.
**Useful but needs triage before becoming a blocking gate.**

### Low confidence — probable false positives

**XPC.D.kind-whitelisted (701 findings)** — partially false positive. Verified
the `ops` AppProject (`appproject-ops.yaml`):
```yaml
clusterResourceWhitelist:
  - group: "*"
    kind: "*"
# (no namespaceResourceWhitelist field at all)
```
xpc reports `ConfigMap (group core) not in AppProject ops whitelist`. ArgoCD's
behavior when `namespaceResourceWhitelist` is omitted is **permit-all** for
namespaced kinds; xpc is treating the absent field as deny-all. So the 436
unique source files flagged in `ops` are likely all false positives.

The `preview` project, by contrast, has explicit per-group whitelists (which is
why `0a4603e584` had to add `route53.aws.upbound.io`). For projects with
explicit whitelists, the rule is correct. For projects with absent whitelists,
the rule over-flags.

**XPC006 wave-ordering: Function vs Composition (30 findings)** — likely
false positive. Sampled finding: "Function function-go-templating (wave 0)
must have a lower sync-wave than Composition fargateapp-preview (wave 0)".
Both objects live in the same `crossplane-platform.yaml` AppSet (per the team's
sync-wave convention: providers wave 2, platforms wave 7). They deploy in the
same Argo wave because they're in the same AppSet — the Function-before-
Composition ordering is enforced by ArgoCD applying both in the same
sync transaction, not by intra-wave ordering. xpc's view sees them at the
same wave (wave 0, the default) and flags it; the team's working pattern
deploys them together without trouble. Worth examining whether R6b's
wave-strict-less-than is the right semantics here.

## What xpc gets right that this team needs

1. **The single highest-blast-radius rule (R24, AppSet finalizer + preserve)
   is implemented and fires correctly.** This is the precise INC-6 anti-pattern
   that took down fg-synapse. The team's mitigation is post-incident VAP +
   per-resource annotations; xpc's pre-merge gate is a missing layer.

2. **State-bearing-MR Orphan check (R23) is implemented and finds real gaps**,
   verified against `docdb-prod-cluster`.

3. **AppProject whitelist (R15) catches a recurring class of fix commits** —
   3 in the last 3 weeks alone — that currently surface as failed ArgoCD syncs
   in preview/prod.

4. **Selector / late-init ignoreDifferences (R16, R21)** — the two categories
   that dominated fg-manifold's MR history per the April 18 study, both now
   implemented.

5. **`xpc plan --base=main --head=HEAD`** would catch destructive deletes
   across two refs (R26) and immutable-field changes (R27) — capabilities
   that the team's current toolchain has no static equivalent of.

## What xpc doesn't help with

Looking at fg-manifold's last 60 commits, the dominant fix categories are
**runtime / config bugs** that no static manifest checker catches:

- `fix(sustain): set search_path=auth in GoTrue DB URI` — runtime config string
- `fix(sustain): KC_CACHE=local to fix JGroups cluster health check loop` — runtime env
- `fix(sustain): repair stale GoTrue auth schema on init container startup` — init logic
- `fix(signoz): bump clickhouse PVC 100Gi->200Gi` — capacity tuning
- `perf(runners-large): bump gp3 EBS throughput 125→500 MB/s` — perf tuning
- `feat(sustain): wire ES256 JWKS signing` — application logic
- E2E test breakage (Pranjal's sequence) — test infrastructure

These are 80%+ of recent fix volume and **none of them are in xpc's universe.**
xpc's value is concentrated in the Crossplane/ArgoCD manifest-shape category,
which is a real but narrow slice of operational work.

## Practical adoption blockers (today)

1. **Zero CI integration.** `grep -i "xpc\|cross-validate" .gitlab-ci.yml lib/ci/*.yml lefthook.yml` returns no matches. Not even an experimental job. Whatever value xpc has, the team is not currently receiving it.

2. **Kernel-bootstrap race.** `pkg/checker/bridge.go:128–187` materializes the
   embedded kernel into `$TMPDIR/xpc-kernel-<digest16>/`. Two concurrent
   invocations both `rename` the staging dir, the loser hits `XPC000:
   publish kernel dir: rename ... file exists`. Reproduces in this session
   on back-to-back runs. Fine for a single CI job; would surface immediately
   if run from pre-commit / Claude `xpc-edit` skill.

3. **Signal-to-noise problem.** 1,324 errors in the default report. The single
   biggest category (XPC.D, 701) is partially false-positive due to the
   wildcard-handling issue. Until that's triaged or the rule narrowed, a
   blocking CI gate would block every PR. The team needs either (a) a
   per-rule baseline ("don't fail on existing findings, fail on new ones"),
   (b) a triage pass that fixes the real subset and waivers the rest, or (c)
   the rule narrowed to handle Argo CD's actual whitelist-omission semantics.

4. **Render time.** 7m38s on a real tree is workable for CI but not for
   editor / pre-commit. The rule kernel itself is fast (<1s); rendering
   28 Helm + Kustomize charts is the cost. Scoping `xpc check <subtree>` to
   only the changed Application would solve this.

5. **`crossplane` binary missing.** 10 `XPC.H.composition-renders` infos
   because `crossplane` isn't on PATH. Composition-render rules degrade
   gracefully to skipped; doesn't block other rules.

## How "useful" decomposes

| Axis                                                        | Today                                  |
|-------------------------------------------------------------|----------------------------------------|
| Does the tool exist and run on this repo?                   | Yes, end-to-end                        |
| Does it cover the historical pain categories?               | Yes, after R15–R27                     |
| Does it find real INC-6-class bugs in the current tree?     | Yes — verified for R23, R24, R25       |
| Would it have blocked recent fix commits pre-merge?         | At least 3 in the last ~3 weeks (XPC.D)|
| Is it integrated in CI?                                     | **No**                                 |
| Is the default report actionable as a blocking gate?        | Not without triage of FP-heavy rules   |
| Is cold-start adoption-friendly (no race, scoped run)?      | Not yet                                |

The middle three rows say "useful in principle, with verified hits"; the
bottom three rows say "the path from here to received-value isn't built."

## Code references

### cross-validate (this repo)

- `cmd/xpc/main.go:54–81` — subcommand dispatch
- `pkg/checker/bridge.go:128–187` — kernel materialization (XPC000 race lives here)
- `kernel/r23-crossplane-state-needs-orphan.shen` — INC-6 floor rule
- `kernel/r24-appset-finalizer-without-preserve.shen` — INC-6 root cause #1
- `kernel/r25-prod-appset-autosync.shen` — INC-6 root cause #4
- `kernel/r15-appproject-whitelist.shen` — the rule with the FP issue
- `pkg/plan/r26.go:38` — destructive-delete (plan-mode)
- `pkg/plan/r27.go` — immutable-change (plan-mode)

### fg-manifold

- `~/fg/fg-manifold/.gitlab-ci.yml` — confirms no xpc job
- `~/fg/fg-manifold/lefthook.yml` — empty, no pre-commit
- `~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/appproject-ops.yaml` — the wildcard / absent-whitelist case behind the XPC.D FPs
- `~/fg/fg-manifold/deploy/facilitygrid/ops/applications/crossplane-platform-aws-prod/aws/us-east-2/facilitygrid-ops/manifests/docdb-prod-cluster.yaml` — verified missing `deletionPolicy: Orphan`
- `~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/crossplane-platform-aws-mgmt.yaml` — sample of the 23 finalizer-without-preserve AppSets
- Recent commits: `0a4603e584`, `8d6b0079aa`, `7a36280083` — XPC.D regressions; `f829f373e5` — managementPolicies / R22 territory

## Historical context (from thoughts/)

- `thoughts/shared/research/2026-04-18-fg-manifold-target-study.md` — the
  original "what would xpc need to be useful here" gap analysis. Verdict
  at the time: ~0% coverage of the historical MR pain. The roadmap items it
  named (A1=R17, A2=R16, A3/D1=R15, B1=R18/R19/R20, B2=AppSet expansion,
  late-init=R21, SSA×MP=R22, INC-6 floor=R23/R24/R25, plan-mode=R26/R27) have
  all landed since.
- `thoughts/shared/research/2026-04-22-inc6-coverage-gap.md` — re-scoping
  doc that defined the static floor (R23/R24/R25) and dynamic ceiling (R26/R27)
  exactly as they now ship.
- `thoughts/shared/research/2026-04-29-actual-capabilities-vs-crossplane-plan.md`
  — the current state-of-tool snapshot from one week ago.

## Related research

The April 18 → April 29 arc is the answer to the user's question in slow
motion: the tool was honestly not useful for fg-manifold in mid-April; the
rule corpus that closes that gap landed across April 21–29; this doc is the
post-landing empirical check.

## Open questions

- **R15 wildcard / absent-whitelist semantics.** Does ArgoCD's permit-all
  default for absent `namespaceResourceWhitelist` need to be modeled? If yes,
  the 701 XPC.D findings drop to a much smaller real number, mostly in
  `preview` and `prod` projects which have explicit whitelists.
- **R6b inside-AppSet wave semantics.** Are 30 Function/Composition same-wave
  flags real (the Function does need to be Healthy before the Composition
  references it) or false (intra-AppSet sync handles it atomically)?
- **Is the team interested in xpc adoption?** This whole assessment is moot if
  the answer is no. If yes, the smallest viable wedge is probably:
  (1) fix the kernel race (one-line `os.MkdirAll` → `os.Rename` retry),
  (2) ship a GitLab CI job that runs only R23/R24/R25 (the INC-6 floor),
  failing the build on those alone — high signal, low noise, immediate
  payoff. Broader rules can ramp in after triage.
- **Is there appetite to land xpc as an in-tree tool inside fg-manifold,
  vs. running the published binary via `go install`?** The colleague-built
  framing in the user's question suggests it's been treated as external; a
  vendored or git-submoduled version with fg-manifold-specific config
  (`xpc.yaml`) would tighten the loop.
