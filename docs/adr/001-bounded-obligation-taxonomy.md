# ADR-001: Bounded Obligation Taxonomy

**Status**: Accepted
**Date**: 2026-04-12
**Author**: xpc team

## Context

xpc v0.1 ships with 11 hand-written rules (R1-R11). Each rule is a standalone
Go function in `pkg/checker/rules.go` that loops over an input set and emits
diagnostics. Adding a 12th rule means writing a 12th function. There is no
structural relationship between the rules, no story for why R11 exists and R12
doesn't, and no mechanism to claim "we check everything in category X." The
rules are useful but ad hoc.

The vision for xpc v1.0 is a tool that can make a defensible completeness
claim: "for the modeled primitives, we catch any violation of obligations in
the modeled categories." This requires the checks to be derived from a bounded,
documented taxonomy of obligation categories, not accumulated one-off pattern
matches.

## Decision

### 1. Fixed obligation taxonomy

All checks in xpc are organized into exactly 12 obligation categories. Each
category has a documented scope, a generator contract, and an exhaustiveness
criterion. New checks MUST fit into an existing category. New categories require
a new ADR extending this taxonomy.

The 12 categories are:

| ID | Category | Scope |
|----|----------|-------|
| A  | Schema obligations | (CRD x field) over cluster context |
| B  | Reference-resolution obligations | Cross-references in IR |
| C  | Version-coherence obligations | (CRD x version) over cluster context |
| D  | AppProject-constraint obligations | (Application x AppProject) |
| E  | Sync-option interaction obligations | (syncOption x resource-kind) per Application |
| F  | Trajectory-invariant obligations | (invariant x sync-step) per Application |
| G  | Cross-Application obligations | (rendered resource x peer set) |
| H  | Rendering obligations | (Application x source) |
| I  | Provider-capability obligations | (managed-resource x provider-version) |
| J  | Conversion-cost obligations | (CRD x webhook) |
| K  | Secret-flow obligations | (field x taint lattice) per rendered manifest |
| L  | Deprecation/calendar obligations | (API x k8s version x date) |

### 2. Generator-driven rule materialization

Each category is implemented by one or more **generators**. A generator takes
the cluster context and the IR World as input and produces a finite,
deterministic list of **obligations**. Each obligation has:

- A structured ID (`XPC.<Category>.<Generator>.<Instance>`)
- A provenance record (which generator, which inputs)
- A discharge function that proves or falsifies the obligation
- A diagnostic emitted on failure

The checker's outer loop is:

```
for each generator in registry:
    obligations = generator.Generate(ctx, world)
    for each obligation:
        result = obligation.Discharge(ctx, world)
        record(result)
```

### 3. No checks outside the taxonomy

No new check may be added to xpc without being associated with a category and
a generator. The discipline is enforced by code structure: the only way to emit
a diagnostic is through the obligation framework in `pkg/obligation/`.

The legacy `pkg/checker/rules.go` path is preserved during the transition but
will be removed once all R1-R11 rules are absorbed into generators.

### 4. Completeness claim

Once all generators are implemented for all 12 categories, xpc's completeness
claim becomes:

> For input I and cluster context C, xpc enumerates every obligation in
> categories A-L against (I, C) and discharges each one. If xpc returns no
> errors, no obligation in any modeled category is violated by I against C.

The qualifiers:
- "modeled categories" -- bugs outside the 12 categories are not claimed
- "modeled primitives" -- only primitives with IR types are checked
- "simulated trajectory" -- runtime bugs outside the apply loop are not caught

### 5. Absorption of R1-R11

Existing rules map to categories as follows:

| Rule | Category | Generator |
|------|----------|-----------|
| R1   | C        | version-coherence |
| R2   | J        | conversion-cost-opt-in |
| R3   | B        | comp-xrd-ref |
| R4   | B        | pipeline-fn-ref |
| R5   | A + B    | patch-source-type, patch-target-type |
| R6   | F        | trajectory-wave-order |
| R7   | G        | cross-app-label-tracking |
| R8   | A + C    | crossplane-machinery-placement |
| R9   | F        | trajectory-bootstrap |
| R10  | K        | secret-source-sink |
| R11  | L        | api-deprecation-calendar |

### 6. Error code restructuring

Legacy codes XPC001-XPC011 remain as aliases. New obligations use structured
codes: `XPC.<Category>.<Generator>[.<Instance>]`. The `xpc explain` command
documents at the generator level.

## Consequences

- No more ad hoc rules. Every check has a category, a generator, and a
  provenance trail.
- The obligation count scales with input size, not with developer effort.
  Hundreds or thousands of obligations per run is expected and correct.
- Adding a new CRD to the cluster adds obligations automatically (via
  schema and version-coherence generators). No code changes needed.
- Adding a genuinely new *kind* of check (not a new instance of an existing
  kind) requires extending the taxonomy via a new ADR.
- The transition from R1-R11 to generators is incremental: each rule is
  ported independently, the old path stays until all are absorbed.
