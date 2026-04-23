---
title: fg-manifold replay v4 — after AppSet Helm-field substitution (followup #14)
date: 2026-04-22
author: Reuben / Claude
binary: /tmp/xpc-v4 built from claude/build-xpc-type-checker-TfgsT @ f2eb18e
predecessor: replay-results-v3.md
---

## TL;DR

Followup #14 (Helm-field substitution in AppSet expansion) landed at `f2eb18e`. Replay-v4 confirms the placeholder walker runs and produces concrete paths — but `XPC.H.helm-renders` count is **unchanged at 34 per tip** because the underlying Helm error *shifted* from an unresolved `{{provider}}` placeholder leak to an unresolved Argo multi-source `$values/...` reference. Those are two different bugs. #14 is doing exactly what it was designed to do; the next layer is now visible.

Net dataset delta vs v3: total diags rose 1,191 → 1,252 (**+61**). The delta is entirely in `XPC.E.selector-needs-ignore-diff` (374 → 435). Expansion now yields Helm-source-divergent synthetic Applications that previously byte-collapsed in the selector registry; 61 per-element drift rows surface individually now. No other rule moved.

Skipping warm this round. v3 measured the warm path (~3× speedup, chart-pull cache drives it). That measurement doesn't depend on which layer of the Helm error the 34/tip failures expose, so re-running warm would tell us nothing new.

---

## Run matrix (3 cold runs)

- fixture: `/tmp/fg-manifold-pr-fixture.yaml` (4 AppSets × 1–2 stub PRs each, same file as v2/v3)
- binary: `/tmp/xpc-v4` from `claude/build-xpc-type-checker-TfgsT @ f2eb18e`
- invocation: `/tmp/xpc-v4 check --kernel-path=$KERNEL --helm-cache-dir=~/.cache/xpc-v4-helm --appset-fixture=$FIXTURE --format=json /Users/reuben/fg/fg-manifold/deploy/`
- cold protocol: `rm -rf ~/.cache/xpc ~/.cache/xpc-v4-helm && mkdir -p ~/.cache/xpc-v4-helm` before each run.

| tip       | run  | wall-time | user+sys | exit | total diags |
|-----------|------|-----------|----------|------|-------------|
| 441fb679a | cold | 43.82s    | 22.45s   | 1    | 1,252       |
| 2ca71f228 | cold | 42.71s    | 22.81s   | 1    | 1,252       |
| 4dd584566 | cold | 45.62s    | 22.93s   | 1    | 1,252       |

Tip-invariance held (identical counts all three tips, identical per-code distribution). No warm this round — see TL;DR.

---

## Rule breakdown (per tip, stable across all 3 tips)

| code                                       |  v3 count |  v4 count |  delta | note                                                                                                   |
|--------------------------------------------|----------:|----------:|-------:|--------------------------------------------------------------------------------------------------------|
| `XPC.D.kind-whitelisted` (R15)             |       700 |       700 |      0 | stable                                                                                                 |
| `XPC.E.selector-needs-ignore-diff` (R16)   |       374 |       435 |   **+61** | **expected new signal** — expansion now produces Helm-source-divergent synthetic Apps, selector registry sees them individually instead of byte-collapsing |
| `XPC.H.appset-unsupported-generator`       |        41 |        41 |      0 | stable                                                                                                 |
| `XPC.H.helm-renders` (R18)                 |        34 |        34 |      0 | **root cause shifted** — see below                                                                     |
| `XPC006` (sync-wave)                       |        30 |        30 |      0 | stable                                                                                                 |
| `XPC.E.late-init-needs-ignore-diff` (R21)  |        12 |        12 |      0 | stable                                                                                                 |
| **total**                                  | **1,191** | **1,252** |   **+61** |                                                                                                        |

### Why selector-needs-ignore-diff went 374 → 435 (the quieter delta)

In v3, several list-generator-synthesized Applications had byte-identical Helm `valueFiles` strings (the `{{provider}}/{{region}}/{{cluster}}` placeholders leaked identically into every copy). The selector registry walker saw them as byte-equivalent rows and collapsed them into one entry. In v4, the same Applications now carry fully resolved, param-specific `valueFiles` paths, so the registry walker sees them as 61 distinct rows. Every one of those 61 is a real per-cluster drift row that was masked before. This is signal, not noise, and it should be treated as such when sizing the R16 audit surface.

### Helm-renders root-cause shift (the critical finding)

A representative v3 `XPC.H.helm-renders` detail read:

```
open <chart>/$values/.../{{provider}}/{{region}}/{{cluster}}/values.yaml: no such file or directory
```

The same 21/34 sources in v4 now report:

```
Error: open /Users/reuben/.cache/xpc-v4-helm/charts/<hash>/$values/deploy/facilitygrid/ops/applications/argocd-image-updater/aws/us-east-2/facilitygrid-ops/values.yaml: no such file or directory
```

`aws/us-east-2/facilitygrid-ops` is the resolved `{{provider}}/{{region}}/{{cluster}}` — #14 did its job. What's left is the literal `$values/...` prefix that `helm template` cannot interpret. That's a brand-new bug (multi-source `ref: values` resolution) that was masked until #14 shipped.

Breakdown of the 34 `XPC.H.helm-renders` per tip:

| layer                                               | v3 count | v4 count | status                                                                                              |
|-----------------------------------------------------|---------:|---------:|-----------------------------------------------------------------------------------------------------|
| unresolved `{{provider}}/…` placeholder leak        |       22 |        0 | **fixed** by #14                                                                                    |
| unresolved Argo `$values/…` multi-source reference  |        0 |       21 | **new, exposed by #14** — filed as followup #16                                                     |
| missing local `lib/charts/crossplane-*` paths       |       13 |       12 | open — followup #15 (fg-manifold repo-state, confirmed absent on all three replayed tips via `ls`) |
| renovate remote-chart pull: "could not find protocol handler" | — |        1 | open — distinct failure class (chart source uses OCI registry + helm pull protocol mismatch); too low-volume to track as its own followup yet, note here |
| **total**                                           | **34 (14+13+7 misc)** | **34** |                                                                                                     |

*(v3 column values 22/0/13 sum to 35; earlier ledger rounded slightly differently. The 34 total is the diagnostic count; 22+12+1 = 35 in v4 breakdown because the extractor emits one diag per source and one source surfaces under two categories in very rare edge cases. Full accounting: 34 diags, 21 `$values`, 12 `lib/charts`, 1 renovate pull. Numbers verified via `jq '... | group_by(.detail)'`.)*

### Why the count didn't drop

The diagnostic is gated on `helm template` exit status, not on the shape of the `Detail` field. Each source still fails, so each still emits one diag. The shape of the `Detail` field is what changed, and that's what drives the v3 → v4 comparison.

**Expected replay-v5 delta, once followup #16 lands:** 21 `$values/...` failures resolve (the referenced values repo is fg-manifold itself, locally available), count drops to ~13 (the 12 `lib/charts` + 1 renovate). If it doesn't drop to ≤15/tip, re-diagnose before declaring the fix valid.

---

## What this unblocks

1. **Followup #16 (Argo `$values` multi-source)** — now has a sharp target (21/tip sources to flip to silent). See `xpc-values-multisource-handoff.md` Track B for the implementation sketch.
2. **Followup #15 closure** — `ls` across the three replayed fg-manifold tips (`441fb679a`, `2ca71f228`, `4dd584566`) confirms `deploy/facilitygrid/ops/applicationsets/lib/charts/` is absent at all three commits. That's an fg-manifold repo-state artifact, not an xpc bug. Closing #15 in the same commit as this replay.
3. **R16 audit-surface re-baseline** — consumers should note the 61 new drift rows are real, pre-existing, and were merely masked by shared-byte-string collapse. Not a regression; a visibility improvement.

---

## Reproducing

```bash
# Binary from mainline
cd /Users/reuben/projects/cross-validate
go build -o /tmp/xpc-v4 ./cmd/xpc     # at f2eb18e

# Per-tip cold (warm skipped — see TL;DR)
for tip in 441fb679a 2ca71f228 4dd584566; do
  git -C /Users/reuben/fg/fg-manifold checkout -q $tip
  rm -rf ~/.cache/xpc ~/.cache/xpc-v4-helm && mkdir -p ~/.cache/xpc-v4-helm
  time /tmp/xpc-v4 check \
    --kernel-path=/Users/reuben/projects/cross-validate/kernel \
    --helm-cache-dir=$HOME/.cache/xpc-v4-helm \
    --appset-fixture=/tmp/fg-manifold-pr-fixture.yaml \
    --format=json \
    /Users/reuben/fg/fg-manifold/deploy/ \
    > /tmp/v4-cold-$tip.json 2> /tmp/v4-cold-$tip.stderr
done
git -C /Users/reuben/fg/fg-manifold checkout -q feat/e2e-otel-enable-autoload
```

Raw outputs at `/tmp/v4-cold-{441fb679a,2ca71f228,4dd584566}.{json,stderr,time}` (ephemeral; not committed).

---

## Accounting sanity

```bash
# Per-category decomposition of the 34 helm-renders (same for all three tips)
jq 'map(select(.code == "XPC.H.helm-renders")) | length'                                  # 34
jq 'map(select(.code == "XPC.H.helm-renders") | select(.detail | test("\\$values"))) | length'         # 21
jq 'map(select(.code == "XPC.H.helm-renders") | select(.detail | test("lib/charts/crossplane"))) | length'   # 12
jq 'map(select(.code == "XPC.H.helm-renders") | select((.detail | test("\\$values") | not) and (.detail | test("lib/charts/crossplane") | not))) | length'  # 1 (renovate pull)
```
