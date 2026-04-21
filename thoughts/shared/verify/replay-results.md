---
session: post-S5 replay (Track 1)
rule: validate 62% primary-coverage claim against fg-manifold `main`
branch: claude/phase1-cleanup
tip: 3d4476f
repo: /Users/reuben/fg/fg-manifold
date: 2026-04-21
---

# fg-manifold Replay — 3 Known-Good Tips, Cold + Warm

**Summary:** Coverage claim **partially validated**. xpc runs cleanly on real fg-manifold manifests (no crashes, exit=1 expected), within the cold perf budget (<2 min). But two pre-existing rules (R15, XPC006) emit cartesian-shaped false-positive floods that dominate the output (64k + 2k errors from 288 + 30 unique messages), and the render cache is not populating, so warm runs are not faster than cold. The selector rule (R16) looks healthy — 374 errors, all unique. Helm render failures (34) point at a real coverage gap in remote-chart handling.

---

## Run matrix (6 runs)

fixture: `/tmp/fg-manifold-pr-fixture.yaml` (4 AppSets × 1–2 stub PRs each)
binary: `/tmp/xpc` built from `claude/phase1-cleanup @ 3d4476f`
invocation: `/tmp/xpc check --format=json --appset-fixture=… /Users/reuben/fg/fg-manifold/deploy/`
cwd: `/Users/reuben/projects/cross-validate` (see followup #5 — kernel lookup requires this)

| tip | run | wall-time | exit | total diags |
|---|---|---|---|---|
| 441fb679a | cold | 1:34.12 | 1 | 66,913 |
| 441fb679a | warm | 1:27.89 | 1 | 66,913 |
| 2ca71f228 | cold | 1:22.58 | 1 | 66,913 |
| 2ca71f228 | warm | 1:23.95 | 1 | 66,913 |
| 4dd584566 | cold | 1:14.74 | 1 | 66,913 |
| 4dd584566 | warm | 1:16.54 | 1 | 66,913 |

All three tips produce **byte-identical diagnostic counts per code**, which is a nice determinism signal for xpc itself but also confirms these tips don't differ in anything these rules look at.

## Diagnostic breakdown (identical across all 6 runs)

| code | total | unique | multiplier | read |
|---|---|---|---|---|
| `XPC.D.kind-whitelisted` (R15) | 64,484 | 288 | ~224× | **cartesian FP flood** — same (kind, group) pair blamed once per Application rather than once per offending resource |
| `XPC006` (sync-wave order, pre-plan) | 1,980 | 30 | ~66× | similar multiplicative blowup, different rule |
| `XPC.E.selector-needs-ignore-diff` (R16) | 374 | 374 | 1× | looks legit — each unique |
| `XPC.H.appset-unsupported-generator` | 41 | 1 | 41× | all 41 emissions for the `preview-environments` AppSet, same `template uses non-trivial Go-template syntax` message |
| `XPC.H.helm-renders` (R18) | 34 | 34 | 1× | real gap — remote-chart `Path required` failures (see followup #4) |

R16 (S2, 20% of the target-study coverage budget) is the only rule in this tip that *looks* production-ready on this repo: zero duplicate messages, plausible per-resource emit.

## Perf

- Cold budget (<2 min): **met** on all 3 tips (74–94s).
- Warm budget (<20s): **badly missed** — warm runs are within 8 seconds of cold.
- Root cause: `~/.cache/xpc/` does not exist after 6 runs. Neither `renders/` nor `schemas/` populated. The renderer is either short-circuiting disk write or resolving the path wrong. Cache is documented at `pkg/renderer/cache.go:55` (`~/.cache/xpc/renders/`) and in `pkg/schemas/fetcher.go:21` (`~/.cache/xpc/schemas/`).

## Surprises

1. **R15 cartesian blowup.** Every (kind, group) pair is emitted once per Application in the world rather than once per offending resource. This is the single biggest signal-to-noise issue in the replay. Coverage number for R15 (2% on paper) is technically "met" but the UX is unusable: you get 64k errors to wade through to find the 288 real ones.
2. **XPC006 has the same shape.** 30 unique × ~66 multiplier = 1,980 emissions. The taxonomy counted this rule as "pre-plan" so it wasn't in the 62% scoreboard, but the same defect class (cartesian attribution) applies.
3. **Render cache never populates.** 6 runs, `~/.cache/xpc/` still absent. Violates S4's "hit rate >95% on second run" success criterion (`thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md:336`).
4. **No crashes, no panics, no kernel errors.** xpc is stable on real fg-manifold manifests once kernel lookup is satisfied — a real validation of the "no regressions" S5 claim.
5. **Diagnostic counts are tip-invariant on recent main.** Three separate merge commits on `main` produce identical error counts. Either these tips genuinely don't touch anything the rules look at, or these rules are too coarse to distinguish day-to-day changes. Probably the former — pick more divergent tips if the goal is coverage-sensitivity testing.
6. **R16 is quietly healthy.** 374 unique errors, zero duplication. Suggests the S2 vertical slice generalizes. This is the best signal we have that R21 (Track 2) will land cleanly.

## Followup queue (ordered by severity)

### P0

1. **R15 n×m cartesian FP flood** — `kernel/r15-*.shen` + the extraction path in `pkg/ir/`. R15 should emit one diag per `(resource, owning-application)` pair, not per `(resource, every-application-in-the-world)` pair. Likely fix: attribute resources to their owning Application in the extractor before the kernel joins against the whitelist.
2. **XPC006 same-shape blowup** — check if the sync-wave rule joins against the Application list the same way. If so, the fix is structural, not rule-specific.

### P1

3. **Render cache not populating** — `pkg/renderer/cache.go` disk-tier write isn't landing at `~/.cache/xpc/renders/`. Could be `os.UserCacheDir()` failing silently, a missing `MkdirAll`, or the cache being disabled in some code path that S4 didn't cover. Warm-run perf goal depends on this.
4. **Remote Helm chart `Path required`** — `pkg/renderer/` rejects `{chart: argocd-image-updater, repoURL: https://…, path: ""}` with `unsupported source kind: remote Helm chart "argocd-image-updater" not supported (Path required)`. Either the `Chart` field should count as the source, or this is an intentional limitation that should be a soft info-level skip instead of a hard error.

### P2

5. **xpc `--kernel-path` flag** — `resolveKernelPath` in `pkg/checker/bridge.go` walks cwd upward looking for `kernel/check.shen`. In fg-manifold (or any non-xpc-repo tree) there's no such ancestor, so xpc exits with XPC000. Real CI use requires `xpc check --kernel-path=$XPC_INSTALL/kernel …` or an env var fallback. Workaround we used: run with cwd=cross-validate, pass the target as an arg.
6. **`preview-environments` AppSet template uses Go-template syntax** — 41 emissions of `XPC.H.appset-unsupported-generator` all for this one AppSet. `pkg/ir/appset_expand.go:16` already documents this as an accepted gap; if we want the 62% scoreboard to reflect preview-fleet coverage, we need either an expander upgrade (handle `{{if}}`/`{{range}}`) or a higher-fidelity fixture that provides pre-resolved parameter sets.

### P3

7. **R21 registry cross-check** — R15's 288 unique (kind, group) pairs will overlap with the Crossplane managed-resource kinds R21 cares about. When seeding the late-init registry (Track 2), sanity-check each row's `(Group, Kind)` appears in the replay's R15 error list. Missing kinds = registry gap.
8. **Pick more divergent tips for next replay** — three adjacent merge commits on `main` produced identical diagnostic counts. For future coverage-sensitivity work, sample tips across a wider history window (e.g., one per month).

## Appendix

### Raw output locations

- Per-run JSON + stderr + time: `/tmp/xpc-replay/<sha>-{cold,warm}.{json,stderr,time}` — retained on the local box for the session.
- Commands used: documented inline in the diagnostic-count column above.

### Reproduction

```bash
cd /Users/reuben/projects/cross-validate
/tmp/xpc check \
  --format=json \
  --appset-fixture=/tmp/fg-manifold-pr-fixture.yaml \
  /Users/reuben/fg/fg-manifold/deploy/ | jq '[.[] | .code] | group_by(.) | map({code: .[0], n: length}) | sort_by(-.n)'
```

### Fixture shape (verified against `pkg/ir/appset_expand.go:32–36`)

Top-level keys = `ApplicationSet.metadata.name`. Values = list of parameter maps. See `/tmp/fg-manifold-pr-fixture.yaml` for the stub used in this replay.
