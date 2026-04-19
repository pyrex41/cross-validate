# xpc → fg-manifold Coverage — Orchestration Handoff

Running state of the 5-session team-of-agents plan. Update after each wave.

## Plans (external, do not duplicate)

- **Base plan** (source of truth): `/Users/reuben/.claude/plans/research-written-wiggly-nova.md` — defines S1–S5, success criteria, target MRs.
- **Orchestration meta-plan**: `/Users/reuben/.claude/plans/users-reuben-claude-plans-research-writ-virtual-shell.md` — team shape, wave pipeline, dispatch cheat sheet.

## Wave status

| Wave | Session | Status | Branch | Notes |
|---|---|---|---|---|
| 0 | Makefile + prep artifacts | ✅ merged | `claude/phase1-cleanup` @ `64fa2f3` | Makefile, `thoughts/shared/prep/{s2,fixtures/{s3,s4,s5}}/` committed |
| 1 | S1 — XPC.D.kind-whitelisted (R15) | ✅ merged | `claude/phase1-cleanup` @ `849a129` | 6 impl commits + 1 verify report. Verify report: `thoughts/shared/verify/s1-report.md` |
| 2 | S2 — XPC.E.selector-needs-ignore-diff (R16) | ⬜ next | — | Consumes `thoughts/shared/prep/s2/selector-mappings.md` (53 rows) |
| 3 | S3 — XPC.A.resource-field-valid | ⬜ | — | Bumps audit proof to v4. Consumes `thoughts/shared/prep/fixtures/s3/` |
| 4 | S4 — XPC.H.helm-renders + values-well-typed | ⬜ | — | Consumes `thoughts/shared/prep/fixtures/s4/` |
| 5 | S5 — Kustomize + AppSet + determinism | ⬜ | — | Consumes `thoughts/shared/prep/fixtures/s5/` |

## Dispatch recipe (validated on S1)

**DO NOT** use `Agent(isolation: "worktree")` on this repo — the tool's default base is `origin/HEAD = claude/build-xpc-type-checker-TfgsT` (pre-Shen-runtime, wrong architecture). See `~/.claude/projects/-Users-reuben-projects-cross-validate/memory/feedback_agent_worktree_base.md`.

Correct pattern for each session:

```bash
# 1. Pre-create a worktree with explicit base
git worktree add .claude/worktrees/sN-impl -b claude/xpc-sN-<slug> claude/phase1-cleanup

# 2. Dispatch implementer without isolation parameter, telling it to cd into the pre-made worktree
#    as its first action. Include a sanity check: "wc -l pkg/checker/bridge.go should be ~896+"
```

Gate each session with a separate verifier agent (Haiku is enough). Verifier writes `thoughts/shared/verify/sN-report.md` and does NOT modify source. Human gate reviews the report before merge.

After merge:
```bash
git checkout claude/phase1-cleanup
git merge --ff-only claude/xpc-sN-<slug>
cp <worktree>/thoughts/shared/verify/sN-report.md thoughts/shared/verify/
git add thoughts/shared/verify/sN-report.md && git commit ...
git worktree remove -f -f <worktree-path>
git branch -d claude/xpc-sN-<slug>
```

## S2 dispatch — exact next steps

1. Pre-create worktree:
   ```bash
   git worktree add .claude/worktrees/s2-impl -b claude/xpc-s2-selector-ignore-diff claude/phase1-cleanup
   ```
2. Dispatch `Agent(subagent_type="general-purpose", model="sonnet", name="s2-impl")` with a prompt built from base-plan lines 123–181 ("Session 2") plus:
   - Path to the prep artifact: `thoughts/shared/prep/s2/selector-mappings.md` (53 rows ready to paste into `pkg/ir/selector_registry.go`)
   - Verified anchors on current HEAD: re-run `grep -n` against `pkg/ir/immutable_registry.go`, `pkg/ir/trajectory_extract.go`, `pkg/types/types.go` (World at 630, ImmutableField struct), `pkg/checker/bridge.go` (sortedSection at 331, worldToShenObj at 341), `kernel/check.shen` (loads end ~31, extracts ~58–73) BEFORE writing the prompt — S1 shifted some lines
   - Gating rules (unchanged from S1 prompt): no proof version bump, no `t.Parallel()`, Shen path only, no obligation-framework
   - First-action sanity check: `cd .claude/worktrees/s2-impl && wc -l pkg/checker/bridge.go` → expect ≥940 lines (S1 added ~43 to the ~896 baseline)
3. Verifier after implementer returns. Same shape as S1 verifier but running S2 success criteria (base plan lines 183–193).

## Key file locations

- S1 verify report: `thoughts/shared/verify/s1-report.md`
- S2 prep artifact: `thoughts/shared/prep/s2/selector-mappings.md`
- Makefile targets: `test`, `lint`, `build` (note: `make lint` has pre-existing failures in `internal/shenfull/*` generated code and a handful of pre-existing-unmodified files — treat those as baseline, fail the check only on NEW regressions)

## Gotchas captured so far

- **Worktree default base**: see memory file, pre-create worktrees manually.
- **`argoAppToObj` is pattern-matched in r6/r6c/r7**: S1 added a separate `argo-app-proj-links` section rather than modifying the tuple. Future rules touching App facts should follow the same pattern (new section, not field addition) to avoid breaking existing rules.
- **Manual fg-manifold replay**: target MRs may already be merged upstream, so "find the miss live" isn't always possible. Fixture-based validation is the primary signal; real-world replay is a crash/false-positive smoke test.
- **Implementer reporting**: S1 implementer v1 reported "`make test` passed" against a worktree that had no Makefile — it ran `go test` directly. Verify success-criteria commands were literally run, not paraphrased.
