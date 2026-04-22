---
date: 2026-04-22
mainline: claude/build-xpc-type-checker-TfgsT @ 1373987
preceding handoff: thoughts/shared/orchestration/xpc-fg-manifold-handoff.md
---

# Post-parallel-wave handoff

The 2026-04-22 four-track parallel wave (T1 replay-v3, T2 SSA rule, T3 array-path lift, T4 CI docs) landed three of four tracks on mainline. T2 is preserved uncommitted-in-spirit on a feature branch; T1 also surfaced one new followup. This doc tells the next agent exactly where to pick up.

## Mainline state at handoff

`claude/build-xpc-type-checker-TfgsT` @ `1373987`, `go test ./...` green.

Commits added by the parallel wave:

```
1373987 docs: tick T3/T4/replay-v3 + log R22 attempt on WIP branch
a3e52fc verify: replay-v3 report against 3 fg-manifold tips
ba7fdce checker: add R16 array-path fixture + test for selector wildcard drift
85bf746 ir: wire WalkPath into selector + late-init extraction
2df298f ir: add WalkPath utility for array-aware JSON path traversal
80c1b2f docs: CI integration guide + GitLab SAST template
```

## Two pickup tracks — ordered by ROI

### A. Finish R22 (SSA × managementPolicies rule) — HIGHEST-ROI

**Why first**: ~2–3% MR coverage, last deferred rule in the base plan's scoreboard, and Go-side work is already done. Only the Shen rule needs rewriting.

**Preserved state**: branch `claude/t2-ssa-mp-rule` @ `f00c7c6` (a single WIP commit on top of mainline). Worktree already laid out at `.claude/worktrees/t2-ssa-mp-rule`. The `Prompt is too long` overflow happened on Opus — use Sonnet or Haiku for the pickup; this task is small enough to fit.

**What's ready to reuse (all Go-side)**:

| File | Status |
|---|---|
| `pkg/types/types.go` | `SSAMPConflict` struct + `World.SSAMPConflicts` + `World.SSAMPMode` added |
| `pkg/ir/builder.go` | `Builder.SSAMPMode` field wired |
| `pkg/ir/trajectory_extract.go` | `extractSSAMPConflicts` — joins SSA-enabled apps to their MRs and emits one `SSAMPConflict` per combo. Already called from the IR pipeline. |
| `pkg/checker/bridge.go` | `ssaMPConflictCmp` / `ssaMPConflictToObj` / `sortedSection("ssa-mp-conflicts", ...)` + `[ssa-mp-mode <sym>]` single-element section emitted |
| `cmd/xpc/main.go` | `--ssa-mp-mode={observe,partial,any}` flag default `observe`, plumbed to `Builder.SSAMPMode` |
| `testdata/fixtures/ssa-mp-{observe,partial,ok}/` | All three fixture trees present (app.yaml + mr.yaml) |
| `pkg/checker/check_test.go` | `TestR22_SSAMPObserve`, `TestR22_SSAMPPartial_DefaultSuppressed`, `TestR22_SSAMPSafe` — the test bodies look right; they panic today because the Shen rule crashes the kernel. |

**What's broken**: `kernel/r22-ssa-managementpolicies-safety.shen`. The Shen runtime panics `can't apply object` at `shen-go/kl/eval.go:234` (that's `apply` trying to invoke a non-function). The file loads a bunch of helpers (`r22-member?`, `r22-all-observe?`, `r22-has-write-op?`, `r22-has-update?`, `r22-is-default?`, `r22-mode-at-least-partial?`, `r22-mode-at-least-any?`), three `r22-emit-*` wrappers, `r22-check-row`, `check-r22`, `r22-filter-by-code`, `mark-r22-rules`. The simplification the prior agent landed at `check-row` only emits the `-observe` variant — partial/nondefault paths were still stubbed at context-exhaust time.

**Pickup recipe**:

1. `cd .claude/worktrees/t2-ssa-mp-rule`
2. Sanity: `go build ./... && go test ./pkg/ir/... ./pkg/types/...` — the Go side should compile and its unit tests should pass. If those fail, the handoff's Go-side claim is wrong; read the failure before touching Shen.
3. Diff vs. the known-working R21 rule side-by-side: `diff -u kernel/r21-late-init-needs-ignore-diff.shen kernel/r22-ssa-managementpolicies-safety.shen`. R21 is the closest structural template — same shape (per-row check → emit judgment → mark-rule → append to world). The divergence points are the emission count (R21 emits 0 or 1 per row; R22 emits 0, 1, or 2) and the mode-gating predicates.
4. The `can't apply object` crash almost certainly comes from one of:
   - **`flatten` or `map` not being available in shen-go** — R21 doesn't use them. Inline the map/flatten by structural recursion instead (see how R21 walks its usages list by pattern matching `[First | Rest]`).
   - **Symbol quoting in `r22-mode-at-least-partial?`** — patterns like `partial -> true` match the Shen *symbol* `partial`; confirm the bridge emits `[ssa-mp-mode partial]` as a raw symbol, not a string.
   - **`(and A (and B C))` nesting** — Shen's `and` is a special form; if one of the nested calls is being read as a non-function at runtime, the crash surfaces there. Replace manual `and`-nesting with sequential `if` steps (R21 style).
5. After each edit, smoke with: `go test ./pkg/checker/ -run TestR22_SSAMPObserve -v 2>&1 | head -40`. If the panic moves to a different line, that's progress — bisect to the broken helper.
6. Once observe fires: wire partial + nondefault emission paths in `r22-check-row`, then run the other two tests.
7. When green: squash the WIP commit (`git reset --soft mainline`) and commit as one or two clean commits:
   - `ir+bridge+types: add SSAMPConflict extractor + ssa-mp-mode flag for R22` (Go side)
   - `kernel: add R22 SSA × managementPolicies rule + fixtures + tests` (Shen + tests + fixtures)
8. Fast-forward onto mainline: `cd /Users/reuben/projects/cross-validate && git merge --ff-only claude/t2-ssa-mp-rule`.
9. Update `thoughts/shared/orchestration/xpc-fg-manifold-handoff.md` followup #7 from ◐ to ~~struck~~; update scoreboard row from ◐ to ✅.
10. Clean up: `git worktree remove .claude/worktrees/t2-ssa-mp-rule && git branch -d claude/t2-ssa-mp-rule`.

**Load-bearing gotchas** (the prior agent validated these; don't relitigate):
- Fact discriminator is `ssa-mp-conflict-fact` (lowercase-dashed, 8 elements).
- SSA flag emits as the symbol `ssa-yes` / `ssa-no` (never `true`/`false` — Shen uppercase tokens are pattern variables).
- `make-error` signature is `(make-error Code Src Msg Detail Fix Related)` — six args, matching R21.
- `check.shen` already `(load "r22-ssa-managementpolicies-safety.shen")` is wired.

### B. Followup #13: surface real helm stderr when `helm template` fails on remote charts

**Why**: The T1 replay-v3 measurement showed that `fa027fb` fixed remote-chart pull (19 charts / 13 MB warm-cached across fg-manifold) but `XPC.H.helm-renders` stayed at 34 per tip. `helm template` fails on every pulled chart and the error collapses to a scrubbed `"<release>: helm template failed"`. Cause cannot be diagnosed from xpc output alone.

**Scope** (small, self-contained — no new rule):
- `pkg/renderer/helm.go` — wherever `helm template` is exec'd, capture stderr (or CombinedOutput) and propagate the tail into the returned error instead of discarding it.
- Make sure the error still rolls up into the `XPC.H.helm-renders` diagnostic's Detail field, not just logs.
- Add/extend a unit test: `TestRenderChart_PropagatesHelmStderr` — render a chart known to fail, assert the returned error contains identifiable text from helm's real stderr (e.g., "could not find template").
- Once landed, re-run `xpc check` on one fg-manifold tip and paste the new error shape into `thoughts/shared/verify/replay-results-v3.md` §#4b to close the diagnostic loop.

**Not in scope**: fixing the underlying chart issues (subchart deps, pre-apply CRDs) — those are fg-manifold-side. xpc's job is just to surface them clearly.

## Context pointers (do NOT re-derive)

- Preceding plan: `/Users/reuben/.claude/plans/build-a-detailed-plan-snug-sphinx.md` — the four-track plan that drove this wave.
- Preceding handoff: `thoughts/shared/orchestration/xpc-fg-manifold-handoff.md` — full followup ledger, gotcha list, scoreboard.
- Replay methodology: `thoughts/shared/verify/replay-results-v{1,2,3}.md` — three successive measurements; v3 validates XPC006 cartesian fix and remote-chart-pull fix, exposes helm-template followup.
- Shen-rule template: `kernel/r21-late-init-needs-ignore-diff.shen` — closest structural analog for R22.
- Memory file of interest: `/Users/reuben/.claude/projects/-Users-reuben-projects-cross-validate/memory/feedback_agent_worktree_base.md` — why you must pre-create worktrees with an explicit base branch.

## Do NOT

- Do not dispatch R22 on Opus — the prior agent overflowed. Use Sonnet or Haiku; the remaining surface is small.
- Do not use `Agent(isolation: "worktree")` — wrong default base. The `.claude/worktrees/t2-ssa-mp-rule` worktree is already pre-made off the correct branch; agents should `cd` into it as their first step.
- Do not revert any of the Go-side T2 work without reading the test panic first — the Go side is almost certainly fine; the crash is in the Shen kernel.
