# Rules and Invariants

This section documents the invariants that `xpc` checks over Crossplane and
Argo CD configuration.

`xpc` treats a repository as a typed world of facts:

1. YAML, Helm, Kustomize, ApplicationSet, snapshot, and optional render output
   are normalized into `pkg/types.World`.
2. Go enrichment extracts facts that are awkward or expensive to derive in the
   rule language: ownership, sync waves, trajectory state, RBAC edges,
   selector/late-init usages, field-validation failures, state-bearing
   deletion-policy facts, and precomputed high-cardinality rule violations.
3. The kernel dispatches each invariant and returns diagnostics with source
   locations and fix hints.

The intent is practical pre-merge checking, not a complete Kubernetes model.
Every rule has an explicit modeled surface and should fail only when the tool
has enough source information to point at a concrete manifest.

## Pages

- [TLA+ vs Shen](tla-plus-vs-shen.md) explains where TLA+ fits, where Shen
  fits, and why the production checker should remain a Go-indexed fact checker
  rather than become a TLA+ model checker.
- [Rule Catalog](catalog.md) describes every shipped rule/invariant, including
  inputs, invariant statement, risk, diagnostic code, and usual fix.

## Rule Families

| Family | Codes | Purpose |
| --- | --- | --- |
| Version and schema | `XPC001`, `XPC005`, `XPC008`, `XPC.A.*` | Catch invalid CRD/XRD versions, bad patch types, Crossplane v1/v2 field placement, and manifest/schema mismatches. |
| References | `XPC003`, `XPC004`, `XPC009` | Ensure Crossplane references resolve before runtime. |
| Argo ordering and ownership | `XPC006`, `XPC007`, `XPC012`, `XPC014`, `XPC.D.*` | Enforce sync-wave dependencies, tracking-mode safety, trajectory invariants, and AppProject constraints. |
| Drift suppression | `XPC.E.selector-needs-ignore-diff`, `XPC.E.late-init-needs-ignore-diff` | Catch fields Crossplane writes back into `spec` unless Argo ignores them. |
| Rendering | `XPC.H.*` | Make renderer coverage explicit and catch chart/schema/determinism problems. |
| State preservation | `XPC.S.*`, `XPC.P.*`, `XPC.E.appset-*`, `XPC.E.prod-*` | Prevent INC-6-shape destructive cascades and immutable/state-bearing changes. |

## Source of Truth

The executable rule dispatch lives in [`kernel/check.shen`](../../kernel/check.shen).
Many expensive joins are intentionally precomputed in Go before reaching the
kernel; the rule catalogue documents the invariant, not the implementation
accident of which side performs a lookup.

For the bounded obligation taxonomy, see [`docs/obligations.md`](../obligations.md).
For the historical decision to use Shen as the executable rulebook, see
[`docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md`](../adr/002-shen-as-canonical-spec-and-trajectory-simulator.md).
