# ADR-005: The Runtime-Decidable Subset (xpcd)

**Status**: Accepted
**Date**: 2026-06-08
**Author**: xpc team

> **Filename note**: the runtime work was scoped as "ADR-003" while ADR
> numbers 003 ([appset-expansion](003-appset-expansion.md)) and 004
> ([p-prefix-diagnostic-codes](004-p-prefix-diagnostic-codes.md)) were still in
> flight. To keep ADR numbers collision-free and chronological it lands as
> **005**. The subject is unchanged: the runtime-decidable subset and the
> `xpcd` daemon.

## Context

`xpc` is a static analyzer. It runs offline at lint/CI time over a directory of
YAML: no operator, no cluster credentials, no controller in the data path
([README](../../README.md)). That is a deliberate and load-bearing property —
it is what lets `xpc check` run pre-merge on every MR and what
[ADR-002](002-shen-as-canonical-spec-and-trajectory-simulator.md) leans on when
it makes the Shen kernel the canonical rulebook.

It also leaves a gap. CI only sees what flows through CI. [INC-6](../inc-6.md)
was a SEV-2 in which an ApplicationSet cascade-deleted ~70 state-bearing
Crossplane managed resources (Aurora, DocDB, KMS, S3) because `deletionPolicy`
defaulted to `Delete`. `xpc` now encodes that failure as a **static floor**
(R23/R24/R25, `xpc check --focus=inc6-floor`) — but only objects that reach a
linted repo are covered. An object applied out-of-band with `kubectl`, written
by a controller, or merged through a repo that does not run `xpc` never meets
the static floor. [INC-6](../inc-6.md) already names the missing half: the
static floor is the lint-time analog of fg-manifold's runtime
`crossplane-state-require-orphan` **ValidatingAdmissionPolicy** (VAP). We have
the static analog. We do not have a first-party runtime analog that speaks the
same rulebook.

The instinct is to write the runtime check in a policy DSL — Cedar, or OPA /
Rego, or a hand-rolled VAP CEL expression. We rejected that. A second rulebook
is a second source of truth: the "state-bearing kind" allowlist, the prod-name
patterns, the bypass-annotation semantics would all have to be re-encoded and
kept in sync with `kernel/*.shen` by hand. Cedar in particular is too specific
to its authorization model to express the obligation taxonomy without
distortion. The whole point of [ADR-002](002-shen-as-canonical-spec-and-trajectory-simulator.md)
is that the Shen kernel *is* the meaning of the product; forking that meaning
into a second engine at admission time would re-create exactly the drift the
single-kernel decision was meant to kill.

## Decision

### 1. The runtime twin reuses the Shen kernel. No second engine.

`xpcd` (`cmd/xpcd`) is the always-on cluster companion to `xpc`. It is a
Kubernetes `ValidatingWebhook` — a plain `net/http` server speaking
`AdmissionReview` v1 JSON, with no client-go in the admission path — that
evaluates the **same** `kernel/*.shen` rulebook against a single live object at
admission time. There is no rule duplication and no policy DSL: the runtime
floor and the static floor are byte-for-byte the same predicates.

`xpcd` restricts the kernel using the **existing `RuleAllowlist` mechanism**
(`kernel/check.shen` `rule-allowed?`; the same wiring `xpc check
--focus=inc6-floor` uses, `cmd/xpc/main.go` `focusPresetAllowlist`). An empty
allowlist runs everything; a non-empty allowlist gates each `check-rN` dispatch.
`xpcd` simply passes the runtime-decidable subset as the allowlist. The kernel
needs no runtime-specific branch.

### 2. The runtime-decidable subset is a formal property, not a hand-picked list.

A rule is **runtime-decidable** when, evaluated against a single
`AdmissionReview` object, it is:

- **Single-object sound** — the verdict depends only on the object under review
  (and, at most, on cached cluster *schema/type* context), never on resolving a
  reference to *another mutable object*. A rule that joins across repos or
  resolves a live cross-reference can flip its verdict the instant the referent
  changes, which at admission time produces **false positives** (deny something
  that is actually fine) — the one outcome an admission gate must not have.
- **Terminating** — evaluation provably halts. The kernel predicates in the
  subset are structural recursion over the object's own fields; no fixpoint
  iteration, no trajectory simulation.
- **Bounded-cost** — evaluation cost is bounded by the object size, with no
  cluster fan-out on the hot path. Admission has a latency budget (the webhook
  `timeoutSeconds`); an unbounded rule blows it.

Rules sort into three tiers by how much context they need:

| Tier | Needs | At admission |
|------|-------|--------------|
| **single-object** | the object alone | evaluated directly; the default subset is all here |
| **ambient-with-cache** | object + cached cluster *types* (CRD/XRD/Composition/Provider schema) | evaluable against a watch-backed cache; verdict still single-object once the cache is warm |
| **excluded — needs-trajectory** | two Worlds, sync-wave simulation, or cross-repo joins | **not** run at admission; stays a CI/plan-time concern |

The **default subset is the INC-6 safety floor plus the self-contained rules**:

| Rule | Code | Tier |
|------|------|------|
| R23 crossplane-state-needs-orphan | `XPC.S.crossplane-state-needs-orphan` | single-object |
| R24 appset-finalizer-without-preserve | `XPC.E.appset-finalizer-without-preserve` | single-object |
| R25 prod-appset-autosync | `XPC.E.prod-appset-autosync` | single-object |
| R22 ssa-managementpolicies | `XPC.E.ssa-managementpolicies-observe` / `-partial` / `-nondefault` | single-object |
| R29 fargate-claim-env-label | `XPC.E.fargate-claim-env-label` | single-object |
| R31 forprovider-canonical-form | `XPC.M.forprovider-canonical-form` | single-object (Tier-1 resource walk) |
| R33 duplicate-env-key | `XPC.M.duplicate-env-key` | single-object |

Explicitly **excluded** at admission: the reference-resolution rules (category
B), the trajectory invariants (category F — by construction, per ADR-002 §2),
the cross-Application rules (category G), the rendering rules (category H), and
the plan-mode variant-axis rules (the `XPC.P.*` family,
[ADR-004](004-p-prefix-diagnostic-codes.md)). These are not weaker checks; they
are checks whose *natural data shape is not a single object*, so they remain
where they are sound — in `xpc` at CI/plan time.

### 3. Two modes. Audit is the default.

`xpcd serve` runs in one of two modes via `--mode`:

- **`audit`** (default) — evaluate, emit a decision event and metrics, **always
  admit**. Never blocks. This is the safe rollout posture.
- **`enforce`** — deny objects that produce an error-severity diagnostic in the
  subset; admit everything else, still emitting events.

The `ValidatingWebhookConfiguration` sets `failurePolicy: Ignore` in **both**
modes: a down, slow, or erroring daemon must never wedge the cluster. Enforce
narrows what gets through; it does not change the fail-open posture.

### 4. Observability is a first-class output, not a side effect.

Every admission produces a structured **JSONL decision event** (for ClickHouse
or log shipping) and updates Prometheus counters/histograms at `/metrics`. The
event records the verdict, mode, object identity, the rule codes that fired,
and evaluation time. The point is that "what would enforce have blocked?" is
answerable from the audit stream *before* anyone flips to enforce. The event
shape and metrics are specified in [docs/runtime-policy.md](../runtime-policy.md)
and are part of this decision: a runtime floor you cannot observe is one you
cannot trust enough to enforce.

### 5. Controller sweep (ambient tier).

`xpcd serve` (admission) decides on a single object, so it can only run the
**single-object** tier soundly: the ambient-tier rules in §2 need the cluster
type-environment (selector resolution, late-init ignore-diff context) that is
*not* present in one `AdmissionReview` payload. Running them at admission would
mean resolving a reference to an object the webhook cannot see — the exact
false-positive-denial failure §2 forbids.

`xpcd watch` (the controller) removes that constraint by changing the unit of
evaluation. Each interval it **captures the whole cluster** — the same kubectl
path `xpc snapshot` uses (`pkg/clustersrc`): it lists the fixed Crossplane/Argo
kinds, discovers managed-resource CRDs dynamically, and lists each one
cluster-wide — and runs the kernel over that complete captured world. Because
**every referenced object is present in the capture**, the ambient-tier rules
are sound at runtime: there is no unresolved reference that could flip a verdict
the instant an unseen referent changes. The controller therefore runs the
single-object subset **plus** the ambient tier, lighting up rules admission
structurally cannot.

The controller is **observe-only** by deliberate design — it never mutates the
cluster. Three reasons:

- **Soundness is about reading the whole world, not gating one write.** The
  value the controller adds is whole-cluster coverage of state that is *already
  live* (applied before xpcd existed, written out-of-band, or admitted while the
  webhook was fail-open). Gating is admission's job; sweeping is the
  controller's.
- **No false-positive blast radius.** A mutating or blocking sweep over the
  whole cluster could act on hundreds of objects per interval; an observe-only
  loop has no destructive failure mode. It emits only violations
  (`would-deny`/`warn`) to a signal-rich stream and updates metrics — the same
  observability-first posture as §4, applied to the standing population rather
  than the admission flow.
- **It stays read-only at the RBAC layer.** The ServiceAccount grants only
  `get`/`list` (`deploy/runtime/rbac.yaml`); there is no write path to abuse.

**R32 (live observed-vs-desired fixed-point) is the next tier to unlock here.**
It needs the live `status` subresource — the observed state — which only exists
on a running object, so it is meaningless at admission (the object has no status
yet) and unavailable to `xpc`-in-CI (no cluster). The controller's whole-cluster
capture is the first context in which R32 *could* be evaluated soundly. It is
**not** included in the current subset; promoting it is a future tier expansion,
gated by the same single-object-soundness-within-the-captured-world reasoning
the tiers in §2 use.

## Consequences

- **Single source of truth holds across the static/runtime boundary.** A change
  to R23's allowlist in `kernel/` changes both `xpc check` and `xpcd` with no
  second edit. There is no Cedar/Rego artifact to drift.
- **No false-positive denials at admission.** By excluding every
  reference-resolution / trajectory / cross-object rule from the subset, `xpcd`
  cannot deny an object because of a referent it cannot see. The cost is
  coverage: those rules only fire in CI. That is the correct trade — CI is where
  the two-Worlds and cross-repo context exists.
- **Two-layer defense, matching [INC-6](../inc-6.md).** CI catches violations
  pre-merge (static floor); `xpcd` catches whatever reaches the cluster by any
  path (runtime floor). `xpcd` complements, and does not replace, `xpc`-in-CI.
- **Admission and the controller are complementary, not redundant.** `xpcd
  serve` gates one object as it arrives (single-object tier); `xpcd watch`
  sweeps the standing population every interval and, because it captures the
  whole world first, soundly runs the wider ambient tier (§5). Admission cannot
  see state that is already live; the controller cannot block a write. Together
  they cover both the inflow and the standing set with one kernel.
- **Relationship to fg-manifold's VAP.** fg-manifold ships a
  `crossplane-state-require-orphan` ValidatingAdmissionPolicy whose kind
  allowlist R23 already mirrors ([obligations.md](../obligations.md) category
  S). `xpcd` is the first-party generalization of that single VAP: same
  fail-open admission posture, but driven by the shared kernel and covering the
  whole INC-6 floor (R23 **and** R24/R25) plus the self-contained rules, rather
  than one CEL expression per failure mode. Where the VAP and `xpcd` overlap on
  a cluster, the VAP's `policy.facilitygrid.io/allow-delete` bypass annotation
  is honored by R23 already, so the two do not contradict.
- **Bounded by construction.** Because the subset is defined by a formal
  property (single-object soundness + termination + bounded cost), adding a new
  rule to `xpcd` is a *classification* question — which tier is it? — not a
  reimplementation. A new single-object rule joins the subset by adding its code
  to the allowlist; nothing else changes.
- **The exclusion list is load-bearing.** Promoting an excluded rule into the
  subset without first making it single-object sound would reintroduce
  false-positive denials. The tiers in §2 are the gate for that promotion.
