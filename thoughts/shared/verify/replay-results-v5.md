---
title: fg-manifold replay v5 — after Argo $values multi-source resolution (followup #16)
date: 2026-04-22
author: Reuben / Claude
binary: /tmp/xpc-v5 built from claude/build-xpc-type-checker-TfgsT @ b6ab26b
predecessor: replay-results-v4.md
---

## TL;DR

Followup #16 (Argo `$values` multi-source resolution) landed at `b6ab26b`. Replay-v5 against the same three fg-manifold tips used by v3/v4 **fully eliminates the `$values`-leak failure class**: 21 per-tip diagnostics → 0. `XPC.H.helm-renders` drops from 34 → **15** per tip, matching the predicted ~13 plus 2 newly-exposed YAML parse errors from charts that now actually render end-to-end.

Net dataset delta vs v4: total diags rose 1,252 → 1,551 (**+299**). The delta lives in `XPC.D.kind-whitelisted` (700 → 1,018, +318) because helm template now produces real rendered resources for ~19 charts that previously errored out; their kinds flow into World.Resources and R15 gets a new per-resource whitelist check surface. This is signal, not regression — the same "successful render exposes masked signal" pattern we saw for R16 in v4.

Skipping warm runs again. v3 measured the warm path (~3× speedup driven by chart-pull caching); that measurement is orthogonal to which error layer R18 exposes.

---

## Run matrix (3 cold runs)

- fixture: `/tmp/fg-manifold-pr-fixture.yaml` (same as v2/v3/v4)
- binary: `/tmp/xpc-v5` from `claude/build-xpc-type-checker-TfgsT @ b6ab26b`
- invocation: `/tmp/xpc-v5 check --kernel-path=$KERNEL --helm-cache-dir=~/.cache/xpc-v5-helm --appset-fixture=$FIXTURE --format=json /Users/reuben/fg/fg-manifold/deploy/`
- cold protocol: `rm -rf ~/.cache/xpc ~/.cache/xpc-v5-helm && mkdir -p ~/.cache/xpc-v5-helm` before each run.

| tip       | run  | wall-time | user+sys | exit | total diags |
|-----------|------|-----------|----------|------|-------------|
| 441fb679a | cold | 2:23.01   | 67.76s   | 1    | 1,551       |
| 2ca71f228 | cold | 2:05.40   | 65.83s   | 1    | 1,551       |
| 4dd584566 | cold | 1:57.99   | 61.78s   | 1    | 1,551       |

Tip-invariance held (identical counts all three tips, identical per-code distribution). Wall-time and user+sys both ~3× v4 — successful renders do actual work (values merging, helm template, YAML parse, resource walk) that in v4 short-circuited on `$values` open-file errors. This is expected cost; warm runs recover most of it via chart-pull caching (see v3 for the 3× warm-speedup baseline).

---

## Rule breakdown (per tip, stable across all 3 tips)

| code                                       |  v4 count |  v5 count |   delta | note                                                                                               |
|--------------------------------------------|----------:|----------:|--------:|----------------------------------------------------------------------------------------------------|
| `XPC.D.kind-whitelisted` (R15)             |       700 |     1,018 |   **+318** | **expected new signal** — renders now succeed, new rendered kinds flow into R15's surface         |
| `XPC.E.selector-needs-ignore-diff` (R16)   |       435 |       435 |       0 | stable                                                                                             |
| `XPC.H.appset-unsupported-generator`       |        41 |        41 |       0 | stable                                                                                             |
| `XPC.H.helm-renders` (R18)                 |        34 |        **15** |   **−19** | **primary target** — 21 `$values` leaks resolved; 2 new YAML parse errors surface from newly-successful renders |
| `XPC006` (sync-wave)                       |        30 |        30 |       0 | stable                                                                                             |
| `XPC.E.late-init-needs-ignore-diff` (R21)  |        12 |        12 |       0 | stable                                                                                             |
| **total**                                  | **1,252** | **1,551** | **+299** |                                                                                                    |

### Helm-renders decomposition (the one that matters)

| layer                                              | v4 count | v5 count | status                                                                                   |
|----------------------------------------------------|---------:|---------:|------------------------------------------------------------------------------------------|
| unresolved Argo `$values/...` multi-source ref     |       21 |        0 | **fixed** by #16                                                                         |
| missing local `lib/charts/crossplane-*` paths      |       12 |       12 | open — followup #15 (fg-manifold repo-state; not actionable from xpc side)               |
| renovate remote-chart pull protocol handler        |        1 |        1 | open — chart source uses OCI registry + helm pull protocol mismatch; low-volume, no ticket yet |
| parsing rendered YAML (duplicate mapping key)      |        0 |        2 | **new, exposed by #16** — charts that used to die on `$values` now render end-to-end; rendered output has duplicate keys. Filed internally below. |
| **total**                                          |   **34** |   **15** |                                                                                          |

The 2 new parse-error diagnostics come from `aws-ebs-csi-driver` and `wazuh` charts (one each per tip). Sample error:

```
parsing rendered YAML: decoding document in /Users/reuben/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/aws-ebs-csi-driver.yaml:
  yaml: unmarshal errors: line 71: mapping key "provisioner" already defined at line 63
```

Both are legitimate content-level issues with the rendered output (duplicate keys inside a single rendered doc) that were previously masked because `$values` failures preempted rendering. Could be a chart-template bug or a multi-doc-separator issue on fg-manifold's side; either way, xpc surfaces them correctly now. Not a regression.

### Validation vs prediction

- Predicted: `XPC.H.helm-renders 34 → ~13` (21 `$values` resolve, 12 `lib/charts` remain, 1 renovate remains).
- Actual: `34 → 15` (21 `$values` resolve ✅, 12 `lib/charts` remain ✅, 1 renovate remains ✅, +2 newly-surfaced parse errors ⚠️).

The +2 delta vs prediction is the "successful renders expose new signal" effect (same as v4's +61 on R16). Prediction held on the fix itself.

### Why R15 jumped 700 → 1,018

In v4, helm template failed on 22 sources (placeholder leaks) + 13 (lib/charts) + ... so the World only got rendered resources from the subset that rendered successfully. In v5, 21 of the 22 previously-leaking sources now produce rendered output, which means ~19 Helm charts' worth of new Kubernetes resources flow into `World.Resources` with `Provenance == "rendered:helm:<app>"`. R15 walks every resource and checks its kind against the app's AppProject whitelist; +318 diagnostics is the volume of newly-visible resources × the same per-resource filter that fires when a kind isn't whitelisted. No rule-logic change — just more data.

Readers sizing the R15 audit should treat these 318 as pre-existing coverage that was masked, not new regressions.

### Info diagnostics

41 info diagnostics per tip, all `XPC.H.appset-unsupported-generator` (unchanged from v4). Notably **zero** `XPC.H.values-ref-unknown` or `XPC.H.values-ref-remote` diagnostics — every `$<ref>/...` encountered resolved cleanly via the local `.git` walk-up on fg-manifold, validating the "first-cut local-only" assumption in the followup #16 planning doc.

---

## What this unblocks

1. **Followup #16 — closes.** 21/21 targeted failures resolved; zero info diagnostics from the `values-ref-*` fallback paths. Handoff doc can be archived.
2. **Render-cache perf on real data.** With 19 charts now rendering successfully, `~/.cache/xpc/renders/` populates on cold runs. Warm-run render-cache hit rate is now measurable — deferred here, but a follow-up replay could quantify the additional speedup on top of v3's 3× chart-pull caching.
3. **New-parse-error triage.** The 2 duplicate-key errors are content issues on fg-manifold's side (aws-ebs-csi-driver + wazuh chart templates or values). Flagging here for visibility; no xpc work required unless the parse error is a yaml.v3 behavior we can relax.
4. **Coverage re-baseline at ~80%+.** The 318 new R15 rows are pre-existing miss-coverage that was hidden until renders completed. Total diagnostic volume now 1,551/tip, still dominated by legitimate signal. Under the total-coverage scoreboard in the main ledger, R15 (~2% of MRs) just saw its effective surface multiply; no bucket re-weighting needed but worth noting as "R15's realized signal density went up."

---

## Reproducing

```bash
# Binary from mainline
cd /Users/reuben/projects/cross-validate
go build -o /tmp/xpc-v5 ./cmd/xpc     # at b6ab26b

# Per-tip cold
for tip in 441fb679a 2ca71f228 4dd584566; do
  git -C /Users/reuben/fg/fg-manifold checkout -q $tip
  rm -rf ~/.cache/xpc ~/.cache/xpc-v5-helm && mkdir -p ~/.cache/xpc-v5-helm
  { time /tmp/xpc-v5 check \
      --kernel-path=/Users/reuben/projects/cross-validate/kernel \
      --helm-cache-dir=$HOME/.cache/xpc-v5-helm \
      --appset-fixture=/tmp/fg-manifold-pr-fixture.yaml \
      --format=json \
      /Users/reuben/fg/fg-manifold/deploy/ \
      > /tmp/v5-cold-$tip.json 2> /tmp/v5-cold-$tip.stderr ; } 2> /tmp/v5-cold-$tip.time
done
git -C /Users/reuben/fg/fg-manifold checkout -q feat/e2e-otel-enable-autoload
```

Raw outputs at `/tmp/v5-cold-{441fb679a,2ca71f228,4dd584566}.{json,stderr,time}` (ephemeral; not committed).

---

## Accounting sanity

```bash
# Per-category decomposition of the 15 helm-renders (identical across tips)
jq 'map(select(.code == "XPC.H.helm-renders")) | length'                                            # 15
jq 'map(select(.code == "XPC.H.helm-renders") | select(.detail | test("\\$values"))) | length'            # 0
jq 'map(select(.code == "XPC.H.helm-renders") | select(.detail | test("lib/charts/crossplane"))) | length' # 12
jq 'map(select(.code == "XPC.H.helm-renders") | select(.detail | test("protocol handler"))) | length'     # 1
jq 'map(select(.code == "XPC.H.helm-renders") | select(.detail | test("parsing rendered YAML"))) | length' # 2
```
