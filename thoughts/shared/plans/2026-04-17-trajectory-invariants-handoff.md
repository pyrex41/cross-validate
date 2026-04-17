# Implementation Handoff — Trajectory Invariants ("The Real Adult Thing")

You are the implementation agent. Your job is to execute the plan at:

**`thoughts/shared/plans/2026-04-17-trajectory-invariants-the-real-adult-thing.md`**

Read it in full before doing anything else. It is self-contained and committed.

## Context you must have before touching code

Read these in order, fully (no `limit`/`offset`):

1. **`thoughts/shared/research/2026-04-17-vision-recap-after-phase1-cleanup.md`** — recaps the three-pivot history (Go-rules → Shen → obligation framework → Shen-as-spec) and contains the rule-by-rule audit that motivated this plan. The follow-up section is the *why* for everything you're about to build.
2. **`thoughts/shared/research/2026-04-16-project-review-pr3.md`** — deep technical state of `pkg/checker`, `pkg/ir`, `pkg/types`, `pkg/audit`. Skim Part 1; ignore Part 2 (it describes a now-deleted obligation framework).
3. **`docs/adr/001-bounded-obligation-taxonomy.md`** — the taxonomy you're filling in. §3 will be superseded by the ADR-002 you write in Phase 5.
4. **`docs/obligations.md`** — the F-category generators you're implementing are listed here under "Trajectory-invariant obligations."
5. **`kernel/r6-wave-ordering.shen`** — the only existing trajectory invariant. Mirror its style for the new R12/R13/R14 rules.
6. **`pkg/checker/bridge.go`** — read it in full. Pay attention to `enrichSyncWaves` ([bridge.go:160](pkg/checker/bridge.go)), `resolvePatchTypes` ([bridge.go:214](pkg/checker/bridge.go)), `worldToShenObj` ([bridge.go:301](pkg/checker/bridge.go)), and `obligationRefForCode` ([bridge.go:597](pkg/checker/bridge.go)).
7. **`kernel/check.shen`** — short. Read in full so you understand exactly which lines need extending.

## Ground rules — non-negotiable

1. **No pivots.** This plan represents an architectural commitment ("Go simulates, Shen checks") that is being baked into ADR-002 in Phase 5. If you find yourself wanting to change the substrate (move trajectory simulation into Shen; move trajectory invariants back into Go; introduce a third runtime), **stop and ask the user.** This is the failure mode the plan exists to prevent.
2. **No scope creep.** The "What We're NOT Doing" section in the plan is exhaustive. If you find yourself wanting to also implement Category D / E / G / H / I, or rewrite R8/R9/R10, or invoke a renderer — **stop and ask.** Each of those is a separate ticket on purpose.
3. **One phase at a time.** Each phase ends with an "Implementation Note" telling you to pause for confirmation. Honor those pause points. Do not pre-stage Phase N+1 work in a Phase N commit.
4. **Existing tests must stay green at every checkpoint.** If you break `go test ./...`, fix it before continuing — do not defer.
5. **Mirror existing patterns; don't invent new ones.**
   - For Go enrichment passes: mirror `enrichSyncWaves` and `resolvePatchTypes`.
   - For Shen rule files: mirror `kernel/r6-wave-ordering.shen` (top-level `check-rN`, helpers below).
   - For test fixtures: mirror `testdata/fixtures/wave-ordering/app.yaml` (single-file YAML stream).
   - For integration tests: mirror `loadFixture` / `checkFixture` / `findDiagByCode` in `pkg/checker/check_test.go`.
6. **`ResourceInfo.Raw` is the YAML escape hatch.** Do not re-parse YAML; do not change the loader. All of the new extraction in Phase 1 reads from `Raw`.
7. **No new external dependencies** without asking. The current deps are `tiancaiamao/shen-go` and `gopkg.in/yaml.v3`. That's the budget for this work.
8. **Determinism.** Every new section emitted into the Shen world must be sorted by a stable natural key Go-side, the same way `worldToShenObj` already sorts CRDs/XRDs/Compositions etc. Re-running `xpc check` on the same input must produce byte-identical output.

## Phase-by-phase execution checklist

For each phase, you will:

1. Re-read the relevant section of the plan.
2. Implement the changes the plan lists, in the order the plan lists them.
3. Run the **Automated Verification** checklist for the phase. **All boxes must check.**
4. If the phase has **Manual Verification**, run the listed commands and capture the output.
5. Pause and report to the user. Wait for explicit go-ahead before starting the next phase.

### Phase 1 verification
```bash
go build ./...
go test ./pkg/ir/... -run TestEnrichTrajectoryData
go test ./...
go vet ./...
```

### Phase 2 verification
```bash
go build ./...
go test ./pkg/trajectory/...
go test ./...
go vet ./...
```

### Phase 3 verification
```bash
go build ./...
go test ./pkg/checker/... -run TestBridge_TrajectorySerialization
go test ./...
go run ./cmd/xpc check testdata/fixtures/basic    # must produce same output as before
```

### Phase 4 verification
```bash
go build ./...
go test ./pkg/checker/... -run 'TestR6c_ProviderWave|TestR12_DanglingMount|TestR13_RuleLoaded|TestR14_RbacRegression'
go test ./...
go run ./cmd/xpc check testdata/fixtures/basic              # no XPC012/013/014
go run ./cmd/xpc check testdata/fixtures/wave-ordering      # same as before
go run ./cmd/xpc check testdata/fixtures/dangling-mount     # XPC012 fires; capture output for the user
go run ./cmd/xpc check testdata/fixtures/rbac-regression    # XPC014 fires; capture output for the user
go run ./cmd/xpc check testdata/fixtures/provider-wave      # XPC006 fires with R6c message; capture output
```

### Phase 5 verification
```bash
go build ./...
go test ./...
ls docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md   # exists
grep -q 'XPC012' docs/obligations.md                                  # updated
grep -q 'superseded' docs/adr/001-bounded-obligation-taxonomy.md      # has supersession note
```

## When you get stuck

- **Tests fail in a way the plan didn't predict:** stop, share the failure with the user, propose a fix that stays within the plan's scope. Do not "fix" by expanding scope.
- **The Shen kernel rejects your new rule:** debug with a tiny direct `kl.Eval` of the load expression in a throwaway test before assuming the issue is structural. Shen-go's parser is finicky about pattern-variable casing and `where` guards (notes in `pkg/shen/eval.go` from the earlier custom evaluator are not authoritative for `tiancaiamao/shen-go` — read its parser if needed).
- **`ResourceInfo.Raw` doesn't have what you need:** check whether the loader actually populates it for the kind in question (some classifications may zero it out). If it's a real loader bug, surface it and stop — fixing the loader is in-scope only if a Phase-1 enrichment cannot proceed without it.
- **You feel a strong urge to refactor `pkg/checker/bridge.go`:** resist. The bridge is intentionally large and intentionally one file. The plan adds to it; the plan does not restructure it.

## What to commit

One commit per phase. Suggested messages:

1. `Phase 1: extract MountRef/SARef/RBAC into typed IR + immutable-field registry`
2. `Phase 2: trajectory simulator (pkg/trajectory)`
3. `Phase 3: serialize trajectory + reference graphs into Shen world`
4. `Phase 4: trajectory invariants (R6c, R12 dangling-mount, R13 immutable-change, R14 rbac-regression)`
5. `Phase 5: ADR-002 — commit to Shen-as-spec + Go-side simulator`

Each commit must build cleanly and pass `go test ./...`.

## When you are done

Report to the user with:

1. The five commit SHAs.
2. The output of `go run ./cmd/xpc check` against each of the three new fixtures, so they can eyeball that the diagnostics read well.
3. Any deviations from the plan and *why*.
4. Any items you noticed that should become follow-up tickets (especially around the Phase 2 stub `Updated []ResourceKey` and the audit/RuleSubtrees inconsistency mentioned in Migration Notes).

That's it. Read the plan. Read the research. Build the real adult thing.
