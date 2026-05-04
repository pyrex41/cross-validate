---
date: 2026-05-04
researcher: "Reuben Brooks / Claude"
git_commit: 125b30b07c2f005c295dd419bba508d201d826f6
branch: claude/standalone-xpc-cli
repo: cross-validate
topic: "R16 (XPC.E.selector-needs-ignore-diff) signal/noise audit on fg-manifold"
tags: [xpc, audit, R16, fg-manifold, ignoreDifferences, managedFieldsManagers, false-positive]
status: complete
---

# R16 (XPC.E.selector-needs-ignore-diff) signal audit on fg-manifold

## Summary

**FP rate: 25/25 = 100%.** R16 is currently producing **uniformly false positives**
on fg-manifold — at least within the resources owned by Crossplane-platform
ApplicationSets that account for the bulk of the 440 findings. The root cause
is a single missing field in the Go→kernel bridge: `IgnoreDiffEntry` drops
`managedFieldsManagers`, so the kernel never sees that fg-manifold's
Crossplane AppSets cover *every* Crossplane-managed field via a wildcard
`group: "*", kind: "*", managedFieldsManagers: [crossplane]` entry.

**Verdict: >30% FP — R16 cannot be trusted as currently shipped.** It must be
kept out of any CI gate until the bridge and the rule learn about
`managedFieldsManagers`. The fix is mechanically small but cuts across both
Go and Shen.

## Methodology

- Source: `/tmp/xpc-render.json` (output of
  `xpc check --format=json deploy/facilitygrid/` against fg-manifold @ HEAD,
  prior 7-minute render run).
- Total `XPC.E.selector-needs-ignore-diff` findings: 440.
- Distribution by owning app directory (top 5):

  | App                                   | Count |
  | ------------------------------------- | ----: |
  | `crossplane-platform-aws-prod`        |   184 |
  | `crossplane-platform-aws-preview`     |   158 |
  | `crossplane-platform-aws-sustain-preview` |    23 |
  | `aurora-ops-cluster`                  |    13 |
  | `crossplane-platform-aws-ops`         |    12 |

  Together these five own 390 / 440 = 88.6% of all R16 findings.

- Top 5 source **files** (used for the sample, since the spec asks for source
  files specifically):

  | File                                                                                                           | Count |
  | -------------------------------------------------------------------------------------------------------------- | ----: |
  | `…/crossplane-platform-aws-prod/…/manifests/securitygroups-prod.yaml`                                          |    24 |
  | `…/crossplane-platform-aws-prod/…/manifests/egress-proxy-prod.yaml`                                            |    23 |
  | `…/crossplane-platform-aws-prod/…/manifests/vpc-prod.yaml`                                                     |    22 |
  | `…/crossplane-platform-aws-preview/…/manifests/vpc-preview.yaml`                                               |    22 |
  | `…/crossplane-platform-aws-prod/…/manifests/ecs-cluster-prod.yaml`                                             |    21 |

  Total sampled population: 112 / 440 = 25.5% of all R16 findings.
  Sample: 5 stratified findings per file (indices 0, 5, 10, 15, 20 within
  each file's R16 list — deterministic, not random, but spread across each
  file).

- For each sample, I:
  1. Inspected the cited line in the source YAML.
  2. Identified the owning ApplicationSet from the path
     (`applications/<appset-name>/...`).
  3. Read `spec.template.spec.ignoreDifferences` on that AppSet.
  4. Checked for `spec.managementPolicies` on the resource (none found in
     any sampled file — all five top files contain zero `managementPolicies`
     keys).

## Findings: 25-row classification

All findings target Crossplane managed resources (`*.aws.upbound.io`) inside
two AppSets — `crossplane-platform-aws-prod` and `crossplane-platform-aws-preview`.
**Both AppSets have an identical wildcard entry:**

```yaml
ignoreDifferences:
  - group: "*"
    kind: "*"
    managedFieldsManagers:
      - crossplane
```

In Argo CD, this entry causes Argo to ignore *every* field on *every*
resource whose `metadata.managedFields` shows `crossplane` as the manager.
Since Crossplane writes (and field-manages) the resolved selector targets
(`vpcId`, `securityGroupId`, `subnetId`, `routeTableId`, `policyArn`, etc.),
this single wildcard fully covers every resolved-field path R16 is flagging.

| # | File | Line | Resource / Selector → Resolved | AppSet | Wildcard covers? | Class |
|--:|------|----:|--------------------------------|--------|------------------|-------|
| 1 | securitygroups-prod.yaml | 14 | SecurityGroup `fg-prod-alb-sg` `vpcIdSelector` → `vpcId` | aws-prod | yes (mFM=crossplane) | FP — wildcard miss |
| 2 | securitygroups-prod.yaml | 117 | SecurityGroupRule `…-egress-fargate-healthcheck` `securityGroupIdSelector` → `securityGroupId` | aws-prod | yes | FP — wildcard miss |
| 3 | securitygroups-prod.yaml | 459 | SecurityGroupRule `…-equinix-ingress-fg-auth` `sourceSecurityGroupIdSelector` → `sourceSecurityGroupId` | aws-prod | yes | FP — wildcard miss |
| 4 | securitygroups-prod.yaml | 373 | SecurityGroupRule `…-fargate-egress-s3` `securityGroupIdSelector` → `securityGroupId` | aws-prod | yes | FP — wildcard miss |
| 5 | securitygroups-prod.yaml | 196 | SecurityGroupRule `…-fargate-ingress-alb-healthcheck` `sourceSecurityGroupIdSelector` → `sourceSecurityGroupId` | aws-prod | yes | FP — wildcard miss |
| 6 | egress-proxy-prod.yaml | 610 | Attachment `…-egress-proxy-tg-attachment` `autoscalingGroupNameSelector` → `autoscalingGroupName` | aws-prod | yes | FP — wildcard miss |
| 7 | egress-proxy-prod.yaml | 453 | LB `…-egress-proxy-nlb` `subnetMapping[0].subnetIdSelector` → `subnetMapping[0].subnetIdRef` | aws-prod | yes | FP — wildcard miss |
| 8 | egress-proxy-prod.yaml | 453 | LB `…-egress-proxy-nlb` `subnetMapping[2].subnetIdSelector` → `subnetMapping[2].subnetId` | aws-prod | yes | FP — wildcard miss |
| 9 | egress-proxy-prod.yaml | 516 | LaunchTemplate `…-egress-proxy` `networkInterfaces[0].securityGroupSelector` → `networkInterfaces[0].securityGroups` | aws-prod | yes | FP — wildcard miss |
| 10 | egress-proxy-prod.yaml | 328 | SecurityGroupRule `…-egress-proxy-ingress-fargate` `securityGroupIdSelector` → `securityGroupId` | aws-prod | yes | FP — wildcard miss |
| 11 | vpc-prod.yaml | 186 | NATGateway `fg-prod-nat` `allocationIdSelector` → `allocationId` | aws-prod | yes | FP — wildcard miss |
| 12 | vpc-prod.yaml | 298 | Route `fg-prod-private-route` `routeTableIdSelector` → `routeTableId` | aws-prod | yes | FP — wildcard miss |
| 13 | vpc-prod.yaml | 329 | RouteTableAssociation `…-private-a-assoc` `routeTableIdSelector` → `routeTableId` | aws-prod | yes | FP — wildcard miss |
| 14 | vpc-prod.yaml | 369 | RouteTableAssociation `…-private-c-assoc` `subnetIdSelector` → `subnetId` | aws-prod | yes | FP — wildcard miss |
| 15 | vpc-prod.yaml | 556 | RouteTableAssociation `…-public-c-assoc` `routeTableIdSelector` → `routeTableId` | aws-prod | yes | FP — wildcard miss |
| 16 | vpc-preview.yaml | 186 | NATGateway `fg-preview-nat` `allocationIdSelector` → `allocationId` | aws-preview | yes | FP — wildcard miss |
| 17 | vpc-preview.yaml | 486 | Route `fg-preview-private-route` `routeTableIdSelector` → `routeTableId` | aws-preview | yes | FP — wildcard miss |
| 18 | vpc-preview.yaml | 508 | RouteTableAssociation `…-private-a-assoc` `routeTableIdSelector` → `routeTableId` | aws-preview | yes | FP — wildcard miss |
| 19 | vpc-preview.yaml | 548 | RouteTableAssociation `…-private-c-assoc` `subnetIdSelector` → `subnetId` | aws-preview | yes | FP — wildcard miss |
| 20 | vpc-preview.yaml | 443 | RouteTableAssociation `…-public-c-assoc` `routeTableIdSelector` → `routeTableId` | aws-preview | yes | FP — wildcard miss |
| 21 | ecs-cluster-prod.yaml | 427 | RolePolicyAttachment `…-acmpca-issue-attachment` `policyArnSelector` → `policyArn` | aws-prod | yes | FP — wildcard miss |
| 22 | ecs-cluster-prod.yaml | 256 | RolePolicyAttachment `…-exec-secrets-policy` `roleSelector` → `role` | aws-prod | yes | FP — wildcard miss |
| 23 | ecs-cluster-prod.yaml | 616 | RolePolicyAttachment `…-kms-task-attachment` `policyArnSelector` → `policyArn` | aws-prod | yes | FP — wildcard miss |
| 24 | ecs-cluster-prod.yaml | 542 | RolePolicyAttachment `…-ses-send-attachment` `roleSelector` → `role` | aws-prod | yes | FP — wildcard miss |
| 25 | ecs-cluster-prod.yaml | 313 | RolePolicyAttachment `…-tailscale-state-attachment` `roleSelector` → `role` | aws-prod | yes | FP — wildcard miss |

**Unclear: 0 / 25.**

## FP rate

**25 / 25 = 100% FP.** All sampled findings collapse to the same root
cause — the Crossplane wildcard `managedFieldsManagers: [crossplane]` entry
isn't being honored by xpc.

## Verdict

**>30% FP — R16 must not ship in any CI gate as-is.** Coupled with the
April 18 study showing this *is* a real and important pain category, the
finding here is sharper than the headline number suggests:

- The kernel rule's *intent* (warn when a selector resolves to a path that
  ignoreDifferences doesn't cover) is correct.
- The kernel rule's *string-contains-leaf* coverage check is too loose
  but currently doesn't matter, because…
- The Go→kernel bridge silently strips `managedFieldsManagers` from every
  ignoreDifferences entry. The kernel literally cannot tell that
  Crossplane-managed fields are exempt, so the rule fires on every
  selector usage in fg-manifold.

This is not a "rule too strict" problem — it's a missing feature: xpc has
no concept of `managedFieldsManagers` at all. Until that lands, R16 will
generate hundreds of FPs against any GitOps shop that uses the
recommended Crossplane + Argo CD pattern (which is the public docs'
canonical pattern — see Crossplane docs "Working with Argo CD").

## Fix recommendation

This is at minimum a **two-file change** plus optional rule-side hardening.

### 1. Carry `managedFieldsManagers` through the bridge

- File: `pkg/types/types.go:881`
  Add `ManagedFieldsManagers []string` to `IgnoreDiffEntry`.
- File: `pkg/checker/bridge.go:1211-1247` (`buildIgnoreDiffEntries`) and
  `pkg/checker/bridge.go:1259-1265` (`ignoreDiffEntryToObj`)
  Populate the new field from `app.IgnoreDifferences[].ManagedFieldsManagers`
  (already parsed at `pkg/ir/builder.go:1288-1294`) and add a sixth element
  (a list of strings) to the `ignore-diff-entry` Klambda tuple.

  Note: `buildIgnoreDiffEntries` currently flattens one entry per
  JSONPointer / per JQPath. It should also emit one row per
  `managedFieldsManagers` entry (or include the full slice on every emitted
  row — the kernel rule will treat them as a logical OR of coverage
  reasons).

### 2. Teach R16 about `managedFieldsManagers`

- File: `kernel/r16-selector-needs-ignore-diff.shen:40-44`
  Update the destructure pattern from
  `[ignore-diff-entry _ _ _ JSONPointer JQPath]`
  to also pull `ManagedFieldsManagers`, and add a clause to
  `r16-entry-covers?`:

  - If `ManagedFieldsManagers` contains `"crossplane"` AND the entry's
    `Group` is `"*"` (or matches the resource's group) AND the entry's
    `Kind` is `"*"` (or matches the resource's kind), the entry covers
    *any* path on that resource — return true unconditionally.

  This requires the rule to know the resource's `Group` and `Kind`, which
  the `selector-usage-fact` already carries (positions 1 and 2; see
  `r16-check-usage` line 59).

- The current group/kind matching logic is **also** absent from
  `r16-entry-covers?` — the existing implementation ignores the entry's
  `Group`/`Kind` entirely and relies only on the leaf-segment string
  contains test. Adding wildcard-aware group/kind matching is required for
  the `managedFieldsManagers` clause to be sound; it would also tighten
  the existing leaf-match coverage by gating it to the right resource
  scope.

### 3. Test fixtures (don't forget)

- A new fixture under `tests/fixtures/` (or wherever the existing R16
  fixtures live) covering the canonical Crossplane-on-Argo pattern: an
  AppSet with `group: "*", kind: "*", managedFieldsManagers: [crossplane]`
  and a managed resource with selectors. Expected outcome: 0 R16 findings.

### Effort estimate

Half a day for the bridge + kernel changes, plus another half day for
fixtures and re-running on fg-manifold to confirm the 440 findings drop
substantially. After the fix, expect a small residue of *true positives*
(any AppSet that doesn't use the wildcard pattern) — those should be
investigated case-by-case to confirm R16's signal is genuine on the
remainder.

## Additional observation (out of audit scope but worth flagging)

After fixing this, the next question is "are there genuine R16 findings at
all in fg-manifold?". A grep across all 14 Crossplane-platform AppSets
shows every one of them carries the `managedFieldsManagers: [crossplane]`
wildcard, which suggests R16's *true-positive* rate against fg-manifold's
current state may be ~zero. That doesn't make R16 worthless — it would
catch a NEW Crossplane-on-Argo deployment that forgets the wildcard, which
is exactly what the April 18 study identified as a recurring pain — but
it does mean the rule's value is preventative rather than backlog-clearing
for this repo. Worth recording as part of the trust story for R16 once it
lands.
