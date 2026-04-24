# ADR-004: P-prefix diagnostic codes for plan-mode-only rules

## Status

Accepted — 2026-04-24.

## Context

xpc's earliest rules (R1–R11) emitted numeric diagnostic codes —
`XPC001` through `XPC011` — handed out in implementation order with no
structural relationship to the taxonomy. ADR-001 introduced the 12
obligation categories (A–L) and committed new rules to the structured
form `XPC.<Category>.<Generator>[.<Instance>]`. Subsequent rules
followed that form: `XPC.D.kind-whitelisted` (R15), `XPC.E.selector-needs-ignore-diff`
(R16), `XPC.H.composition-renders` (R17-era), `XPC.S.crossplane-state-needs-orphan`
(R23). The letter is a category; the second segment names the generator.

P4.b added a rule that does not fit this pattern cleanly. R26
(destructive-delete) and R27 (immutable-change) only make sense when
xpc is given *two* inputs — a base ref and a head ref — and asked
"what changes across this plan?". Running them against a single tip is
meaningless: removal and modification are relations between two
Worlds, not properties of one. The single-tip `xpc check` command has
no base/head to diff; only `xpc plan --base --head` does.

These rules were given a new prefix letter — `P` — for plan-mode.
`XPC.P.destructive-delete`, `XPC.P.immutable-change`, and
`XPC.P.cascade-risk` are the three codes in use today, all emitted
from `pkg/plan/`. None of the A–L category letters fit: `P` is not
naming a new obligation category, it is naming a new *mode* of
invocation. The choice to overload the same `XPC.<letter>.<name>`
slot for a mode distinction has not been documented. This ADR records
it.

This ADR is scoped to the diagnostic code shape. The rules themselves,
their bypass annotations, and the plan-mode markdown layout are
documented elsewhere (`pkg/plan/`, `cmd/xpc/main.go` explanations).

## Decision

### 1. Letter-prefixed codes replace numeric codes for all new rules.

ADR-001 §6 already established the structured form. Legacy codes
`XPC001`–`XPC014` remain as aliases for their corresponding rules but
no new numeric code will be allocated. The structured form carries
provenance (which taxonomy cell the check belongs to) and is the
contract surface for `xpc explain`, which documents at the
generator level.

### 2. `P` is the diagnostic-code letter reserved for plan-mode-only rules.

The letter `P` is distinct from the A–L obligation categories of
ADR-001. It is the one axis along which xpc's taxonomy does *not*
run: A–L classify *what* is checked; `P` classifies *when*. A
`P`-prefixed code is emitted only by the plan driver in
`pkg/plan/runner.go`, never by the single-tip check path in
`pkg/checker/`.

The second segment is the rule's short name (`destructive-delete`,
`immutable-change`, `cascade-risk`), not a generator within a
category. Plan-mode rules are not generated from a bounded enumeration
the way schema obligations are; they are written by hand against
`pkg/plan/ResourceDelta`.

### 3. The contract.

A `P`-prefixed code carries three guarantees:

- **Emission site.** A diagnostic with `Code` matching `XPC.P.*` MUST
  only be produced by the `pkg/plan/` package, invoked via `plan.Run`
  from `xpc plan`. The Shen kernel does not and will not emit
  `XPC.P.*` codes; the single-tip checker does not and will not emit
  `XPC.P.*` codes. Downstream consumers (proof generator, SARIF
  writer, CI integrations) can key off the `XPC.P.` prefix to decide
  whether a diagnostic is plan-scoped.

- **Input shape.** A `P`-prefix rule takes a `ResourceDelta` — a
  `{Added, Removed, Modified}` triple over typed resource identities
  with both `BaseRaw` and `HeadRaw` available on each modified entry.
  It has no access to a single World; the diff *is* the input. Rules
  that need only one side operate on either the base or head slice
  of the delta.

- **Single-tip invariance.** `xpc check` MUST NOT emit any
  `XPC.P.*` code, whether or not plan-mode logic happens to be
  statically reachable from the same binary. Under the current layout
  this falls out of the package split: `pkg/checker` does not import
  `pkg/plan`, and `runCheck` in `cmd/xpc/main.go` never calls the
  `R26`/`R27` entry points. Future rules must preserve that split.

### 4. Codes using the P prefix today.

Exhaustively, by grep over `pkg/plan/`:

| Code | Rule | Source |
|------|------|--------|
| `XPC.P.destructive-delete` | R26 — state-bearing Crossplane MR removed across plan without `deletionPolicy: Orphan` | `pkg/plan/r26.go` |
| `XPC.P.cascade-risk` | R26 — ArgoCD Application removed across plan with cascading finalizer | `pkg/plan/r26.go` |
| `XPC.P.immutable-change` | R27 — scalar-leaf immutable field modified across plan | `pkg/plan/r27.go` |

`XPC013 no-immutable-change` (the retired trajectory-axis form) was
subsumed by `XPC.P.immutable-change` in P4.d; `kernel/r13-no-immutable-change.shen`
is kept as a dormant reference and is not loaded by `check.shen`.

### 5. Adding a new P-prefix rule.

Implement the rule in `pkg/plan/rNN_<name>.go` against
`ResourceDelta`, wire it into `Run` in `pkg/plan/runner.go` after the
existing `R26`/`R27` calls, and add an entry in `errorExplanations`
in `cmd/xpc/main.go`. The check path stays untouched.

## Consequences

- Plan-mode rules have a visible surface in tooling. CI integrations
  that want to fail the pipeline only on destructive-plan findings
  can match `XPC.P.*` and nothing else — exactly the condition the
  `runPlan` exit-code loop in `cmd/xpc/main.go` already uses.

- `xpc explain` gains a clear signal that a code is plan-scoped:
  every `XPC.P.*` explanation opens with "Emitted only by
  'xpc plan --base --head'." The prose is load-bearing — users who
  hit the code for the first time need to understand immediately why
  it did not fire on their single-tip CI run yesterday.

- The letter `P` is now reserved and may not be introduced as an
  obligation category in a future ADR. If a 13th obligation
  category is ever added, it must pick a letter outside both A–L
  and P. This is cheap: the Latin alphabet has 14 free letters left.

- The Shen kernel's category-letter vocabulary (A, D, E, H, S today;
  B, C, F, G, I, J, K, L implicit) stays uncontaminated by mode
  concerns. A reader of `kernel/*.shen` can trust that every code
  emitted there is a single-tip-checkable obligation.

- `pkg/plan/` is the only place P-prefix codes are manufactured.
  Concentrating them there means the bypass semantics (`xpc.io/allow-delete`
  for R26, `xpc.io/allow-immutable-change` for R27) live next to the
  code emission, not scattered across the kernel.

## Alternatives considered

- **Keep numeric codes (XPC015, XPC016, …).** Rejected for the reasons
  in ADR-001 §6: numeric codes carry no provenance, don't compose with
  `xpc explain` at the generator level, and make it impossible to
  distinguish plan-mode from check-mode diagnostics by inspection.
  Every other rule added since ADR-001 uses the structured form;
  regressing the two plan rules would be a stylistic wart.

- **Reuse an existing category letter (e.g. `XPC.S.destructive-delete`).**
  Rejected: the A–L/S letters classify *what* is checked against. R26
  and R27 check the same `(kind, deletionPolicy)` and `(kind, field)`
  surfaces that category S and category A already cover — but only
  under a different input shape (a diff between two Worlds). Burying
  the mode distinction inside the category letter would make it
  invisible at the code surface, and a CI integration would have to
  maintain an exception list to tell plan-scoped S-codes from
  single-tip S-codes.

- **Encode mode in severity (e.g. use `error` for single-tip, a new
  `destructive` severity for plan-mode).** Rejected: severity is
  already overloaded — it drives exit codes, SARIF level mapping, and
  CI gating thresholds. Stacking a mode axis on it would force
  downstream consumers (proof writer, SARIF exporter, markdown
  renderer) to learn a new severity tier, and would leave the code
  itself mode-ambiguous. Severity answers "how bad?", not "under
  what invocation?".

- **Encode mode in a suffix (e.g. `XPC.S.destructive-delete.plan`).**
  Rejected: ADR-001 reserves the third segment for the *instance*
  identifier (the specific Composition, the specific ignoreDifferences
  entry). Overloading it for a mode tag would break `xpc explain`'s
  generator-level lookup — the explain map keys on the full code, and
  `XPC.S.destructive-delete.plan` would either need to match by
  prefix (a new lookup scheme) or duplicate every explanation entry.

- **Add a separate `Mode` field to the `Diagnostic` struct.** Rejected:
  this is the cleanest design on paper but the most disruptive in
  practice. `types.Diagnostic` is serialized into JSON output, SARIF
  output, the proof file format, and the snapshot format; a new field
  propagates through every consumer and every on-disk artifact
  version. The P-prefix encodes the same information in an existing
  string, with zero schema churn. A `Mode` field would only pay for
  itself if we needed to filter on mode independently of code, which
  we don't — every `XPC.P.*` code *is* plan-mode, tautologically.

## Related

- [ADR-001](001-bounded-obligation-taxonomy.md) — the A–L obligation
  taxonomy and the structured-code form this ADR extends.
- [ADR-002](002-shen-as-canonical-spec-and-trajectory-simulator.md) —
  the Shen-owns-rules split; `P`-prefix rules are the one class of
  rules Shen does *not* own.
- `pkg/plan/r26.go`, `pkg/plan/r27.go` — the three production
  `XPC.P.*` emitters.
- `cmd/xpc/main.go` `runPlan` — the exit-code path that keys on
  `XPC.P.` prefix + `SeverityError`.
