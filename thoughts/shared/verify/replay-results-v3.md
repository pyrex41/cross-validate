---
title: fg-manifold replay v3 — after XPC006 OwningApp + remote Helm chart fixes
date: 2026-04-21
author: Reuben / Claude (Track T1)
binary: /tmp/xpc-v3 built from claude/t1-replay-v3 @ 4e1a77e (tree of claude/build-xpc-type-checker-TfgsT)
predecessor: replay-results-v2.md (@ claude/build-xpc-type-checker-TfgsT a7ead5c)
---

## TL;DR

**Two landed fixes validated, one partially.** Replay-v3 re-runs `xpc check` against the same three fg-manifold tips used in v2 with two post-v2 landings in scope:

1. **XPC006 OwningApp propagation** (commit `3530f0c`) — predicted <<100; actual **30** per tip, a **66× reduction** from v2's 1,980. Cartesian is gone.
2. **Remote Helm chart pull via `--helm-cache-dir`** (commit `fa027fb`) — predicted 0 or near-0; actual **34** per tip, **unchanged**. The remote-pull path is now working (19 remote charts successfully fetched into `~/.cache/xpc-v3-helm/charts/`, 13 MB on disk) but `helm template` still fails downstream for every chart. Pull path fixed; template path is the next gate.

**Warm-cache perf — first time measurable.** Because the chart-pull path no longer errors out before reaching the cache, we can finally time warm vs cold on this workload. Result: **~3× speedup** (cold ≈36–40s, warm ≈12–14s across all three tips). Helm-chart tarballs persist in `~/.cache/xpc-v3-helm/charts/` between runs; warm runs skip the remote pull entirely. The render cache at `~/.cache/xpc/renders/` is still empty because all 34 renders fail — no `Put` calls happen — so the measured speedup comes from chart-pull caching, not render caching.

**Determinism preserved.** All six runs (3 tips × 2 phases) produce bit-for-bit identical diagnostic sets within each tip. This is better than v2, which had a ~12-diag drift on two of three tips between cold and warm; v3 is dead stable.

---

## Run matrix (6 runs)

- fixture: `/tmp/fg-manifold-pr-fixture.yaml` (4 AppSets × 1–2 stub PRs each, same file as v2)
- binary: `/tmp/xpc-v3` from `claude/t1-replay-v3 @ 4e1a77e49c4be14ea28fe5d329a864199bfbc09b`
- invocation: `/tmp/xpc-v3 check --kernel-path=.../kernel --helm-cache-dir=~/.cache/xpc-v3-helm --appset-fixture=/tmp/fg-manifold-pr-fixture.yaml --format=json /Users/reuben/fg/fg-manifold/deploy/`
- cold protocol: `rm -rf ~/.cache/xpc ~/.cache/xpc-v3-helm && mkdir -p ~/.cache/xpc-v3-helm` before each cold run.
- warm protocol: no cache clear; rerun immediately.

| tip       | run  | wall-time | user+sys | exit | total diags |
|-----------|------|-----------|----------|------|-------------|
| 441fb679a | cold |  39.54s   | 18.32s   | 1    | 1,191       |
| 441fb679a | warm |  12.36s   |  9.12s   | 1    | 1,191       |
| 2ca71f228 | cold |  35.52s   | 18.71s   | 1    | 1,191       |
| 2ca71f228 | warm |  13.75s   |  9.90s   | 1    | 1,191       |
| 4dd584566 | cold |  36.87s   | 19.00s   | 1    | 1,191       |
| 4dd584566 | warm |  12.64s   |  9.77s   | 1    | 1,191       |

Determinism check: `jq -S sort_by(…)` of cold vs warm diffs is empty for every tip. Across all 6 runs the total, the per-code breakdown, and the individual diagnostic messages are identical. Better than v2.

---

## Rule breakdown (per tip, stable across all 6 runs)

| code                                       | v1 count |  v2 count |  v3 count |  v3 delta vs v2 | note                                                          |
|--------------------------------------------|---------:|----------:|----------:|----------------:|---------------------------------------------------------------|
| `XPC.D.kind-whitelisted` (R15)             |   64,484 |       700 |       700 |              0  | stable — per-resource, per-owning-app already in v2.          |
| `XPC006` (sync-wave ordering)              |    1,980 |     1,980 |    **30** |      **−1,950** | **fixed — OwningApp propagation (3530f0c) killed cartesian.**  |
| `XPC.E.selector-needs-ignore-diff` (R16)   |      374 |       374 |       374 |              0  | stable; all unique.                                           |
| `XPC.H.appset-unsupported-generator`       |       41 |        41 |        41 |              0  | open — Go-template appset-expander gap (followup #6).          |
| `XPC.H.helm-renders` (R18)                 |       34 |        34 |        34 |              0  | partial — pull fixed; template still fails (see below).        |
| `XPC.E.late-init-needs-ignore-diff` (R21)  |        — |        12 |        12 |              0  | stable; tip-invariant.                                        |
| **total**                                  | 66,913   |   **3,141** | **1,191** |     **−1,950**  | **−62% of v2's remaining noise; signal density now very high.**|

### Validation rows (predictions vs actual)

- **XPC006**: v2 = 1,980 → v3 = **30** (predicted `<<100`). **PREDICTION HELD.** The 30 remaining are genuine wave-ordering constraints on `function-go-templating`, `function-auto-ready`, `function-status-transformer`, `function-patch-and-transform` vs the various Compositions under `crossplane-platform` across a handful of app/region combinations. Every diagnostic is now sensibly scoped to a real owning Application; no more cross-app Cartesian explosion. The `OwningApp` field threaded into `XRDInfo`, `CompositionInfo`, `FunctionInfo`, `ProviderInfo` by `3530f0c` does exactly what the v2 postmortem predicted.
- **XPC.H.helm-renders**: v2 = 34 → v3 = **34** (predicted `0 or near-0`). **PREDICTION NOT HELD**, but the diagnosis refines: `fa027fb` makes the remote-chart pull work (`~/.cache/xpc-v3-helm/charts/` populates with 19 chart hashes totalling 13 MB — see Warm-Cache section). However `helm template` on the pulled charts still fails with the generic message `"<release>: helm template failed"`. Probable causes (not yet root-caused in this replay): missing chart dependencies not pulled by the shallow pull, required values files that aren't resolvable, or CRDs the charts expect to be pre-applied. This is a new followup (#4b), distinct from the original `Path required` blocker which is now fixed.

---

## Warm-cache section (first-time measurable on fg-manifold)

v2 could not measure warm-cache perf: every Helm source errored at `ResolveChart` ("Path required") before reaching any cache path. v3 fixes the pull path, so warm caching is now observable.

| metric                                   | v2            | v3 cold             | v3 warm         | delta                 |
|------------------------------------------|---------------|---------------------|-----------------|-----------------------|
| wall-time (median of 3 tips)             | 1:03 / 1:05   | 36.87s              | **12.64s**      | **~3× warm speedup**  |
| `~/.cache/xpc-v3-helm/charts/` entries   | N/A           | 19 dirs (13 MB)     | 19 dirs (13 MB) | no new pulls on warm  |
| `~/.cache/xpc/renders/` entries          | 0             | 0                   | 0               | unchanged — see note  |
| warm cache-hit signal (stderr)           | silent        | silent              | silent          | no progress noise     |

**Chart-pull cache works.** After the first cold run, all 19 remote charts (crossplane, external-secrets, argocd-image-updater, cert-manager, aws-load-balancer-controller, istio, karpenter, kyverno, wazuh, etc.) are present on disk and reused across tips and across warm runs. The ~25s difference between cold and warm wall-time is entirely attributable to skipping the remote `helm pull` network round-trips; nothing else about the run changes.

**Render cache does not populate.** `~/.cache/xpc/renders/` exists (the `3d4476f` silent-failure fix confirmed, as in v2) but stays empty because `helm template` fails on all 34 sources, so the renderer never calls `Put`. This is a second-order consequence of the still-open `helm-renders` template failure — once that's addressed, render caching will be on tap for a further speedup on top of chart-pull caching.

**Warm cache-hit rate** is not explicitly reported by the JSON format or stderr (both are silent). The signal we have is:
1. 19 chart tarballs on disk after cold, same 19 after warm (no new pulls).
2. Wall-time drops from ~37s to ~13s — ~24s saved, which is approximately the amount of time 19 remote pulls take.
3. User+sys time drops from ~19s to ~10s — Helm subprocess invocations are still happening (template attempts), they just don't network.

Effective chart-pull cache hit rate on warm: 19/19 = **100%**.

---

## Determinism check

| tip       | cold totals | warm totals | diff |
|-----------|-------------|-------------|------|
| 441fb679a | 1,191       | 1,191       | 0    |
| 2ca71f228 | 1,191       | 1,191       | 0    |
| 4dd584566 | 1,191       | 1,191       | 0    |

Stronger than v2 (which had a drift of ~12 diagnostics cold→warm on tips 1 and 2, attributed to `renderer.DoubleRenderHelm` seed-order sensitivity). Plausibly v3 is more deterministic precisely because no renders are actually succeeding — the non-determinism surface is only exposed when renders complete. If/when followup #4b (helm template failures) lands, re-check this.

Full `jq -S sort_by(.code,.message,.source.file,.source.line)` diff between cold and warm is empty for all three tips.

---

## What this unblocks

1. **R18 template-failure triage (new followup, call it #4b).** With `Path required` gone, the remaining 34 helm-renders errors surface the *next* layer of remote-chart brittleness — probably missing chart dependencies (`helm dependency update` not run), missing values files, or pre-apply CRDs. Reasonable next step: capture the stderr of `helm template` invocations (currently swallowed into the generic `"helm template failed"` message) so the diagnostic carries the real helm error. That is an xpc-side observability fix before it becomes an fg-manifold data-quality fix.

   **UPDATE 2026-04-22** — xpc-side fix landed (pkg/renderer: `subprocessErrTail` helper wired into helm.go + kustomize.go; regression-tested by `TestRenderChart_PropagatesHelmStderr` and extended R18 check). Re-running the same xpc command on fg-manifold HEAD (44698ba) surfaces the real helm stderr in the `XPC.H.helm-renders` diagnostic's `detail` field — 0 empties across all 35 diagnostics. The 35 split into two distinct root causes now visible from the diagnostic alone:
   - **22 / 35** — `open <chart>/$values/.../{{provider}}/{{region}}/{{cluster}}/values.yaml: no such file or directory`. Argo's ApplicationSet templating left `{{provider}}/{{region}}/{{cluster}}` placeholders unresolved in the helm `$values` ref. xpc's AppSet expander is producing Applications whose Helm source still carries unresolved Argo template vars — a real expansion gap, not a chart issue. Open as followup #14.
   - **13 / 35** — `path "/…/deploy/facilitygrid/ops/applicationsets/lib/charts/crossplane-{claim,fargateservice,workers}" not found`. xpc is passing a path that doesn't exist on disk to `helm template` — the `crossplane-*` local-chart sources referenced by fg-manifold's ApplicationSets aren't in `lib/charts/` at this tip. Open as followup #15 (could be an fg-manifold repo-state artifact or an xpc path-resolution bug; quick `ls` on the repo will decide).
   Both are now actionable without having to shell out to helm manually — which was the observability invariant we wanted.
2. **Render-cache perf measurement.** Blocked until #4b lands; once `helm template` succeeds on at least some of the 34 sources, `~/.cache/xpc/renders/` will populate and we'll finally see warm-render-cache hit rate on real data.
3. **~80% coverage defensibility.** Total diagnostic count is now 1,191/tip — 95% of the noise that made v1's 66,913-diag stream unreadable is gone, and the remaining counts are dominated by legitimate signal (R15 app-project whitelist gaps, R16 selector drift, R21 late-init, R18 template failures, XPC006 wave-ordering on a small set of Functions/Compositions). The "useful on real repos" bar from the base plan is met on this workload.
4. **T2 SSA × managementPolicies landing.** The 30 XPC006 diags confirm the OwningApp threading infrastructure works end-to-end; T2's `extractSSAMPConflicts` can rely on `ResourceInfo.OwningApp` being populated correctly.
5. **Handoff ledger.** Followup items #3 (render cache), #5 (kernel-path flag), and (partial) #4 (remote helm pull) can be ticked; #6 (appset Go-template expander) and the new #4b (helm template failures) remain open; XPC006 cartesian (formerly #2) is now done.

---

## Reproducing

```bash
# Worktree + binary
cd /Users/reuben/projects/cross-validate/.claude/worktrees/t1-replay-v3
go build -o /tmp/xpc-v3 ./cmd/xpc          # SHA 4e1a77e49c4b...

# Per-tip cold+warm
for tip in 441fb679a 2ca71f228 4dd584566; do
  git -C /Users/reuben/fg/fg-manifold checkout $tip
  rm -rf ~/.cache/xpc ~/.cache/xpc-v3-helm && mkdir -p ~/.cache/xpc-v3-helm
  time /tmp/xpc-v3 check \
    --kernel-path=/Users/reuben/projects/cross-validate/.claude/worktrees/t1-replay-v3/kernel \
    --helm-cache-dir=$HOME/.cache/xpc-v3-helm \
    --appset-fixture=/tmp/fg-manifold-pr-fixture.yaml \
    --format=json \
    /Users/reuben/fg/fg-manifold/deploy/ > /tmp/v3-cold-$tip.json 2> /tmp/v3-cold-$tip.stderr
  time /tmp/xpc-v3 check [same args] > /tmp/v3-warm-$tip.json 2> /tmp/v3-warm-$tip.stderr
done
git -C /Users/reuben/fg/fg-manifold checkout feat/e2e-otel-enable-autoload
```

Raw outputs at `/tmp/v3-{cold,warm}-{441fb679a,2ca71f228,4dd584566}.{json,stderr,time}`.

---

## Aggregation recipe

```bash
for tip in 441fb679a 2ca71f228 4dd584566; do
  for phase in cold warm; do
    f=/tmp/v3-$phase-$tip.json
    echo "=== $phase $tip (total=$(jq length $f)) ==="
    jq -r 'group_by(.code) | map({code: .[0].code, count: length})
           | sort_by(-.count) | .[] | "\(.count)\t\(.code)"' $f
  done
done
```
