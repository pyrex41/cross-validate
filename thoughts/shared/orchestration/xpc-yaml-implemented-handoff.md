---
date: 2026-04-27
mainline: claude/build-xpc-type-checker-TfgsT (working tree dirty, no new commits)
preceding handoffs:
  - thoughts/shared/orchestration/p6-design-block-landed-handoff.md
status: GATE CLEARED — replay v9 completed with fg-manifold xpc.yaml; counts
        stayed flat against replay v8
---

# Handoff — xpc.yaml + chunk B' implemented; replay v9 is next

## TL;DR

Chunk A (xpc.yaml) and chunk B' (drop `policy.facilitygrid.io/` from
compile-time defaults) both landed in this session. `go test ./...` green;
`go vet ./...` clean. Code is uncommitted on the working tree.

2026-04-27 pickup: option (b) was executed. A root `xpc.yaml` was added to
the local fg-manifold checkout re-registering the FG-branded allow-delete
alias, then replay v9 was run. Counts stayed line-for-line flat against
replay v8 for both single-tip `check` and the three plan-mode pairs.
Report: `thoughts/shared/verify/replay-results-v9.md`.

The replay-v9 trigger named in the prior handoff DID fire — chunk B' is a
deliberate behaviour change on fg-manifold's default path (R23/R26 will now
fire on any resource that bypassed only via the FG-branded annotation
without a co-located xpc.yaml). That gate is now cleared by the fg-manifold
`xpc.yaml`; the remaining requirement is to actually land that file upstream
before deploying this cross-validate change.

After replay v9 settles, the next pickable chunks (B, C) from
`p6-design-block-landed-handoff.md` are still relevant — nothing about
that ranking changed.

## Mainline state at handoff

`claude/build-xpc-type-checker-TfgsT` HEAD = `36739fe` (the prior
handoff's commit; nothing new committed since).

Working tree dirty:

```
modified:
  cmd/xpc/main.go
  docs/obligations.md
  kernel/check.shen
  kernel/r23-crossplane-state-needs-orphan.shen
  kernel/r25-prod-appset-autosync.shen
  pkg/checker/bridge.go
  pkg/checker/check_test.go
  pkg/ir/builder.go
  pkg/ir/trajectory_extract.go
  pkg/plan/r26.go
  pkg/plan/r27.go
  pkg/plan/r27_test.go
  pkg/plan/runner.go
  pkg/types/types.go
  testdata/fixtures/crossplane-state-needs-orphan/bypass-alias/cluster.yaml

untracked:
  pkg/config/                                       (5 files; ~750 LOC)
  testdata/fixtures/crossplane-state-needs-orphan/bypass-alias/xpc.yaml
  testdata/fixtures/crossplane-state-needs-orphan/xpc-yaml-extends-carveouts/  (2 files)
  testdata/fixtures/prod-appset-autosync/xpc-yaml-extends-patterns/            (2 files)
```

132 commits ahead of origin; still no PR.

## What landed

### Chunk A — `xpc.yaml`

New package `pkg/config/`:

- `config.go` — typed schema (`Config`, `ProdPatternsConfig`,
  `ImmutableFieldEntry`, `BypassAnnotationsConfig`, `BypassKeyConfig`,
  `NameCarveoutsConfig`).
- `defaults.go` — `Default()` returning the pre-xpc.yaml hardcoded values.
- `resolve.go` — per-knob resolvers with documented merge-semantics
  (`ResolveProdPatterns`, `ResolveAllowDeleteKeys`,
  `ResolveAllowImmutableChangeKeys`, `ResolveCrossplaneStateNeedsOrphanCarveouts`,
  `ResolveImmutableFields`).
- `load.go` — `Parse(bytes) (*Config, error)` + `Load(path)`. Strict-decode
  on nested keys (yaml.v3 KnownFields), warn-on-unknown top-level keys,
  reject `version != 1`, validate GVK/paths.
- `discover.go` — `Discover(start)` (cwd-upward + exe-dir fallback,
  mirroring `pkg/checker`'s kernel-path resolver), `LoadIfPresent(dir)`
  (non-walking, for hermetic test fixtures), `Resolve(flag, env, start)`
  (combined precedence helper).
- `config_test.go` — `TestDefault_Matches_Builtin` is the safety-net
  asserting absent-config ≡ old hardcoded path; plus tests for parse
  errors, version-mismatch, all four merge-semantic shapes, and discovery.

`pkg/types/types.go` extensions:

- `World.ProdPatterns []string`
- `World.NameCarveouts map[string][]string`
- `World.BypassKeys BypassKeySet` (with `AllowDelete`, `AllowImmutableChange`
  slices and `Has` / `HasRaw` helpers, plus `BypassSlot` enum).

`pkg/ir/builder.go`:
- `Builder.Config *config.Config` (nil → `Default()`).
- `Build()` resolves all four knobs onto `World` BEFORE `EnrichTrajectoryData`.

`pkg/ir/trajectory_extract.go`:
- Stops overwriting `w.ImmutableFields`.
- New `ensureKnobDefaults()` fallback for direct callers (test code that
  constructs a World inline without going through `Builder.Build`).
- Bypass-collapse in `extractCPDeletionPolicyFacts` reads `w.BypassKeys`.

`pkg/plan/r26.go`, `r27.go`:
- Signatures changed:
  - `R26DestructiveDelete(delta, bypassKeys)`
  - `R27ImmutableChange(delta, registry, bypassKeys)`
- Fix-hint strings now render the actual primary key the binary recognizes
  (so the user sees the right annotation when xpc.yaml has remapped it).
- `r27_test.go` updated to pass `nil` registry + zero `BypassKeySet` for the
  Added-only smoke test.

`pkg/plan/runner.go`:
- `Config.ConfigOverride *config.Config` field for explicit `--config`.
- `resolveVariantConfig(override, variantDir)` — per-variant in-repo
  discovery (HEAD wins per design §3.c) with explicit override fast-path.

`pkg/checker/bridge.go`:
- Two new sections in `worldToShenObj`:
  - `(prod-patterns "-prod" "prod-" ...)`
  - `(crossplane-state-needs-orphan-carveouts "alb-logs" ...)`
- New helper `stringListSection(tag, items)` shared between both.

`kernel/`:
- `check.shen` extracts the two new sections and threads them as args.
- `r25-prod-appset-autosync.shen`: `r25-name-matches-any?` folds over the
  list (no more hardcoded `-prod`/`prod-`); `check-r25` and `r25-check-row`
  now take a `Patterns` arg.
- `r23-crossplane-state-needs-orphan.shen`: `r23-name-carved-out?` folds
  over the list (no more hardcoded `"alb-logs"`); `check-r23` and
  `r23-check-row` now take a `Carveouts` arg.

CLI (`cmd/xpc/main.go`):
- `--config=<path>` and `XPC_CONFIG_PATH` env on both `xpc check` and `xpc plan`.
- One stderr info line if discovery falls back to the exe-dir
  (`info: xpc.yaml discovered via exe-dir fallback at …`) — same shape
  the design §3.c calls out for the kernel-path fallback.
- `printUsage()` strings updated.

### Chunk B' — drop FG default alias

- `pkg/config/defaults.go`: `defaultAllowDeleteAliases` shrank to nil.
  Comment block in-file documents the move.
- `testdata/fixtures/crossplane-state-needs-orphan/bypass-alias/`:
  - `cluster.yaml` annotation flipped to `mycorp.example.com/allow-delete`.
  - new `xpc.yaml` registers the alias via
    `bypass-annotations.allow-delete.aliases`.
- `pkg/checker/check_test.go`: `loadFixture` now calls
  `config.LoadIfPresent(path)` so a fixture-local xpc.yaml applies.
  Non-walking — fixtures stay isolated.
- `docs/obligations.md`: R23 entry rephrased to describe the alias as
  user-registerable via xpc.yaml.

### End-to-end smoke fixtures (kernel-side proof)

Two new fixtures + matching test cases prove the kernel actually consumes
the new sections (not just the Go side):

- `testdata/fixtures/prod-appset-autosync/xpc-yaml-extends-patterns/`
  — replaces default `{-prod, prod-}` with `{-staging}`; the same
  staging-named AppSet that `nonprod-ok` keeps silent now trips R25.
- `testdata/fixtures/crossplane-state-needs-orphan/xpc-yaml-extends-carveouts/`
  — adds a `scratch` substring carve-out so a `scratch-throwaway-cluster`
  Cluster that would otherwise trip R23 stays silent.

## Test status

`go test -count=1 ./...` is fully green. `go vet ./...` clean. Highlights:

```
ok    pkg/checker    6.078s
ok    pkg/config     1.652s
ok    pkg/ir         2.060s
ok    pkg/plan       4.148s
```

## Replay v9 trigger — load-bearing

Chunk B' is a deliberate behaviour change on the default-config path. The
prior handoff said:

> **Replay v9 trigger:** material behaviour change to a rule's emission
> counts. xpc.yaml landing should *not* change counts on the
> default-config-absent path (it's the load-bearing invariant). If counts
> do change, that's the v9 trigger.

Chunk A by itself preserves the invariant — `Default()` is bit-identical
to the prior compile-time path, asserted by `TestDefault_Matches_Builtin`.
Chunk B' breaks the invariant *intentionally*: on fg-manifold without a
co-located xpc.yaml, every resource that bypassed only via
`policy.facilitygrid.io/allow-delete: "true"` will now trip R23 (and
potentially R26 in plan-mode).

**Two ways to handle this** before the next replay:

1. **Land an xpc.yaml in fg-manifold** that re-registers the alias:
   ```yaml
   version: 1
   bypass-annotations:
     allow-delete:
       aliases:
         - "policy.facilitygrid.io/allow-delete"
   ```
   Counts stay flat. Replay v9 is then a sanity check, not a
   semantic-change validation.

2. **Run replay v9 first, eyeball the new fires.** They're the resources
   that *should* migrate to the `xpc.io/`-branded primary anyway. Use the
   replay output to drive a fg-manifold migration MR.

Recommendation: option (1). The fg-manifold `xpc.yaml` is a one-paragraph
change and lets the existing alias keep working without surprise; the
migration to `xpc.io/allow-delete` then happens on a calmer schedule.

## Pickable next chunks

Same ranking as the prior handoff, with one prerequisite added at the top:

### Chunk 0 — Replay v9 + fg-manifold xpc.yaml (gating, ~½ session)

The chunk-B' default change has to settle before any further surgery on
this code path. Concrete sub-steps:

1. Commit the current working-tree changes (single squash or two commits,
   one per chunk).
2. Build a binary with the kernel bundled (release-archive shape; or just
   pin the kernel path explicitly for the replay run).
3. Run `xpc check` against fg-manifold's `deploy/` tree. Compare against
   replay v8.
4. If counts diverge: write the fg-manifold xpc.yaml registering the
   alias, re-run, expect line-for-line match with v8.
5. Write up `thoughts/shared/verify/replay-results-v9.md`.

### Chunk B — Registry per-provider split (~½ session)

Genericization step 2 from `thoughts/shared/design/genericization-roadmap.md`.
Now that xpc.yaml exists, threading the provider list through it is the
natural shape. Unchanged from the prior handoff.

### Chunk C — CI GitHub Action implementation (~1-2 sessions)

Spec: `thoughts/shared/design/ci-integration-github.md`. Two prereqs
before the chunk lands cleanly:

- Release workflow ships first and produces a working v0.1.0 archive.
- `docs/ci-integration.md:138` go-install bug fix rides along.

Unchanged from prior handoff.

### Chunk D — fg-manifold wrapper filter for R12

Out of cross-validate; tracked upstream. Unchanged.

## Settled in this session

- xpc.yaml is `bypass-annotations.allow-delete.aliases` (additive),
  `bypass-annotations.allow-delete.primary` (replace-on-non-empty). No
  rule-namespace prefix needed in v1.
- Loader rejects unknown nested keys (strict yaml.v3 KnownFields), warns
  on unknown top-level keys (forward-compat across binary versions).
- Config discovery starts from CWD for `xpc check` and from the variant
  worktree for `xpc plan`. HEAD-side wins per the design's §3.c.
- Fixture loading is non-walking via `config.LoadIfPresent(dir)` — a
  fixture's xpc.yaml only applies to that fixture, never leaks from a
  parent dir. This was the right call for hermetic test isolation; the
  CLI uses the walking `Discover` instead.
- R26/R27 take explicit `BypassKeySet` and `[]ImmutableField` args rather
  than reading from a `*World`. Clean signatures, no global state.

## Known gotchas / leftover

- **129 → 132 commits ahead of origin, still no PR.** This is now the
  third handoff to call this out. Worth a deliberate decision: PR the
  whole stack against `main`, or keep stacking until P6 is feature-frozen.
- **Plan-mode R27 immutable-field overlay has no end-to-end smoke
  fixture** comparable to the prod-patterns / carve-outs ones. The
  existing `plan-immutable-change*` fixtures still pass, which proves the
  default path; what's NOT pinned is "user supplies a new GVK in
  xpc.yaml and R27 catches it." Worth adding when chunk 0 settles, low
  priority because the resolver itself has unit-test coverage in
  `pkg/config/config_test.go`.
- **`docs/ci-integration.md:138` go-install snippet** still latently
  broken; flagged in the prior handoff as a chunk-C item.
- **No xpc.yaml exists yet at the cross-validate repo root.** None
  needed — defaults are correct for self-hosted use. Worth dropping a
  one-line example file once chunk C lands so `xpc check` against this
  repo demonstrates the canonical layout.

## What this handoff doesn't cover

- **Plan-SARIF.** Still deferred (decision 6).
- **Stack alternatives** (Flux, Terraform, Pulumi). Out of scope.
- **`xpc proof` interaction with config.** Not exercised yet — the proof
  digest covers the IR but not the loaded xpc.yaml. Probably fine
  (xpc.yaml is the policy not the data being verified) but worth a
  pass before chunk C.
