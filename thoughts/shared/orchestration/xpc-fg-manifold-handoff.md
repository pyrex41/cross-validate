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
| 2 | S2 — XPC.E.selector-needs-ignore-diff (R16) | ✅ merged | `claude/phase1-cleanup` @ `994e052` | 6 impl commits + verify report + frontmatter fixup. Verify report: `thoughts/shared/verify/s2-report.md`. Registry: 53 entries (35 scalar-active + 18 array-path TODO). |
| 3 | S3 — XPC.A.resource-field-valid | ⬜ next | — | Bumps audit proof to v4. Consumes `thoughts/shared/prep/fixtures/s3/` |
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

## S3 dispatch — exact next steps

1. Pre-create worktree (NOTE: S3 bumps proof format v3→v4, expect wider blast radius):
   ```bash
   git worktree add .claude/worktrees/s3-impl -b claude/xpc-s3-resource-field-valid claude/phase1-cleanup
   ```
2. Dispatch `Agent(subagent_type="general-purpose", model="sonnet", name="s3-impl")` with a prompt built from base-plan lines 185–** (find S3 end-marker in `/Users/reuben/.claude/plans/research-written-wiggly-nova.md`) plus:
   - Path to fixture prep: `thoughts/shared/prep/fixtures/s3/` (list contents first to know what's there)
   - Verified anchors on current HEAD: re-run `grep -n` for `pkg/schemas/fetcher.go` (ResolveFieldType, TypeAssignable), `pkg/audit/proof.go` (current v3 format constant + marshal), `pkg/checker/bridge.go` (now 1041 lines), `kernel/check.shen` (now 134 lines)
   - Gating rules: **S3 IS authorized to bump proof format** (explicit in base plan). Still no `t.Parallel()`, Shen path only where possible (may need Go-native schema walker), no obligation-framework wiring
   - First-action sanity check: `wc -l pkg/checker/bridge.go` → expect 1041, `wc -l pkg/ir/selector_registry.go` → expect ~465 (proves the tree is post-S2)
3. Verifier after implementer returns. S3 adds a proof-version migration check: verifier must confirm v3→v4 bump happened cleanly (old fixtures still replay, new field reason captured in proof).

### Past S2 gotchas (kept for S3 to read)

- **Shen `check-world` paren discipline**: adding one new Section extract + one new Rule binding requires exactly one more `)` at the end of the big `let`. Off-by-one yields `Panic: &{22}` from `PrimSimpleError` — bisect paragraph-by-paragraph if it fires.
- **Shen string literals DO NOT support `\"`**: keep all quotes out of kernel-file strings. Use `cn` concatenation with pre-built quote-free segments.
- **Prelude `string-contains?` arg order is (Haystack Needle)** — easy to get backwards.
- **Array-indexed selector paths (`spec.x.y[].z`) were punted in S2 trajectory_extract** — 18 of 53 rows are inert. If S3 needs to walk array-indexed schema paths for CRD validation, this is the same problem; consider sharing a helper.

## Key file locations

- S1 verify report: `thoughts/shared/verify/s1-report.md`
- S2 verify report: `thoughts/shared/verify/s2-report.md`
- S2 prep artifact: `thoughts/shared/prep/s2/selector-mappings.md` (consumed; 53 rows now live in `pkg/ir/selector_registry.go`)
- S3 prep artifacts: `thoughts/shared/prep/fixtures/s3/` (not yet inspected by orchestrator — S3 dispatcher should `ls` it first)
- Makefile targets: `test`, `lint`, `build` (note: `make lint` has pre-existing failures in `internal/shenfull/*` generated code and a handful of pre-existing-unmodified files — treat those as baseline, fail the check only on NEW regressions)

## Gotchas captured so far

- **Worktree default base**: see memory file, pre-create worktrees manually.
- **`argoAppToObj` is pattern-matched in r6/r6c/r7**: S1 added a separate `argo-app-proj-links` section rather than modifying the tuple. Future rules touching App facts should follow the same pattern (new section, not field addition) to avoid breaking existing rules.
- **Manual fg-manifold replay**: target MRs may already be merged upstream, so "find the miss live" isn't always possible. Fixture-based validation is the primary signal; real-world replay is a crash/false-positive smoke test.
- **Implementer reporting**: S1 implementer v1 reported "`make test` passed" against a worktree that had no Makefile — it ran `go test` directly. Verify success-criteria commands were literally run, not paraphrased.
