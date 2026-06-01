# Offline preview-diffs for Crossplane PRs

*2026-05-04T17:33:12Z by Showboat 0.6.1*
<!-- showboat-id: 8cf4872b-ffb0-4c75-850d-e3190681b12f -->

This document shows the four-phase build that lets `xpc plan` produce GitLab/GitHub PR comments offline — no in-cluster operator, no cluster credentials in CI. Each phase is independently committed; this demo runs them end-to-end against the `plan-destructive` fixture.

## Phase 1 — Extend `.xpcsnap` to carry cluster state

The snapshot file format gained four optional slices (`resources`, `argo_apps`, `argo_app_sets`, `argo_projects`), behind a `--include-resources` flag. `omitempty` keeps existing type-env-only snapshots byte-identical.

```bash
./xpc snapshot --include-resources --output=/tmp/demo.xpcsnap testdata/fixtures/plan-destructive/base
```

```output
snapshot written to /tmp/demo.xpcsnap
sha256:1e9ad2ac9c80087bc406a1d2fc8a2cba16adab2b80e7f1d4380cd4bf4e816f5a
```

```bash
jq '{version, clusterName, resourceCount: (.resources | length), firstResource: .resources[0] | {apiVersion, kind, name, namespace}}' /tmp/demo.xpcsnap
```

```output
{
  "version": 1,
  "clusterName": "local",
  "resourceCount": 1,
  "firstResource": {
    "apiVersion": "rds.aws.upbound.io/v1beta1",
    "kind": "Cluster",
    "name": "aurora-prod-cluster",
    "namespace": "crossplane-system"
  }
}
```

## Phase 2 — `xpc plan` accepts `.xpcsnap` as base or head

`pkg/plan/runner.go` sniffs the `.xpcsnap` extension before the directory branch and dispatches to `runVariantFromSnapshot`, which loads the file and rebuilds the World via `snap.ToWorld()`. Same `Diff` and Markdown output downstream.

The destructive-removal fixture below removes a state-bearing `Cluster` between base and head. xpc plan should flag it as INC-6-shaped and surface a fix recipe.

```bash
./xpc plan --base=/tmp/demo.xpcsnap --head=testdata/fixtures/plan-destructive/head --skip-render testdata/fixtures/plan-destructive/head
```

```output
## xpc plan: /tmp/demo.xpcsnap → testdata/fixtures/plan-destructive/head

### ⚠ Destructive changes (1)

- Cluster crossplane-system/aurora-prod-cluster would be removed
  - Cluster is state-bearing (Group=rds.aws.upbound.io). Base-side spec.deletionPolicy is absent (Crossplane default is Delete). Applying this change will run a real destructive call against the external object. This is the INC-6 failure shape.
  - **Fix:** Either (a) keep the resource on HEAD (revert the removal), (b) set spec.deletionPolicy: Orphan on the base side before removing the CR, or (c) add annotation xpc.io/allow-delete: "true" to the base manifest if the destruction is genuinely intended.

### Resource changes

- Added: 0
- Modified: 0
- Removed: 1

### Per-tip diagnostics

- base (/tmp/demo.xpcsnap): 1 errors, 0 warnings, 0 info
- head (testdata/fixtures/plan-destructive/head): 1 errors, 0 warnings, 0 info
```

## Phase 3 — `--post-comment` for GitLab and GitHub

The CLI accepts `gitlab://group/proj/-/merge_requests/N`, `github://owner/repo/pull/N`, or `auto` (inferred from CI env vars). Posting shells out to `glab` / `gh`; xpc never touches the token. `--dry-run` resolves the target and reports byte count without posting.

```bash
./xpc plan --base=/tmp/demo.xpcsnap --head=testdata/fixtures/plan-destructive/head --skip-render --post-comment=gitlab://example/repo/-/merge_requests/42 --dry-run testdata/fixtures/plan-destructive/head 2>&1 | tail -3
```

```output
- base (/tmp/demo.xpcsnap): 1 errors, 0 warnings, 0 info
- head (testdata/fixtures/plan-destructive/head): 1 errors, 0 warnings, 0 info
would post 904 bytes to gitlab MR example/repo!42
```

## Phase 4 — Capture helper + documentation

A bash helper dumps live cluster state into a directory, optionally piping through `xpc snapshot --include-resources` to produce a single `.xpcsnap`. Cluster-shape concerns (which CRD groups, which namespaces to skip) live in the script, not the lint tool.

```bash
ls tools/xpc-capture-cluster*.sh && echo --- && ls docs/preview-diffs.md docs/templates/gitlab-ci.yml
```

```output
tools/xpc-capture-cluster-snap.sh
tools/xpc-capture-cluster.sh
---
docs/preview-diffs.md
docs/templates/gitlab-ci.yml
```

```bash
head -30 tools/xpc-capture-cluster.sh
```

```output
#!/usr/bin/env bash
# tools/xpc-capture-cluster.sh
#
# Dumps live cluster resources into a directory layout consumable by
# `xpc plan --base=<dir>` or wrappable via xpc-capture-cluster-snap.sh.
#
# Runtime requirements: kubectl >= 1.24, jq >= 1.6.
set -euo pipefail
trap 'echo "error: command failed at line $LINENO" >&2' ERR

usage() {
  cat <<'EOF'
Usage: tools/xpc-capture-cluster.sh [options] <output-dir>

Dumps live cluster resources into a directory layout consumable by
`xpc plan --base=<dir>` or wrappable via xpc-capture-cluster-snap.sh.

Options:
  --providers=<csv>        CRD group patterns (suffix match) to dump.
                           Default: aws.upbound.io,gcp.upbound.io,
                                    azure.upbound.io,crossplane.io
  --skip-namespaces=<csv>  Skip resources in these namespaces.
                           Default: kube-system,kube-public,kube-node-lease
                           (No-op for v1; documented for forward compat.)
  --include-argo           Capture ArgoApplications/AppSets/AppProjects (default).
  --no-argo                Skip Argo objects.
  --kubeconfig=<path>      Override KUBECONFIG (otherwise inherits env).
  --dry-run                Print kubectl invocations; do not write files.
  --quiet                  Suppress per-kind progress messages on stderr.
  -h, --help               Show usage and exit.
```

## What's left

- The fidelity test `TestPlan_FromSnapshot` in `pkg/plan/plan_test.go` currently fails: per-tip Base diagnostics drop the `XPC.S.crossplane-state-needs-orphan` code on the snapshot path even though the destructive Delta itself is detected (visible above). The Markdown body is correct; only the Base-side diagnostic count is short by one rule. Fix would belong in `runVariantFromSnapshot` — derived facts (`CPDeletionPolicyFacts`) need to be re-extracted after `ToWorld`.
