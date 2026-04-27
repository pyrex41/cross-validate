---
date: 2026-04-26
mainline: claude/build-xpc-type-checker-TfgsT @ 0b35013
preceding handoffs:
  - thoughts/shared/orchestration/xpc-replay-v8-handoff.md
preceding replays:
  - thoughts/shared/verify/replay-results-v8.md
status: OPEN — design-block landed, implementation chunk pickup
---

# Handoff — P6 design block landed; pick the next implementation chunk

## TL;DR

Replay v8 confirmed P5 (R12 dedup + kernel-path fallback) on
fg-manifold real data: XPC012 collapsed 3504 → 12 on every tip; all
other rule counts line-for-line identical to v7. Four design docs
landed afterwards, with all 13 open decisions settled in a walkthrough.
xpc.yaml, CI GitHub integration, and P5.d secret handling each have
implementation-ready specs. A genericization roadmap captures the
multi-consumer audit so the project's tilts are visible.

The next chunk is implementation, not more design. Three candidates,
ranked. xpc.yaml is the highest-leverage pick.

## Mainline state at handoff

`claude/build-xpc-type-checker-TfgsT` @ `0b35013`. Working tree clean.
`go test ./...` last green at session-end-of-2026-04-24 (no Go changes
since; doc-only commits). 129 commits ahead of origin, unpushed,
pending PR construction.

Commits added this session, in order:

```
0b35013 docs: genericization roadmap — 3-tier audit + 4-step plan
902bd80 docs: P5.d addendum — correct "manually provisioned" framing
82a17b3 docs: design — xpc.yaml user-facing config file (P6 scope)
1bdbe1d docs: design — P5.d externally-managed secret handling for R12
69cb6be docs: design — GitHub Actions CI integration (PR-comment + release archive)
60c6469 docs: ADR-004 — P-prefix for plan-mode-only diagnostic codes
98a7109 docs: replay-v8 — correct binary SHA + note P5.e coverage
f3bd646 docs: replay-v8 — R12 dedup + kernel-path fallback validated
```

All 8 commits are docs. No source/kernel/test changes this session.

## Settled decisions (13)

Walkthrough closed all open questions surfaced by the four design
agents. Captured here so the next session doesn't relitigate.

### xpc.yaml (5 decisions, see `thoughts/shared/design/xpc-yaml-config.md`)

1. **Prod classification: substring-only.** Defer label-based matching
   until a second consumer asks. `types.ArgoApplicationSet` IR
   extension is out of scope for v1.
2. **Immutable-fields: overlay with explicit `suppress: true`.**
   Replacement-semantics rejected — silent loss of safety net.
3. **Bypass-key renaming: per-rule.** `allow-delete` and
   `allow-immutable-change` get independent `primary` + `aliases`
   blocks. Global `ignore-prefix` rejected.
4. **Top-level `version: 1` key required.** Future schema-break
   escape hatch.
5. **Canonical location: `./xpc.yaml` (repo root).** Discovery walks
   cwd-upward then exe-dir-upward (mirroring P5.c's kernel fallback).
   `.xpc/config.yaml` reserved for if/when multiple files appear.

### CI GitHub integration (5 decisions, see `thoughts/shared/design/ci-integration-github.md`)

1. **Defer plan-SARIF.** Option B (PR comment via `--format=json` + custom
   renderer) doesn't need it. Add only when a Code Scanning consumer
   asks.
2. **Action lives in this monorepo** at `.github/actions/xpc-pr-comment/`,
   `uses: <org>/cross-validate/.github/actions/xpc-pr-comment@v0.1.0`.
   Marketplace-listing deferred.
3. **Fail-day-one for `XPC.P.destructive-delete` + `XPC.P.cascade-risk`;
   comment-only-30-days for `XPC.P.immutable-change`.** Filter codes
   in the comment-posting script to set the exit code conditionally.
4. **Render-by-default in CI**, with PR-label opt-out (`xpc:fast`).
   `actions/cache` persists `~/.cache/xpc/renders/` and the helm chart
   cache. Render cost is dominated by helm subprocess + DoubleRender,
   not parsing — see `pkg/renderer/cache.go:22-77`.
5. **Sticky comment** with marker `<!-- xpc-pr-comment -->`, prior 3
   bodies preserved in a collapsed `<details>` block. Edit-not-post.

### P5.d (3 decisions, see `thoughts/shared/design/p5d-externally-managed-secrets.md` + addendum)

1. **fg-manifold has no in-house renderer.** GitLab SAST consumes SARIF
   directly. "Punt to fg-manifold" means ~30-LOC bash/python wrapper in
   `.gitlab-ci.yml` that filters known tuples before SARIF emission —
   not zero-effort, but cheap.
2. **Tuple list is stable** across replays v6→v7→v8. Static filter is
   the right shape; revisit only if drift becomes visible.
3. **fg-manifold's `deploy/` tree DOES contain ESO CRDs** (65 ExternalSecret,
   2 ClusterSecretStore, 3 PushSecret, 0 SealedSecret). The earlier "manually
   provisioned" framing was wrong — corrected in the P5.d addendum.
   Implication: option (b) (info-severity demotion via fact extraction)
   is *viable* as a future move, not blocked. Recommendation stays at
   option (c) (wrapper filter) per decision 12, but (b) is now a real
   shelved follow-up rather than an architecturally-blocked dead end.

## Pickable next chunks

Ranked by leverage. Each is mostly self-contained and produces a
reviewable diff.

### Chunk A — Implement xpc.yaml (highest leverage, ~1-2 sessions)

Spec: `thoughts/shared/design/xpc-yaml-config.md`. All 5 decisions
above are baked in.

Estimated scope (per the design doc's §4.b):
- New package: `pkg/config/` (~200 LOC: `Config`, `Load`, `Discover`,
  `Default`).
- IR extensions in `pkg/types/types.go` (~30 LOC for `World.ProdPatterns`,
  `NameCarveouts`, `BypassKeys`).
- `pkg/checker/bridge.go` serialization edits (~40 LOC).
- `cmd/xpc/main.go` flag plumbing (~40 LOC across `runCheck`/`runPlan`).
- `pkg/ir/builder.go`, `trajectory_extract.go`, `immutable_registry.go`,
  `pkg/plan/r26.go`, `r27.go` — swap hardcoded annotation literals for
  config-driven sets (~25 LOC).
- Kernel edits in `kernel/r25-prod-appset-autosync.shen` and
  `kernel/r23-crossplane-state-needs-orphan.shen` (~25 Shen LOC).
- Test fixtures under new `testdata/config/`.

**Critical invariant:** `config.Default()` must produce bit-identical
behaviour to today's compile-time defaults. Pin this with
`TestDefault_Matches_Builtin` against the actual hardcoded slices.

**Pair with chunk B'** (below) — same change-set should drop the
`policy.facilitygrid.io/allow-delete` alias from compile-time defaults
and move it to a documentation example, since xpc.yaml is now the
canonical way to add aliases.

### Chunk B — Registry per-provider split (~½ session structural; cloud packs gated by demand)

Genericization step 2 from `thoughts/shared/design/genericization-roadmap.md`.
Restructure:

```
pkg/ir/
  immutable_registry.go      → splits into:
  registries/
    aws/immutable_fields.go
    aws/state_bearing_kinds.go
    aws/selector_mappings.go
    aws/late_init_mappings.go
    generic/immutable_fields.go    # core K8s only (StatefulSet ServiceName, etc.)
    registry.go                    # Registry interface + loader
```

xpc.yaml gains `cloud-providers: [aws]` (default = AWS pack only,
matching today). Test invariant: same diagnostic output as today on
fg-manifold fixtures.

**Do this AFTER chunk A** — xpc.yaml is the natural place to thread the
provider list through. Doing it before means a one-time refactor.

### Chunk B' — Drop `policy.facilitygrid.io/` default alias (paired with chunk A)

Surgical, ~10 LOC + fixture update. Move the alias from
`pkg/ir/trajectory_extract.go:74-75` to a documentation example in
`docs/` showing how to extend bypass annotations via xpc.yaml. R23
fixture `testdata/.../bypass-alias` updates to use a non-FG-branded
alias.

### Chunk C — CI GitHub Action implementation (~1-2 sessions)

Spec: `thoughts/shared/design/ci-integration-github.md`. All 5
decisions baked in.

Components:
1. `.github/actions/xpc-pr-comment/action.yml` — composite action.
2. `.github/actions/xpc-pr-comment/post-comment.js` — Node script that
   reads `xpc plan --format=json`, renders Markdown, posts/edits a
   sticky PR comment with the prior 3 bodies in `<details>`.
3. `.github/workflows/xpc.yml` — example workflow consumers can copy.
4. **Release-archive workflow** at `.github/workflows/release.yml` —
   builds `xpc` + bundles `kernel/` into `xpc-linux-amd64.tar.gz` on
   tag push. **This is the prerequisite for the action to actually
   work on a fresh runner.**
5. Update `docs/ci-integration.md`: fix the latent `go install` bug
   at line 138 (the snippet places the binary in `$GOBIN` with no
   kernel sibling, broken per replay-v8). Replace with the release-
   archive flow for both GitLab and GitHub.

**Two real prerequisites** before this chunk lands cleanly:
- The release workflow ships *first* and produces a working v0.1.0
  archive. The action depends on a real artifact URL.
- The fix to `docs/ci-integration.md:138` should ride along — current
  state actively misleads anyone copy-pasting it.

### Chunk D — fg-manifold wrapper filter for R12 (NOT in cross-validate)

This is the option-(c) implementation. ~30 LOC of bash or python in
fg-manifold's `.gitlab-ci.yml`, wrapping `xpc plan --format=json` and
filtering the 12 known tuples before SARIF generation. Tracked
upstream as a fg-manifold ticket; cross-validate emits no code change.

The known-external-mounts list:

```yaml
# fg-manifold/.gitlab-ci.d/xpc-known-external-mounts.yaml
known-external-mounts:
  - {target: ConfigMap/pool-seed, owner: CronJob/e2e-pool-replenish}
  - {target: ConfigMap/myanon-config, owner: CronJob/migration-dump}
  - {target: ConfigMap/twilio-shim-ca, owner: Deployment/oneuptime-app}
  - {target: ConfigMap/twilio-shim-ca, owner: Deployment/oneuptime-worker}
  - {target: Secret/migration-secrets, owner: CronJob/migration-dump}
  - {target: Secret/s3-credentials, owner: CronJob/migration-dump}
  - {target: Secret/staging-db-credentials, owner: CronJob/migration-dump}
  - {target: Secret/fg-claude-bot-secrets, owner: Deployment/fg-claude-bot}
  - {target: Secret/khoj-secrets, owner: Deployment/khoj}
  - {target: Secret/usertour-secrets, owner: Deployment/usertour}
  - {target: Secret/gitlab-secrets, owner: StatefulSet/gitlab}
  - {target: Secret/gitlab-ssh-host-keys, owner: StatefulSet/gitlab}
```

## Recommendation

Pick **chunk A (xpc.yaml) + chunk B' (drop FG alias)** as the next
session. They're the right cohabitation: xpc.yaml provides the
mechanism that retires the FG alias's compile-time presence. Two-piece
change-set, single coherent commit story.

Defer chunks B, C, D until A lands. C in particular has two
prerequisites (release workflow, docs fix) that are easier to address
in a dedicated CI session.

## Known gotchas / leftover

- **`docs/ci-integration.md:138` go-install snippet is latently broken.**
  The snippet `go install github.com/.../cmd/xpc@latest` puts the binary
  in `$GOBIN` with no `kernel/` sibling — exactly the replay-v8 failure
  mode. It works today only by accident (callers happen to invoke from
  inside a checkout). Fix in chunk C.
- **129 commits ahead of origin, no PR.** Worth a thought about whether
  to construct a PR now (against what base — the original `main`?) or
  keep stacking. The handoff is a natural pause point.
- **Replay v9 trigger:** material behaviour change to a rule's
  emission counts. xpc.yaml landing should *not* change counts on the
  default-config-absent path (it's the load-bearing invariant). If
  counts do change, that's the v9 trigger.
- **P5.d follow-up triggers (per the corrected addendum):**
  - 12-tuple list grows past ~30 across the fleet.
  - Second downstream consumer beyond fg-manifold appears.
  - A sibling rule wants the same ExternalSecret facts (e.g.
    "ExternalSecret namespace mismatch", "SecretStore scoping").
  Any one of these flips P5.d.4/.5 into "schedule the fact extractor."

## What this handoff doesn't cover

- **Plan-SARIF.** Decision 6 deferred this until a Code Scanning
  consumer asks. Not in any chunk.
- **Stack alternatives** (Flux, Terraform, Pulumi). Out of scope until
  a non-ArgoCD consumer drives requirements.
- **Push to origin / PR construction.** A separate decision for the
  user, not coupled to any chunk.
