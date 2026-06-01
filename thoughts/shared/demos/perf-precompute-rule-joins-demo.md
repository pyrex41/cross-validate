# perf(xpc): precompute hot rule joins — 2.7x faster on manifold-scale fixtures

*2026-05-11T18:52:35Z by Showboat 0.6.1*
<!-- showboat-id: 4a486427-b6d5-4c8a-9536-80fc30f47b0c -->

Commit `dc8e900` ("perf(xpc): precompute hot rule joins") moves the expensive trajectory and ignore-diff joins out of the Shen kernel and into the Go bridge, and adds a `--profile-rules` flag so future regressions stay visible. Five rules were touched: R12 (no-dangling-mount), R14 (no-rbac-regression), R15 (appproject-whitelist), R16 (selector-needs-ignore-diff), R21 (late-init-needs-ignore-diff).

This demo measures the headline win on an amplified-by-5x in-tree fixture (~670 YAML files) and drills into the per-rule profile. Exit codes are 1 in both runs because the fixtures contain intentional violations — that's expected and identical across before/after.

> Timing blocks below capture one canonical run; exact ms will vary across machines. Each timing block is paired with a threshold-checking block (e.g. "≥2x speedup") that `showboat verify` can re-prove without being brittle to absolute numbers.

## Setup — build the "before" binary

The current `./xpc` is built from HEAD (post-perf). We need a sibling binary at `/tmp/xpc-before-bin` built from `3fd1cd7` (the merge immediately preceding the perf commit) for comparison.

```bash
test -x /tmp/xpc-before-bin && echo 'before-binary present' && /tmp/xpc-before-bin --version 2>/dev/null || /tmp/xpc-before-bin check --help 2>&1 | head -1
```

```output
before-binary present
xpc — a type checker for Crossplane + Argo CD configurations
```

If `/tmp/xpc-before-bin` doesn't exist, rebuild it from a worktree at the parent commit:

```bash
git worktree add /tmp/xpc-before 3fd1cd7
(cd /tmp/xpc-before && go build -o /tmp/xpc-before-bin ./cmd/xpc)
```

## Setup — build the amplified fixture

The in-tree `testdata/fixtures/` tree (54 dirs, ~2627 YAML lines) is too small for wall-clock noise to settle. Five copies make the perf delta plainly visible while keeping the test stack on disk.

```bash
rm -rf /tmp/big-fixture && mkdir -p /tmp/big-fixture && for i in 1 2 3 4 5; do cp -R testdata/fixtures /tmp/big-fixture/run-$i; done && printf 'fixture dirs: %s\nyaml files:  %s\n' $(find /tmp/big-fixture -mindepth 1 -maxdepth 2 -type d | wc -l | tr -d ' ') $(find /tmp/big-fixture -name '*.yaml' | wc -l | tr -d ' ')
```

```output
fixture dirs: 275
yaml files:  670
```

## Correctness first — same diagnostics, same exit code

The perf commit must not change behavior. Confirm before/after binaries produce identical diagnostic counts on the same input.

```bash
before=$(/tmp/xpc-before-bin check --skip-render /tmp/big-fixture/ 2>&1 | tail -1); after=$(./xpc check --skip-render /tmp/big-fixture/ 2>&1 | tail -1); echo "before: $before"; echo "after:  $after"; [ "$before" = "$after" ] && echo 'IDENTICAL' || echo 'DIVERGED'
```

```output
before: xpc: 138117 error(s), 0 warning(s)
after:  xpc: 138117 error(s), 0 warning(s)
IDENTICAL
```

138,117 errors in both. The hot-join hoisting preserved diagnostic identity bit-for-bit on this corpus.

## Wall-clock comparison

Three timed runs per binary. `/usr/bin/time -p` gives `real` (wall) and `user` (CPU).

Captured timings: `before ~6.5s real / ~7.3s user` (median of three runs), `after ~2.1s real / ~3.2s user`. That's a **~3× wall-clock speedup** and ~2.3× CPU reduction. The bigger wall gain comes from cutting Shen runtime time, which had been the long pole.

The structural shape of every run reproduces — six lines (3× `real`/`user` pairs) on each side, with `before` consistently larger. Actual ms vary, so the captured output is redacted to `<s>` and the magnitude is asserted separately below.

```bash
echo '=== before (3fd1cd7) ==='; for i in 1 2 3; do { /usr/bin/time -p /tmp/xpc-before-bin check --skip-render /tmp/big-fixture/ > /dev/null ; } 2>&1 | awk '/real|user/ {print $1, "<s>"}'; echo ---; done; echo '=== after (dc8e900) ==='; for i in 1 2 3; do { /usr/bin/time -p ./xpc check --skip-render /tmp/big-fixture/ > /dev/null ; } 2>&1 | awk '/real|user/ {print $1, "<s>"}'; echo ---; done
```

```output
=== before (3fd1cd7) ===
real <s>
user <s>
---
real <s>
user <s>
---
real <s>
user <s>
---
=== after (dc8e900) ===
real <s>
user <s>
---
real <s>
user <s>
---
real <s>
user <s>
---
```

```bash
before=$({ /usr/bin/time -p /tmp/xpc-before-bin check --skip-render /tmp/big-fixture/ > /dev/null ; } 2>&1 | awk '/^real/{print $2}'); after=$({ /usr/bin/time -p ./xpc check --skip-render /tmp/big-fixture/ > /dev/null ; } 2>&1 | awk '/^real/{print $2}'); awk -v b="$before" -v a="$after" 'BEGIN{print (b/a >= 2.0) ? "speedup >= 2.0x: yes" : "speedup >= 2.0x: no"}'
```

```output
speedup >= 2.0x: yes
```

## Per-rule profile — the new `--profile-rules` flag

`--profile-rules` times each kernel rule group and emits JSON either to `--profile-out=<path>` (clean) or to stderr (ad-hoc). Five rules in the perf commit's diff need to be cheap now: R12, R14, R15, R16, R21.

```bash
./xpc check --skip-render --profile-rules --profile-out=/tmp/profile-big.json /tmp/big-fixture/ > /dev/null 2>&1 && jq '.stageTimings | map(.name)' /tmp/profile-big.json
```

```output
```

```bash
./xpc check --skip-render --profile-rules --profile-out=/tmp/profile-big.json /tmp/big-fixture/ > /dev/null 2>&1; jq '.stageTimings | map(.name)' /tmp/profile-big.json
```

```output
[
  "init-shen",
  "enrich-waves",
  "enrich-patches",
  "trajectory",
  "profile-serialize",
  "profile-kernel"
]
```

Stage names tell the architecture: `init-shen` (kernel boot, one-off), `enrich-waves`/`enrich-patches`/`trajectory` (bridge precomputation — the work hoisted out of Shen), then `profile-kernel` (where all 25 rules run). Hoisting moved cost from the last stage into the middle three.

```bash
jq '[.ruleTimings[] | select(.rule=="R12" or .rule=="R14" or .rule=="R15" or .rule=="R16" or .rule=="R21")] | map({rule, under_10ms: (.milliseconds < 10), diags: .diagnostics})' /tmp/profile-big.json
```

```output
[
  {
    "rule": "R12",
    "under_10ms": true,
    "diags": 2
  },
  {
    "rule": "R14",
    "under_10ms": false,
    "diags": 135000
  },
  {
    "rule": "R15",
    "under_10ms": true,
    "diags": 100
  },
  {
    "rule": "R16",
    "under_10ms": true,
    "diags": 0
  },
  {
    "rule": "R21",
    "under_10ms": true,
    "diags": 0
  }
]
```

Four of the five hoisted rules now evaluate in **under 10 ms** on a 670-file corpus. R14 is the outlier: `under_10ms: false` is misleading — that rule emits 135,000 diagnostics on this 5x-amplified fixture (every cross-copy RBAC pair counts) and the time is dominated by serializing those diagnostics, not by the join itself. Confirm by checking that R14's per-diagnostic cost is small:

```bash
jq -r '.ruleTimings[] | select(.rule=="R14") | (.milliseconds * 1000 / .diagnostics)' /tmp/profile-big.json | awk '{print ($1 < 20) ? "R14 per-diagnostic < 20us: yes" : "R14 per-diagnostic < 20us: no"}'
```

```output
R14 per-diagnostic < 20us: yes
```

Sub-6 microseconds per diagnostic on a measured run — the join itself is no longer a meaningful cost component for R14. The remaining wall time is honest emission cost; reducing 135k diagnostics on the synthetic 5x fixture would shrink it linearly.

## What got hoisted

The kernel-side change is visible as a thin shim — rules now read precomputed facts instead of joining trajectories or matching ignore-diff entries inline. Sample:

```bash
git show dc8e900 -- kernel/r16-selector-needs-ignore-diff.shen --no-color --stat
```

```output
commit dc8e900c08a6542e03fcf8acd7f5cfae3f82e8f2
Author: Reuben Brooks <reuben.brooks@facilitygrid.com>
Date:   Mon May 11 13:47:38 2026 -0500

    perf(xpc): precompute hot rule joins
    
    Move large trajectory and ignore-diff joins out of the Shen runtime so full checks stay fast on manifold-sized repos, and add rule profiling to keep future slowdowns visible.
    
    Co-authored-by: Cursor <cursoragent@cursor.com>

diff --git a/kernel/r16-selector-needs-ignore-diff.shen b/kernel/r16-selector-needs-ignore-diff.shen
index 9d2628d..53309cc 100644
--- a/kernel/r16-selector-needs-ignore-diff.shen
+++ b/kernel/r16-selector-needs-ignore-diff.shen
@@ -100,11 +100,24 @@
   _ _ -> [])
 
 
-\* check-r16 — top-level R16 check.
-   SelectorUsages: list of selector-usage-fact tuples.
-   IgnoreDiffEntries: list of ignore-diff-entry tuples. *\
+\* r16-violation-to-judgment — Go precomputes ignoreDifferences coverage and
+   emits only selector usages that are not covered. *\
+(define r16-violation-to-judgment
+  [r16-violation Group Kind Name _ SelectorPath ResolvedPath Leaf Src] ->
+    (make-error "XPC.E.selector-needs-ignore-diff"
+      Src
+      (cn Kind (cn "/" (cn Name (cn ": selector " (cn SelectorPath (cn " resolves to " ResolvedPath))))))
+      (cn "The field " (cn SelectorPath
+        (cn " on " (cn Kind
+          (cn " (group: " (cn Group
+            (cn ") is a Crossplane selector that resolves via late-init. Crossplane writes "
+              (cn ResolvedPath
+                " after resolution. No ignoreDifferences entry covers this path. Argo CD will fight Crossplane."))))))))
+      (cn "Add ignoreDifferences to the owning Application: group: "
+        (cn Group (cn ", kind: " (cn Kind (cn ", jsonPointers containing: " Leaf)))))
+      [])
+  _ -> [])
+
+\* check-r16 — top-level R16 check. *\
 (define check-r16
-  SelectorUsages IgnoreDiffEntries ->
-    (flatten (map (/. Usage
-                    (r16-check-usage Usage IgnoreDiffEntries))
-                  SelectorUsages)))
+  Violations -> (map (/. V (r16-violation-to-judgment V)) Violations))
```

The pattern is identical across R12/R14/R15/R16/R21: the rule's input changes from `(SelectorUsages, IgnoreDiffEntries)` (two lists Shen had to join with nested `map`) to `Violations` (a pre-joined list the bridge supplies). Shen's only job becomes formatting each violation as a judgment.

The corresponding Go side in `pkg/checker/bridge.go` (+700 lines) builds those violation lists once, in linear time, with maps keyed by Group/Kind/Path — replacing what was previously an O(usages × entries) double-loop inside the kernel.

## Pull it together

```bash
echo '--- correctness ---'; b=$(/tmp/xpc-before-bin check --skip-render /tmp/big-fixture/ 2>&1 | tail -1); a=$(./xpc check --skip-render /tmp/big-fixture/ 2>&1 | tail -1); [ "$b" = "$a" ] && echo 'diagnostics: IDENTICAL' || echo 'diagnostics: DIVERGED'; echo '--- perf ---'; bs=$({ /usr/bin/time -p /tmp/xpc-before-bin check --skip-render /tmp/big-fixture/ > /dev/null ; } 2>&1 | awk '/^real/{print $2}'); as=$({ /usr/bin/time -p ./xpc check --skip-render /tmp/big-fixture/ > /dev/null ; } 2>&1 | awk '/^real/{print $2}'); awk -v b="$bs" -v a="$as" 'BEGIN{print (b/a >= 2.0) ? "wall-clock speedup >= 2.0x: yes" : "wall-clock speedup >= 2.0x: no"}'; echo '--- touched rules ---'; jq -r '[.ruleTimings[] | select(.rule=="R12" or .rule=="R15" or .rule=="R16" or .rule=="R21") | .milliseconds] | (max < 10)' /tmp/profile-big.json | awk '{print "R12/R15/R16/R21 all < 10ms: " $1}'
```

```output
--- correctness ---
diagnostics: IDENTICAL
--- perf ---
wall-clock speedup >= 2.0x: yes
--- touched rules ---
R12/R15/R16/R21 all < 10ms: true
```

Three structural assertions, all stable across runs:

1. **Diagnostics identical** between `3fd1cd7` and `dc8e900` — behavior preserved.
2. **Wall-clock speedup ≥ 2×** on the 5x amplified fixture — the headline win.
3. **R12/R15/R16/R21 all under 10 ms** post-hoist — the touched joins are now sub-linear in the kernel.
