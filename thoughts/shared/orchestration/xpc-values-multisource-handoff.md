---
date: 2026-04-22
mainline: claude/build-xpc-type-checker-TfgsT @ f2eb18e
preceding handoffs:
  - thoughts/shared/orchestration/xpc-fg-manifold-handoff.md
  - thoughts/shared/orchestration/xpc-post-parallel-handoff.md
preceding replays:
  - thoughts/shared/verify/replay-results-v3.md
status: OPEN — two tracks for the next agent (A cheap, B real code)
---

# xpc ApplicationSet Helm-values + Argo multi-source `$values` handoff

## TL;DR

Followup #14 (Helm field substitution in AppSet expander) shipped at `f2eb18e`. Replay-v4 against the same three fg-manifold tips used by replay-v3 confirms the placeholder substitution IS landing, but the `XPC.H.helm-renders` count stayed at 34/tip because the underlying Helm error moved from **"unresolved `{{provider}}` placeholder"** to **"unresolved Argo `$values` multi-source reference"**. Those are two different bugs. The next agent owns:

- **Track A** — file the replay-v4 result + open followup #16 so the ledger reflects what's next (cheap, docs-only).
- **Track B** — implement `$values` multi-source resolution in xpc so `helm template` stops seeing `$values/...` as a literal path (real code change, highest-ROI remaining helm-renders move).

Both should land on `claude/build-xpc-type-checker-TfgsT`. Track A can ship in one commit; Track B is a multi-commit vertical slice (types → loader → renderer → fixture+test).

## Mainline state at handoff

`claude/build-xpc-type-checker-TfgsT` @ `f2eb18e`, working tree clean, `make test` + `make lint` green.

Commit layered on top of the prior post-parallel handoff:

```
f2eb18e ir: substitute Helm fields during AppSet expansion (followup #14)
09ebfd7 docs: add Shen cn-arity gotcha to the ledger                      ← pre-handoff
8011f65 docs: close post-parallel handoff — R22 + #13 both landed         ← pre-handoff
```

Pre-existing followups #3 (XPC006 cartesian) and #15 (missing `lib/charts/crossplane-*`) got tick-bookkeeping in the same commit `f2eb18e`. #15 is **still pending your `ls`** on the fg-manifold tips — if you haven't run this, do it before Track A:

```bash
for tip in 441fb679a 2ca71f228 4dd584566; do
  git -C /Users/reuben/fg/fg-manifold checkout -q $tip
  echo "=== $tip ==="; ls /Users/reuben/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/lib/charts/ 2>&1 | head
done
git -C /Users/reuben/fg/fg-manifold checkout -q feat/e2e-otel-enable-autoload
```

## What replay-v4 showed (the key evidence)

xpc binary `/tmp/xpc-v4` built from `f2eb18e`; raw outputs at `/tmp/v4-cold-{441fb679a,2ca71f228,4dd584566}.{json,stderr,time}`.

| code                                 |   v3 |   v4 | delta | interpretation |
|--------------------------------------|-----:|-----:|------:|----------------|
| `XPC.D.kind-whitelisted`             |  700 |  700 |     0 | stable |
| `XPC.E.selector-needs-ignore-diff`   |  374 |  435 |   +61 | **new signal** — dedup loosened once AppSet-expanded Apps diverge in their Helm fields; 61 per-element selectors that collapsed together in v3 now surface individually |
| `XPC.H.appset-unsupported-generator` |   41 |   41 |     0 | stable |
| `XPC.H.helm-renders`                 |   34 |   34 |     0 | **root cause shifted** — see below |
| `XPC006`                             |   30 |   30 |     0 | stable |
| `XPC.E.late-init-needs-ignore-diff`  |   12 |   12 |     0 | stable |
| **total**                            | 1191 | 1252 |   +61 |  |

### Helm-renders root-cause shift (the critical finding)

A representative v3 failure detail read (per `replay-results-v3.md` line 105):

```
open <chart>/$values/.../{{provider}}/{{region}}/{{cluster}}/values.yaml: no such file or directory
```

The v4 failures for the same 22/35 sources read:

```
Error: open /Users/reuben/.cache/xpc-v4-helm/charts/<hash>/$values/deploy/facilitygrid/ops/applications/crossplane/aws/us-east-2/facilitygrid-ops/values.yaml: no such file or directory
```

`aws/us-east-2/facilitygrid-ops` is the resolved `{{provider}}/{{region}}/{{cluster}}` — so `f2eb18e` did its job. What's left is the literal `$values/...` prefix that `helm template` cannot interpret.

The other 13/35 are the same `lib/charts/crossplane-*` "path not found" errors as v3 — unchanged, that's followup #15 (fg-manifold repo-state, not xpc).

### Why the count didn't drop

helm still returns non-zero on each source, so xpc still emits one `XPC.H.helm-renders` diagnostic per source. The diagnostic's `Detail` field changed (from placeholder-leak to `$values`-leak) but the count is gated on exit status, not detail shape. Expected: once Track B lands, 22/34 should vanish and the count should fall to ~12 (the 13 local-chart errors minus one I might be double-counting; recount on replay-v5).

---

## Track A — file replay-v4 + followup #16 (cheap, 30 min, docs-only)

### Files to create / modify

1. `thoughts/shared/verify/replay-results-v4.md` — mirror v3's shape. Use the table above. Key sections:
   - Frontmatter (date 2026-04-22, binary `/tmp/xpc-v4` from `f2eb18e`, predecessor `replay-results-v3.md`).
   - TL;DR describing the root-cause shift (not a regression, a re-diagnosis).
   - Run matrix — just 3 cold runs this time, times already captured at `/tmp/v4-cold-*.time`. Skip warm because we already know the warm speedup is ~3× (replay-v3 measured it) and the helm-renders count doesn't depend on warmth.
   - Rule breakdown: the table above.
   - Why `XPC.E.selector-needs-ignore-diff` went 374→435: list-generator synthetic Apps that previously collapsed into one selector-registry row (because their Helm sources were byte-identical placeholder-leaking strings) now diverge per param-set, so the registry walker sees them as distinct rows.
   - Sample v3→v4 error detail comparison (copy the two lines above into a fenced block).
   - Reproducing: exact `for tip in …; do /tmp/xpc-v4 check --kernel-path=$PWD/kernel --helm-cache-dir=~/.cache/xpc-v4-helm --appset-fixture=/tmp/fg-manifold-pr-fixture.yaml --format=json /Users/reuben/fg/fg-manifold/deploy/ > …; done`.

2. `thoughts/shared/orchestration/xpc-fg-manifold-handoff.md` followup ledger — append:

   ```
   16. **Argo `$values` multi-source resolution** — new, surfaces from replay-v4. `ArgoSource` has no `Ref` field and the renderer passes `source.helm.valueFiles` paths starting with `$values/...` straight to `helm template`, which reads them as literal filesystem paths and fails. Scope: type + loader + renderer, plus fixture. Expected replay-v5 delta: `XPC.H.helm-renders` 34 → ~12 (22 `$values` failures resolve; 13 local-chart "not found" are followup #15).
   ```

3. Commit as `docs: replay-v4 + followup #16 (Argo $values multi-source)`. Use a single commit; artifacts at `/tmp/v4-cold-*` are ephemeral, do NOT commit those.

### Optional during Track A

If the `ls` on fg-manifold tips for #15 confirms the paths are genuinely absent, close #15 in the same commit with the one-liner already drafted in `xpc-fg-manifold-handoff.md`.

---

## Track B — implement `$values` multi-source resolution (real code, vertical slice)

### Background: how Argo multi-source works

An Argo CD Application with multiple sources can have one source carry `ref: values`, then reference that source's path in sibling sources' `valueFiles` using `$<refName>/...`:

```yaml
spec:
  sources:
  - repoURL: https://example.com/charts
    chart: tailscale-operator
    targetRevision: 1.89.3
    helm:
      valueFiles:
      - $values/deploy/facilitygrid/ops/applications/tailscale-operator/aws/us-east-2/facilitygrid-ops/values.yaml
  - repoURL: https://gitlab.com/fg/fg-manifold
    targetRevision: main
    ref: values
```

When Argo renders, it resolves `$values/...` to `<checked-out-values-repo>/...`. xpc currently does neither the parse nor the resolve.

### Files to modify (call order)

1. **`pkg/types/types.go:257-276`** — add `Ref string \`json:"ref,omitempty"\`` to `ArgoSource`. Place it near `RepoURL`/`Path` for locality.

2. **Loader** — grep for where ArgoSource is populated from YAML. Likely candidates (confirm before editing): `pkg/ir/builder.go` (look for struct-literal construction or a `decodeSource` helper) or whatever decodes `spec.sources[].helm`. The field name `ref` maps directly; if the decoder uses yaml.v3 with json tags, nothing to change beyond the type. If it uses an anonymous-struct decoder, add the field there too.

3. **`pkg/renderer/helm.go` around L120-132 and L302-318 (mergeValuesBytes)** — this is where the fix lives. Current flow:
   - `helmSrc.ValueFiles` is passed into `mergeValuesBytes(chartPath, valueFiles, ...)` which joins each `f` against `chartPath` and reads.
   - Need to add: before joining, scan each `f`; if it starts with `$<name>/`, look up the sibling source in the Application's `Sources` slice that has `Ref == name`, resolve that source to its on-disk path (same ResolveChart path — remote sources get pulled the same way, but they can also be pure git repos without a chart; see below), and substitute `$<name>` with that path.

4. **Renderer signature needs access to sibling sources.** Today `Renderer.Render` takes a single source. You'll likely need to extend the signature (or pass an "application context") so the helm renderer can look up `$values` refs. Two options:
   - (a) Add `Siblings []types.ArgoSource` to the render inputs. Smallest surface, but every caller adapts.
   - (b) Resolve `$values` up in the builder (`pkg/ir/builder.go` around where `ResolveChart` is called) and rewrite `valueFiles` to absolute paths before the renderer ever sees them. Keeps the renderer dumb; arguably the cleaner boundary.
   - **Recommend (b).** The renderer already treats `ResolveChart` as "owner decides how to materialize paths"; `$values` resolution belongs on the same layer. Grep `ResolveChart` callers to find the hook.

5. **Values-repo source materialization** — if the `ref: values` sibling has `RepoURL` + `TargetRevision` pointing at the same repo the Application lives in, the on-disk path is just the fg-manifold workload repo (the `/Users/reuben/fg/fg-manifold/deploy/` root passed as the CLI arg). If it points at a different repo, we'd need to `git clone` it, which is out of scope for the first cut; emit an info-level diagnostic (`XPC.H.values-source-remote`) and skip. For the 22 fg-manifold failures this handoff targets, the `ref: values` source points back at fg-manifold's own repo, so local resolution is enough.

6. **Fixture**: `testdata/fixtures/helm-values-ref/`:
   - `app.yaml` with two sources — one Helm chart source, one `ref: values` source pointing at a local directory with a `values.yaml` under `deploy/.../values.yaml`.
   - The sibling `values.yaml` on disk actually exists, with at least one override that a minimal chart templates against.
   - Expected: R18 is **silent** for this fixture (render succeeds).
   - Sibling negative case: `testdata/fixtures/helm-values-ref-missing/` — same shape but the referenced `values.yaml` on disk is absent. Expected: R18 fires, diagnostic detail includes the resolved absolute path so the MR author can debug.

7. **Test**: `TestR18_HelmRenders_ValuesRefResolved` in the appropriate `_test.go` under `pkg/checker/`.

### Reusable surfaces (do not reinvent)

- `pkg/renderer/renderer.go:42` `ResolveChart` — existing relative-to-appfile path resolution; extend this concept.
- `pkg/ir/appset_expand.go:substituteTemplate` — already walks `{{ .key }}` tokens. Don't need it here (`$values` isn't a Go-template placeholder), but if you find yourself writing a token walker, use this for inspiration.
- `pkg/renderer/subprocessErrTail` — the stderr-propagation helper from followup #13. If your new error emit needs a stderr tail, reuse; don't re-roll.

### Gotchas carried forward

- **Bridge booleans**: if any new discriminator needs to go through Shen, emit lowercase-dashed symbols (`values-ref-resolved` / `values-ref-remote` / `values-ref-missing`), never `true`/`false`. See gotcha ledger in `xpc-fg-manifold-handoff.md`.
- **Shen `cn` is strictly 2-argument** — doesn't apply here (no kernel change planned) but if the fix grows a kernel rule, nest pairwise.
- **`make lint` baseline**: `internal/shenfull/*` + a handful of pre-existing files are expected failures. Only regressions in files touched by Track B are real.
- **fg-manifold working tree**: replay-v4 left `/Users/reuben/fg/fg-manifold` on `feat/e2e-otel-enable-autoload`. If you run replay-v5, use the same tip loop and checkout back when done.
- **Chart-pull cache key**: if you change how `valueFiles` gets computed, double-check `pkg/renderer/cache.go:27` doesn't silently drift — `valueFiles` already participates in the cache hash, so substituted paths will produce new keys (expected, but worth a sanity check).

### Verification

1. `make test` — existing R18 tests stay green, new `TestR18_HelmRenders_ValuesRefResolved` passes.
2. `make lint` — no regressions beyond baseline.
3. **Replay-v5** on the same 3 fg-manifold tips using the v4 protocol. Expected rule delta:

   | code | v4 | v5 expected |
   |---|---:|---:|
   | `XPC.H.helm-renders` | 34 | ~12 (22 `$values` cases resolve; 13 local-chart "not found" remain until #15 is addressed fg-manifold-side) |
   | others | | unchanged |

   If `XPC.H.helm-renders` doesn't drop to ≤15/tip, the fix isn't doing what we think; read one of the remaining failure details and re-diagnose before declaring victory.

4. Write `thoughts/shared/verify/replay-results-v5.md` mirroring v4's shape.

5. Tick followup #16 in `xpc-fg-manifold-handoff.md` and cite the commit SHA + replay-v5 numbers.

## Key file locations (at f2eb18e)

- `pkg/ir/appset_expand.go:293-398` — `substituteSource` + `substituteHelm` (the #14 fix; **do not revert**; use as an example of cloning instead of mutating input)
- `pkg/ir/appset_expand_test.go:TestExpandAppSet_SubstitutesHelmValueFiles` + `TestExpandAppSet_HelmSubstitutionDoesNotMutateTemplate` — regression tests for #14
- `pkg/types/types.go:257-276` — `ArgoSource` (extend here for Track B)
- `pkg/renderer/helm.go:120-132`, `L302-318` — `mergeValuesBytes` (extend or hook upstream of here)
- `pkg/renderer/renderer.go:42` — `ResolveChart` reference pattern
- `pkg/ir/builder.go` — caller of `ResolveChart`; best hook point for option (b) in Track B
- `thoughts/shared/verify/replay-results-v3.md` — baseline for v4 comparison
- `thoughts/shared/orchestration/xpc-fg-manifold-handoff.md` — the main running-state ledger; both Tracks end by ticking an entry there
- `/tmp/v4-cold-*.{json,stderr,time}` — raw replay-v4 artifacts (ephemeral; reference, don't commit)

## What NOT to do

- Don't revert `f2eb18e`. The substitution is real, the count just isn't moving because of a different layer of the same problem.
- Don't try to solve the `lib/charts/crossplane-*` failures (#15) from the xpc side — exploration confirmed xpc isn't stripping a prefix. That's fg-manifold's problem.
- Don't clone remote `ref: values` repos in Track B's first cut. Ship local-only resolution + an info diagnostic for remote, then revisit if fg-manifold has a case that needs it.
- Don't commit `/tmp/v4-*` artifacts. The replay-results-v5.md doc cites them; raw files stay ephemeral.
