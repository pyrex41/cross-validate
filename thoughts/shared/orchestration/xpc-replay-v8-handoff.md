---
date: 2026-04-24
mainline: claude/build-xpc-type-checker-TfgsT @ 27916ac
preceding handoffs:
  - thoughts/shared/orchestration/xpc-values-multisource-handoff.md
preceding replays:
  - thoughts/shared/verify/replay-results-v7.md
status: OPEN — single-track replay pickup (no new code)
---

# xpc replay v8 handoff — validate P5 fixes on fg-manifold

## TL;DR

P5 (trajectory-rule hardening) landed on `claude/build-xpc-type-checker-TfgsT`
as three commits (`98ddbcc` P5.b tail-steps removal, `747c791` P5.a R12-cross
dedup, `27916ac` P5.c plan-runner kernel-path fallback). Smoke tests confirmed
the R12 dedup reduces tip-main emissions from **3504 → 12** on the raw
`/tmp/xpc-p5/xpc` verification binary. Replay v8 formalizes that on the full
5-tip matrix used by v6 and v7, and separately validates that `xpc plan` from
outside the repo no longer needs `--kernel-path`.

Single track, mechanical. Estimated ½ session.

## Mainline state at handoff

`claude/build-xpc-type-checker-TfgsT` @ `27916ac`, working tree clean,
`go test ./...` green. 121 commits ahead of origin — unpushed, pending
PR construction.

Commits layered on top of v7:

```
27916ac plan: fall back to kernel discovery from xpc binary location (P5.c)
747c791 kernel: dedup R12-cross emissions per (owner, target) (P5.a)
98ddbcc kernel: remove unused tail-steps helper (P5.b)
f81ccf3 docs: replay-v7 — P4 landing reconciled against fg-manifold  ← v7 baseline
```

## Pre-stated predictions

Derived from the v7 → P5 delta. Scoped narrow because only two shipping surfaces changed.

1. **R12 (XPC012) per-tip counts** — the v7 headline 3504/3654/3854 across
   tips should collapse to the dedup-floor of distinct (owner, target) tuples.
   v7 Section `### R12` enumerates 12 distinct tuples on tip-main.
   - tip-441 / tip-2ca / tip-4dd / tip-main: expect **~12** (exact number may
     shift slightly per-tip if fg-manifold's argocd wave layout differs across
     tips, but the order of magnitude is the call: <50 on every tip).
   - tip-postrem: same order. Record exactly.

2. **Every other rule code** — identical to v7. P5 touched no rule outside R12
   and did not change any fact-extraction logic. If any other code shifts,
   investigate before accepting.

3. **Plan-mode without `--kernel-path`** — the 3 plan-mode pairs
   (`441→2ca`, `2ca→4dd`, `main→postrem`) should now run from inside
   `~/fg/fg-manifold` with no `--kernel-path` flag. v7 required the explicit
   flag and flagged it as ergonomics-blocker. Expect:
   - No XPC000 / "could not locate kernel directory" errors.
   - R26 + R27 diagnostic counts identical to v7 (plan-mode logic is
     unchanged; only the kernel-discovery fell back correctly).

4. **No new rules emit**. R13 stays 0 (retired). R27 single-tip stays 0
   (plan-mode only).

## Runbook

### Step 1: Rebuild binary

```bash
cd /Users/reuben/projects/cross-validate
go build -o /tmp/xpc-replay/xpc ./cmd/xpc
ls -la /tmp/xpc-replay/xpc   # confirm fresh timestamp
/tmp/xpc-replay/xpc version  # sanity
```

### Step 2: Re-run 5 single-tip checks

The tip worktrees at `/tmp/xpc-replay/2026-04-23-phase1/tip-*` should still
exist from v6/v7. If not, reprovision via the reproducer block at the bottom
of `thoughts/shared/verify/replay-results-v6.md`.

```bash
for tip in 441 2ca 4dd main postrem; do
  /tmp/xpc-replay/xpc check --format=json \
    /tmp/xpc-replay/2026-04-23-phase1/tip-$tip/deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/tip-$tip-v8.json \
  2> /tmp/xpc-replay/2026-04-23-phase1/tip-$tip-v8.stderr
done
```

Per-code counts — reuse v7's Python extractor.

### Step 3: Re-run 3 plan-mode pairs — **without `--kernel-path`**

```bash
cd ~/fg/fg-manifold
/tmp/xpc-replay/xpc plan --base=441fb679a --head=2ca71f228 --format=json ./deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/plan-441-2ca-v8.json \
  2> /tmp/xpc-replay/2026-04-23-phase1/plan-441-2ca-v8.stderr
/tmp/xpc-replay/xpc plan --base=2ca71f228 --head=4dd584566 --format=json ./deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/plan-2ca-4dd-v8.json \
  2> /tmp/xpc-replay/2026-04-23-phase1/plan-2ca-4dd-v8.stderr
/tmp/xpc-replay/xpc plan --base=44698ba64 --head=a5f77a3b8 --format=json ./deploy/facilitygrid/ops \
  > /tmp/xpc-replay/2026-04-23-phase1/plan-main-postrem-v8.json \
  2> /tmp/xpc-replay/2026-04-23-phase1/plan-main-postrem-v8.stderr
```

Critical: **no `--kernel-path` flag**. If any command errors out with XPC000 /
kernel discovery, P5.c's fix is incomplete and the replay is a no-go until
it's addressed. Report via stderr snippet, don't mask by falling back to
explicit `--kernel-path`.

### Step 4: Reconcile

Diff every code's count per-tip against v7:

- R12 should collapse. Everything else should match.
- Capture the 12 distinct (owner, target) tuples from tip-main post-dedup
  and verify they match v7's enumeration. If the set differs, something
  about the dedup key is wrong.
- Plan-mode stderr must be clean on all 3 pairs.

### Step 5: Write `thoughts/shared/verify/replay-results-v8.md`

Shape — much shorter than v7 because the matrix is narrow:

```markdown
---
title: fg-manifold replay v8 — after P5 (R12-cross dedup + kernel-path fallback)
date: 2026-04-24
author: <your handle> / Claude
binary: /tmp/xpc-replay/xpc built from claude/build-xpc-type-checker-TfgsT @ 27916ac
predecessor: replay-results-v7.md
---

## TL;DR

<2–3 sentences — headline the R12 collapse, confirm plan-mode kernel-path
fallback works>

## Run matrix

<5 single-tip + 3 plan-pair counts, with v7 deltas>

## Prediction scorecard

<4 predictions, match/miss>

## R12 dedup enumeration

<list the distinct (owner, target) tuples that survived, compare against
v7's 12>

## What this unblocks

<2–3 bullets: R12 is now production-ready for PR gating; P5.d becomes
optional rather than required; replay v9 triggers only on material
behavior changes>
```

Commit on the mainline branch:

```bash
cd /Users/reuben/projects/cross-validate
git add thoughts/shared/verify/replay-results-v8.md
git commit -m "docs: replay-v8 — R12 dedup + kernel-path fallback validated

Confirms P5.a's 3504 → ~12 collapse on fg-manifold real data and P5.c's
plan-mode kernel-discovery working from outside the repo.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## If something goes wrong

- **R12 count doesn't collapse.** Dedup key is wrong or the test harness didn't exercise it. Read `pkg/checker/kernel_path_test.go` and `testdata/fixtures/dangling-mount-dedup/` to see the test's expected shape. Compare against a real fg-manifold diagnostic to find the delta.
- **Plan-mode errors with kernel-not-found.** Read `pkg/checker/bridge.go` — look for the `resolveKernelPath` function. The test `TestResolveKernelPath_ExecutableFallback` pins the contract. If tests pass but the real binary fails, something about `os.Executable()` is returning a path whose parent doesn't contain `kernel/` — dig into the binary's runtime path.
- **Other counts shift.** STOP. Read the diagnostics. The only shipping change outside R12 was kernel-path resolution, which shouldn't affect rule counts. If something shifted, we have an unexpected side effect.

## Known out-of-scope

- **P5.d** (externally-managed secret filter). If v8 shows the 12-tuple residue matches v7's enumeration, P5.d becomes optional — the 12 are actionable signal, not noise. If it doesn't match, P5.d jumps to the top of the list.
- **P5.e** (RBAC fact-schema Role-namespace). Orthogonal to v8; tracked separately.
- **Composition rendering validation.** Requires a machine with the `crossplane` CLI. v8 won't cover this. Flag it in the "what this doesn't validate" section if you add one.
