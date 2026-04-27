---
title: Genericization roadmap — from fg-manifold-only to multi-consumer
date: 2026-04-26
status: living roadmap (re-read when a second consumer surfaces)
related:
  - thoughts/shared/design/xpc-yaml-config.md (step 1 of the roadmap)
  - thoughts/shared/design/p5d-externally-managed-secrets.md (option-c rationale)
  - docs/adr/001-bounded-obligation-taxonomy.md
  - docs/adr/002-shen-as-canonical-spec-and-trajectory-simulator.md
  - docs/adr/003-appset-expansion.md
  - docs/adr/004-p-prefix-diagnostic-codes.md
---

## Why this exists

Every design conversation in 2026-04 has implicitly assumed a single
consumer (fg-manifold). That assumption is invisible until a second
consumer shows up and the project has to triage which constraints were
load-bearing vs. which were defaults of convenience. This doc captures
the audit so future-us doesn't have to rebuild it from cold context.

The audit is structured around three tiers: what's already generic,
what becomes generic once xpc.yaml ships, and what stays
hardcoded-by-construction even after xpc.yaml.

## Tier 1: already generic (~95% of the engine)

These are provider-agnostic and ship as-is to any ArgoCD-shaped
consumer:

- **Two-layer architecture.** Go IR builder (`pkg/ir/`) feeds a Shen
  kernel (`kernel/*.shen`). Adding a rule changes one Shen file plus
  optionally one Go fact extractor. ADR-002 pinned this.
- **Trajectory model.** Sync-wave semantics, `ResourceInfo`-keyed
  state, the per-step World projection. ArgoCD-generic, not
  fg-manifold-specific.
- **Plan-mode** (`pkg/plan/`). `ResourceDelta` over base/head Worlds,
  the `XPC.P.*` taxonomy (ADR-004). Generic.
- **Diagnostic taxonomy.** A–L obligation categories (ADR-001),
  `XPC.<letter>.<name>` code shape, severity tiers, bypass-annotation
  contract.
- **AppSet expansion** (ADR-003). The four offline-expandable generators
  (`list`, `git-directories`, `matrix`, `merge`) work for any ArgoCD
  user; remote-API generators (`pullRequest`, `scmProvider`) take the
  generic `--appset-fixture` flag.
- **Render cache.** SHA-256 keyed two-tier cache at
  `~/.cache/xpc/renders/` (`pkg/renderer/cache.go:22-77`). Provider-
  agnostic.
- **Kernel-path fallback** (P5.c, `pkg/checker/bridge.go:113-161`).
  Generic deployment ergonomics.
- **Output formats.** SARIF, JUnit, JSON, markdown, agent, human, lsp.
  Universal.

Action item: none. These are working assets that ship cleanly.

## Tier 2: tilted defaults that xpc.yaml moves to flexible (~25% of the surface)

These are currently fg-manifold-flavored compile-time defaults. The
xpc.yaml design (`thoughts/shared/design/xpc-yaml-config.md`) makes
each user-overridable. After xpc.yaml lands, this tier is "tilted
defaults" but no longer "limits."

| Knob | Tilt today | xpc.yaml answer | File:line today |
|------|-----------|-----------------|------------------|
| R25 prod patterns | substring `-prod`/`prod-` | `prod-patterns:` block (replacement) | `kernel/r25-prod-appset-autosync.shen:32-36` |
| R23 name carve-out | hardcoded `"alb-logs"` | `name-carveouts:` block (additive) | `kernel/r23-crossplane-state-needs-orphan.shen:41-42` |
| Immutable-field registry | ~30 entries, AWS-flavored | `immutable-fields:` block (overlay + suppress) | `pkg/ir/immutable_registry.go:19-99` |
| Bypass annotation keys | `xpc.io/`, `policy.facilitygrid.io/` | `bypass-annotations:` per-rule block | `pkg/plan/r26.go:106-128`, `pkg/plan/r27.go:73-92` |

The most overtly fg-manifold-branded artifact in the codebase is the
`policy.facilitygrid.io/allow-delete` alias, present in the bypass
extractor (`pkg/ir/trajectory_extract.go:74-75`). Once xpc.yaml lands
and bypass-keys move to per-rule config, that alias should be removed
from defaults and live only as a documentation example.

Action items (tracked separately): land xpc.yaml per the design doc;
delete the `policy.facilitygrid.io/` aliases from compile-time defaults
in the same change-set.

## Tier 3: hardcoded by construction (~5%, plus the registry-contents lift)

Even after xpc.yaml ships, three classes of constraint stay:

### 3.a Registry contents are AWS-only

xpc.yaml lets users *add* immutable-field entries, selector-mappings,
late-init-mappings, and state-bearing kinds. It doesn't *populate* a
useful default for non-AWS clouds. An Azure or GCP user gets a working
tool with empty cloud coverage and has to populate every entry by hand
to get any cloud-side correctness checks.

Files affected:
- `pkg/ir/immutable_registry.go` (~30 AWS entries)
- `pkg/ir/state_bearing_kinds.go` (Crossplane MR kinds, AWS-flavored)
- `pkg/ir/selector_mappings.go` and similar (~50 entries each,
  provider-coupled)
- `pkg/ir/late_init_mappings.go` (~50 entries)

Genericization shape: split each registry into per-provider packs and
load them by config:

```
pkg/ir/registries/
  aws/
    immutable_fields.go
    selector_mappings.go
    state_bearing_kinds.go
    late_init_mappings.go
  azure/
    ...
  gcp/
    ...
  generic/
    immutable_fields.go      # core K8s only (StatefulSet ServiceName, etc.)
```

xpc.yaml grows a `cloud-providers: [aws, azure]` key that gates which
packs load. Authoring an azure pack from scratch is ~1-2 weeks of work
given the AWS pack as a model. The architectural lift is small (a
`Registry` interface that each pack implements); the labor is
researching which Azure CRD fields are actually immutable.

Effort: ~1-2 weeks per cloud. Trigger: a non-AWS consumer.

### 3.b Stack assumption: ArgoCD + Crossplane + Helm + Kustomize

Several rules are deeply coupled to specific deployment-stack
components:

- R15/R16/R20 — assume ArgoCD `Application` and `ApplicationSet` shapes.
- R17/R18 — assume Helm and Kustomize as the rendering layers.
- R23/R26 — assume Crossplane state-bearing MRs with `deletionPolicy`
  semantics.
- R25 — assumes ArgoCD `ApplicationSet` with `spec.template.spec.syncPolicy.automated`.

A Flux user gets ~half the rules; a pure-Pulumi or pure-Terraform user
gets ~none of them. The trajectory model itself is ArgoCD-shaped (sync
waves, hook annotations, the Application-as-fact extraction).

Genericization shape — three sub-options:

1. **Add Flux as a parallel trajectory source.** Flux's `Kustomization`
   and `HelmRelease` resources are conceptually similar to ArgoCD's
   `Application`. New IR extractor (`pkg/ir/flux_extract.go`), Flux-
   specific rule equivalents for R15/R20/R26. ~2 weeks.
2. **Add Terraform/Pulumi as state-bearing alternatives.** Conceptually
   different — Terraform state lives in a backend, not in cluster CRDs.
   The trajectory model would have to grow a "state file" concept.
   Architectural lift, not labor lift. ~4-6 weeks.
3. **Drop trajectory altogether and go pure-static.** Some users only
   want kind-whitelisting and selector validation, no trajectory. A
   `--no-trajectory` mode that skips R15/R16/R20/R23/R25/R26. ~1 week.

Recommendation: do option 1 (Flux) only when a Flux user shows up.
Option 3 is cheap insurance — could ship with the xpc.yaml landing as
a `mode: static-only` config knob if anyone asks.

Effort: 1-6 weeks depending on which sub-option. Trigger: a non-ArgoCD
consumer.

### 3.c Per-consumer residue (genuinely non-genericizable)

The 12 R12 externally-managed tuples and the option-(c) wrapper filter
are fg-manifold-specific by construction. Every consumer will have
their own residue. The shape of the solution generalizes (a config-
driven filter list); the contents don't. xpc.yaml's bypass-annotation
mechanism partially addresses this for the rules that have a bypass
key; for R12, which doesn't have a per-mount bypass annotation, the
wrapper-filter pattern is the durable answer.

If R12 grows a bypass annotation (say `xpc.io/allow-dangling-mount` on
the Pod-spec controller), this whole class of residue disappears from
the wrapper into the manifests, where it belongs. Worth considering as
a P5.d follow-up.

Effort: minimal if added as a bypass-annotation following the existing
R26/R27 pattern. ~50 LOC. Trigger: the wrapper filter becomes a real
maintenance burden.

## The 4-step roadmap

Numbered in dependency order — earlier steps unblock later ones:

1. **Land xpc.yaml** per `thoughts/shared/design/xpc-yaml-config.md`.
   ~350 Go LOC + 25 Shen LOC. Decisions 1–5 already settled
   (substring-only prod, overlay+suppress immutable, per-rule bypass
   rename, `version: 1` required, repo-root location).
   Effort: ~1 session of focused implementation, ~½ session of test
   plumbing. **Highest leverage by far** — moves Tier 2 from "limits"
   to "defaults."

2. **Split registry contents into per-provider packs.** New
   `pkg/ir/registries/` directory, AWS extracted into `aws/`, generic
   K8s into `generic/`. xpc.yaml gains `cloud-providers:` key. No
   change to default behaviour for fg-manifold (it's all-AWS today).
   Effort: ~½ session for the structural split + tests; ~1-2 weeks
   per additional cloud pack to populate. Ship the structural split
   alone first; populate clouds on demand.

3. **(Conditional) Drop the `policy.facilitygrid.io/` defaults.** Tied
   to step 1. Once xpc.yaml is the canonical way to add bypass-key
   aliases, the FG-branded alias should not be a compile-time default.
   Move it to a documentation example. ~10 LOC change + a test fixture
   update.

4. **(Conditional) Stack alternatives.** Pure-static mode (cheap),
   then Flux (medium), then Terraform/Pulumi (architectural lift).
   Driven by actual consumer requests, not speculatively.

Steps 1 and 3 should land in the same chunk so the bypass-annotation
config story is coherent. Step 2 can land independently. Step 4 should
not be touched without a real consumer driving requirements.

## What this doesn't address

- **`xpc.io/` namespace.** Generic-enough — `.io` is widely used by
  upstream tools (kubernetes.io, argoproj.io). Not a tilt worth
  removing.
- **CLI naming (`xpc check`, `xpc plan`, `xpc snapshot`).** Generic.
- **The choice to make this an offline checker rather than a live-
  cluster checker.** Architectural decision, not a tilt — out of scope
  for genericization.
- **The Shen kernel as the spec language.** ADR-002 made this choice
  on its merits. Not a fg-manifold-specific decision.

## When to re-read this doc

- A second consumer beyond fg-manifold appears.
- Someone proposes a "let's open-source this" milestone.
- Annual "what does this project owe its users" review.
- Before a quarterly planning chunk that wants to include "improve
  external usability."
