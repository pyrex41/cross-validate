# ADR-002: Shen as Canonical Rule Spec, Go as Trajectory Simulator

**Status**: Accepted; supersedes [ADR-001](001-bounded-obligation-taxonomy.md) §3
**Date**: 2026-04-17
**Author**: xpc team

## Context

ADR-001 committed the project to a bounded obligation taxonomy and sketched a
generator-driven architecture in §3: each category's checks would be
materialized by a Go-side generator, and the legacy Shen rules (R1-R11) would
be progressively absorbed into that generator registry.

In the months since, the project has pivoted the substrate several times —
moving rule logic from Go to Shen, then back to Go generators, then back to
Shen — each pivot motivated by the same underlying problem: the taxonomy is
stable, but *where the rules should live* has not been. Each round-trip has
cost weeks of rework and has left the codebase with dead Go generator
scaffolding alongside the working Shen kernel.

Two facts now force a decision:

1. The Shen kernel in `kernel/*.shen` is, in practice, the tool's working
   rulebook. All eleven production rules (R1-R11) live there and are
   executed in-process via [`internal/shenfull`](../../internal/shenfull/). The
   Go-side generator registry in `pkg/obligation/` that ADR-001 §3 called for
   was never populated beyond stubs and has been removed in the "Phase 1
   cleanup" branch preceding this ADR.

2. The first three trajectory invariants under category F
   (`no-dangling-mount`, `no-immutable-change`, `no-rbac-regression`) plus
   the missing `R6c provider-wave < MR-wave` require a stateful step-by-step
   *simulation* of an Argo sync — reasoning about what exists in the cluster
   after each wave, what is being pruned, what is being updated. Shen has no
   good story for stateful simulation. Go has the obvious story: write a
   simulator, emit per-step facts, hand them to the existing rule engine.

The taxonomy stands. The division of labor does not.

## Decision

### 1. Shen owns rules. Go owns IR, simulation, and the transport.

The role of each substrate is fixed:

- **Go** owns parsing (`pkg/loader`), the typed IR (`pkg/types`, `pkg/ir`),
  enrichment passes that pre-resolve cross-references (`pkg/checker/bridge.go`
  `resolvePatchTypes`, `enrichSyncWaves`), the trajectory simulator
  (`pkg/trajectory`), and the Shen bridge (`pkg/checker/bridge.go`).
- **Shen** owns rule logic. Every check that emits a diagnostic is a Shen
  predicate over facts in the World s-expression. The rule files under
  `kernel/` are the canonical, executable rulebook.
- The **World → Shen s-expression contract** defined in
  [`pkg/checker/bridge.go`](../../pkg/checker/bridge.go) `worldToShenObj` is
  the formal interface between the two. Adding a new rule class means
  extending that contract with a new section tag (e.g. `mount-refs`,
  `trajectory`) and writing a Shen rule over it.

### 2. Trajectory simulation is intentionally Go-only.

The sync trajectory — waves, hook phases, state after each step — is computed
in `pkg/trajectory/Simulate` and serialized into the `(trajectory …)` section
of the World. Shen rules pattern-match over steps; they do not simulate.

This is a deliberate split. Shen is well-suited to stateless predicate
evaluation over enumerated facts. It is not well-suited to iterative,
mutation-heavy computation. Producing one step-slice per Argo App per wave
in Go and letting Shen check invariants over those slices plays to the
strengths of both substrates.

### 3. No Go-side rule generators.

ADR-001 §3's "generator registry in `pkg/obligation/`" is superseded.
`pkg/obligation/` is removed. The taxonomy in ADR-001 §1 and §2 remains —
rules are still organized into the 12 categories — but category membership
is documented in `kernel/*.shen` headers and in `docs/obligations.md`, not
encoded as a Go type.

### 4. New invariants land as Shen rules over enriched-World sections.

The recipe for adding an invariant is fixed:

1. Decide what facts the rule needs. If the existing World sections suffice,
   write the Shen rule and stop.
2. If new facts are needed, extend one of:
   - `pkg/ir/` (static extraction from YAML, e.g. `MountRef`, `SARef`,
     `RBACBinding`)
   - `pkg/trajectory/` (per-step facts, e.g. `Delta.Deleted`, `Step.State`)
3. Thread the new fact through `pkg/checker/bridge.go` `worldToShenObj` as a
   new section tag.
4. Write the Shen rule under `kernel/rNN-<name>.shen` and wire it into
   `kernel/check.shen`.
5. Add a fixture under `testdata/fixtures/<name>/` and an integration test in
   `pkg/checker/check_test.go`.

### 5. The "modeled primitives" qualifier is made precise.

ADR-001's completeness claim ("for the modeled primitives, xpc catches any
violation of obligations in the modeled categories") is preserved. The
modeled primitives are exactly the types in `pkg/types/types.go`. The
enriched-World sections (`mount-refs`, `sa-refs`, `rbac-bindings`,
`rbac-rules`, `immutable-fields`, `trajectory`) are each either
static extractions from the IR or deterministic functions of it. This ADR
does not weaken the claim; it locates the surface where the claim is made
precise.

## Consequences

- The Shen kernel is the canonical spec for what xpc checks. Changes to
  `kernel/*.shen` are changes to the product's meaning. Code review should
  treat them with the same weight as API changes.
- Adding a new invariant has a documented, mechanical recipe (§4 above). It
  is not a design decision per rule; it is a routine addition.
- Trajectory simulation is a permanent Go concern. Future work on richer
  sync semantics (helm-rendered resources, prune-last promotion, hook
  phases beyond HookSucceeded/HookFailed) extends `pkg/trajectory/`, not
  the Shen kernel.
- The RBAC-regression check (`no-rbac-regression`, XPC014) operates on
  declared state, not deployed state. This is a conscious trade-off: live
  cluster snapshots would give better signal but require cluster access;
  the simulator-based approach works at lint time from YAML alone.
- Phase 2 of the trajectory simulator leaves `Delta.Updated` empty — the
  simulator consumes a single snapshot per resource, so it cannot diff.
  `XPC013 no-immutable-change` is therefore framework-only until a
  multi-snapshot follow-up ticket is taken up.
- There is no plan to reintroduce a Go-side rule engine. The next pivot
  has to argue against this ADR, not reorganize around it.
