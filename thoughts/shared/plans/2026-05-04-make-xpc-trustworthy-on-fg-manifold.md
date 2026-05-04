---
title: Make xpc produce a trustworthy report on fg-manifold (pre-CI work)
date: 2026-05-04
author: Reuben / Claude
status: proposed
inputs:
  - thoughts/shared/research/2026-05-04-xpc-usefulness-for-fg-manifold.md
  - 1,341-finding empirical run against fg-manifold@b364bf674f
scope:
  - In: kernel-bootstrap reliability, two specific high-FP rules, signal audit on a third, focused-mode flag
  - Out: GitLab CI integration, performance work, in-tree adoption inside fg-manifold
---

## Goal

Today `xpc check` against `~/fg/fg-manifold/deploy/facilitygrid/` produces 1,341
findings (1,324 errors). The verified-real subset (R23/R24/R25 — INC-6 floor;
selected XPC.D regressions matching recent fix commits) is buried under
probable false positives in two rules. CI integration is blocked on
trustworthy output. This plan delivers that — and nothing else.

**Definition of done.** A clean run on fg-manifold `main` produces:

- Zero `XPC000` infrastructure errors across concurrent invocations.
- Zero XPC.D findings in AppProjects whose whitelists are absent in YAML.
  Real findings (where a whitelist is explicit and missing a kind) remain.
- Zero XPC006 findings for Function/Composition pairs in the same App at the
  same default wave. Cross-App and explicit-wave-conflict cases remain.
- A documented signal/noise verdict on XPC.E.selector-needs-ignore-diff
  (440 findings today). Either: confirmed precision, or fix that drops volume.
- A `--focus=inc6-floor` (or equivalent config) that runs only R23+R24+R25
  (and R26 in plan-mode) for a known-clean-by-construction baseline.

CI integration is a follow-up plan, not part of this one.

## Phase 1 — Kernel-bootstrap reliability (`XPC000` race)

### Symptom

```
XPC000 publish kernel dir:
  rename /var/folders/.../xpc-kernel-stage-... /var/folders/.../xpc-kernel-847be1907e34cb17:
  file exists
```

Reproduces in this repo when:
- A previous run left a partial publish dir (test killed mid-stage), or
- Two concurrent invocations race through `resolveOrMaterialiseKernel`.

### Root cause

`pkg/checker/bridge.go:128–187` — `resolveOrMaterialiseKernel`:

1. Compute content digest, build `dir = $TMPDIR/xpc-kernel-<digest16>`.
2. If `dir/.xpc-kernel-digest` exists and matches → reuse.
3. Otherwise: `MkdirTemp` a staging dir, write all files + marker.
4. `os.Rename(staging, dir)` — atomic publish.
5. On rename error, fallback (line 181): `Stat(dir/check.shen)`. If it exists,
   succeed. Otherwise return XPC000.

Two flaws in the fallback:

- **Stat checks `check.shen`, not `.xpc-kernel-digest`.** A partially-published
  dir (e.g., killed mid-stage previously) can have `check.shen` without the
  marker, or vice versa.
- **No recovery for genuinely corrupt destination.** If `dir` exists but is
  incomplete, the run dies instead of overwriting.

### Fix — file-level publish

Replace the dir-rename with file-level renames into a `MkdirAll`'d destination.
Each individual file rename is atomic; the marker file is the success signal.

```go
// pseudocode
if err := os.MkdirAll(dir, 0o700); err != nil { return "", err }

// Stage in a sibling temp dir on the same filesystem.
staging, _ := os.MkdirTemp(filepath.Dir(dir), ".xpc-kernel-stage-")
defer os.RemoveAll(staging)

// Write files to staging.
for _, f := range files { os.WriteFile(filepath.Join(staging, f.name), f.data, 0o600) }

// Atomic rename each file into dir. If another process already published
// this exact byte-equal content, EEXIST is fine (rename overwrites on POSIX).
for _, f := range files {
    src := filepath.Join(staging, f.name)
    dst := filepath.Join(dir, f.name)
    if err := os.Rename(src, dst); err != nil {
        return "", fmt.Errorf("publish %s: %w", f.name, err)
    }
}

// Write marker LAST. Reading the marker is the success signal — if a competing
// process is mid-publish, our marker write either wins or loses harmlessly.
return dir, os.WriteFile(filepath.Join(dir, ".xpc-kernel-digest"), []byte(digest), 0o600)
```

Then strengthen the up-front fast path: if the marker exists with matching
digest, the dir is good — even if a competing process is mid-publish (file
renames are atomic; the worst case is reading a file the other process is
about to overwrite with byte-identical content).

### Acceptance test

New test `TestResolveOrMaterialiseKernel_Concurrent` in `pkg/checker/`:
spawn 16 goroutines calling the function simultaneously into a `t.TempDir()`-
overridden temp root. All must return success and return the same path.

### Files touched
- `pkg/checker/bridge.go:117–187`
- `pkg/checker/bridge_test.go` (new test)

### Estimated effort
2–3 hours including the test.

---

## Phase 2 — XPC.D.kind-whitelisted FP fix (~600 of 701 findings)

### Symptom

Sample finding:
```
XPC.D.kind-whitelisted ERROR
kind ConfigMap (group core) not in AppProject ops whitelist
```

Verified in `~/fg/fg-manifold/deploy/facilitygrid/ops/applicationsets/appproject-ops.yaml`:

```yaml
spec:
  clusterResourceWhitelist:
    - group: "*"
      kind: "*"
  # no namespaceResourceWhitelist — absent in YAML
```

ArgoCD's behavior: **whitelist absent → permit-all**. Empty array (`[]`)
is the explicit deny-all. xpc treats both identically as deny-all.

### Root cause

Three-layer plumbing collapses absent and empty into the same value:

- `pkg/types/types.go:469, 473` — `ClusterResourceWhitelist []ArgoGroupKind`
  (no way to express absence).
- `pkg/ir/builder.go:1335–1338` — `parseGroupKindList(spec, key)` returns
  `nil` when the key is missing OR when the value is empty. Both indistinguishable.
- `pkg/checker/bridge.go:758–774` — `argoAppProjectToObj` `range`s over the
  slice; nil and empty produce the same empty list to the kernel.
- `kernel/r15-appproject-whitelist.shen:24` — `r15-whitelisted? G K [] -> false`
  treats empty as deny-all (correct for actual `[]`, wrong for absent).

### Fix — preserve nil vs empty through to the kernel

**Three-line change strategy.** Capture absence at parse time, signal it to
the kernel with a synthetic permit-all entry.

1. **`pkg/ir/builder.go`** — modify `parseGroupKindList` (or the call sites at
   1335–1338) to distinguish:
   - YAML key absent → return a sentinel value (proposal: `nil`).
   - YAML key present but empty `[]` → return `[]ArgoGroupKind{}` (non-nil
     empty).
   - YAML key present and populated → return the parsed slice.

2. **`pkg/checker/bridge.go:758–774`** — in `argoAppProjectToObj`, branch on
   `nil` vs non-nil:
   ```go
   if proj.ClusterResourceWhitelist == nil {
       // Absent in YAML → ArgoCD permits all cluster-scoped kinds.
       cwl = []kl.Obj{argoGroupKindToObj(types.ArgoGroupKind{Group: "*", Kind: "*"})}
   } else {
       for _, gk := range proj.ClusterResourceWhitelist {
           cwl = append(cwl, argoGroupKindToObj(gk))
       }
       // Non-nil empty slice → explicit deny-all, leave cwl as nil/empty.
   }
   ```
   Same for `NamespaceResourceWhitelist`.

3. **No kernel changes required.** Existing R15 wildcard handling
   (`kernel/r15-appproject-whitelist.shen:17–19`) will accept the synthetic
   permit-all entry.

### Acceptance test

- New fixture `testdata/fixtures/appproject-whitelist-absent/` — AppProject
  with `clusterResourceWhitelist: ["*"/"*"]` only, plus an Application
  producing namespaced ConfigMap. **Must produce zero XPC.D findings.**
- Existing fixture `appproject-whitelist-miss` — explicit whitelist missing
  a kind. **Must continue to fire R15.**
- New fixture `appproject-whitelist-empty` — explicit empty
  `namespaceResourceWhitelist: []` plus a namespaced resource. **Must fire R15**
  (validates the deny-all path).
- Re-run against fg-manifold: verify XPC.D drops from 701 to <100. The
  remaining set should concentrate in projects with explicit whitelists
  (`preview`, `prod`).

### Watch-out

`parseGroupKindList` may already round-trip nil for missing keys. Verify
before assuming changes there. The Go convention to "treat nil and empty
identically" means downstream code may rely on it. Audit callers before
flipping the contract.

### Files touched
- `pkg/ir/builder.go:1335–1338` (and `parseGroupKindList` if needed)
- `pkg/checker/bridge.go:758–774`
- `testdata/fixtures/appproject-whitelist-absent/` (new)
- `testdata/fixtures/appproject-whitelist-empty/` (new)
- Existing tests

### Estimated effort
4–6 hours.

---

## Phase 3 — XPC006 same-AppSet same-wave FP fix (~30 findings)

### Symptom

Sample finding:
```
XPC006 ERROR
Function function-go-templating (wave 0) must have a lower sync-wave than
Composition fargateapp-preview (wave 0)
```

Both objects live in `crossplane-platform.yaml` AppSet. Neither has an
explicit `argocd.argoproj.io/sync-wave` annotation, so both default to 0.
ArgoCD applies same-wave-same-app resources in a single sync transaction;
Crossplane reconciles eventually. The team's working pattern is fine.

### Root cause

`kernel/r6-wave-ordering.shen:84–104` — `check-r6b-for-composition`:

```shen
(let CompWave (find-wave "Composition" CompName SyncWaves)
     FnRefs   (extract-fn-refs Pipeline)
  (if (< FnWave CompWave) ...))
```

`find-wave` returns 0 when no entry exists (line 140: `_ _ [] -> 0`).
Both default-0 → `(< 0 0)` is false → emit error.

### Fix — three options, pick one

**Option A (preferred): suppress same-app default-0.** In
`check-r6b-for-composition`, suppress emission when both Function and
Composition resolve to wave 0 *and* neither was explicitly waved.

This requires `SyncWaves` to carry annotation provenance — an extra
"explicit?" field per entry, sourced from `pkg/ir/`. The kernel rule then
only fires error when at least one side has an explicit wave.

**Option B: scope R6b to cross-App pairs only.** Functions and Compositions
in the *same* Application are deployed atomically; the rule's value is at
the cross-App boundary where the Function's AppSet must come up before
the Composition's AppSet.

`check-r6-app` (line 36) already filters by `OwningApp`. Currently both
filtered sets belong to the same App by construction. Reverse the framing:
emit only when the Function and Composition are *not* owned by the same App.

This is the simpler kernel change, but loses coverage of intra-App ordering
hazards (which are real but currently not the team's problem because Crossplane
reconciles).

**Option C: severity downgrade for same-app default-0.** Same as A but
emit `info` instead of `error`. Preserves the warning without breaking CI.

### Recommendation

Start with **Option A** — it's the most precise. Adds an "explicit" bit to
the `(sync-wave Kind Name Wave)` fact tuple → `(sync-wave Kind Name Wave Explicit)`.
Go-side, `pkg/ir/argo_extract.go` (or wherever wave annotations are read)
populates Explicit=true when the annotation is present, false on default.

Apply the same fix to R6a (XRD before XR) and R6d (Composition <= XR) for
consistency — they have the same default-0 issue.

### Acceptance test

- New fixture `testdata/fixtures/wave-default-same-app/` — Function and
  Composition in the same App, neither annotated. **Must produce zero R6
  findings.**
- Existing fixture `wave-ordering` (explicit-wave conflict) **must continue
  to fire.**
- New fixture `wave-default-cross-app` — Function in App A (wave 2),
  Composition in App B (wave 7), neither resource-level-annotated. **Must
  produce zero R6 findings** (the AppSet-level wave handles it).
- New fixture `wave-default-cross-app-bad` — same as above but App ordering
  reversed. **Must fire R6.**
- Re-run against fg-manifold: verify XPC006 drops from 30 to <5.

### Files touched
- `pkg/types/types.go` — extend the `SyncWave`/`Wave` struct with `Explicit bool`
- `pkg/ir/builder.go` or `pkg/ir/argo_extract.go` — set Explicit on parse
- `pkg/checker/bridge.go` — emit the 4-arity tuple
- `kernel/r6-wave-ordering.shen` — consume Explicit; suppress when neither side explicit
- `kernel/r6c-provider-wave.shen` — same treatment
- `kernel/prelude.shen` — update `(sync-wave …)` fact-schema doc
- New fixtures + tests

### Estimated effort
1–2 days. The kernel changes are small; the Go-side wiring is the bulk.

### Decision gate

Before starting Phase 3, decide whether Option A or Option B. **Option A is
recommended; Option B is acceptable and 4× cheaper.** If the team has no
intent to ever rely on intra-App resource-level wave ordering (Crossplane's
reconciliation makes this a non-issue), Option B is the right ROI call.

---

## Phase 4 — XPC.E.selector-needs-ignore-diff signal audit (440 findings)

### Premise

This is the second-largest finding class and tracks the second-largest
fg-manifold MR pain category from the April 18 study. It is NOT presumed to
be FP-heavy, but volume is high enough to gate trust until verified.

### Audit procedure

1. Sample 25 findings stratified across the apps that own them (top 5 apps
   by finding count, 5 findings each).
2. For each, open the source YAML and the owning ApplicationSet's
   `ignoreDifferences` block. Record:
   - **Real** — selector resolves to a path not covered by any `ignoreDifferences` entry.
   - **FP — wildcard** — `ignoreDifferences` has a `group: "*" kind: "*"` entry
     covering the path that xpc isn't recognizing.
   - **FP — Observe-only** — resource has `managementPolicies: [Observe]`,
     selector resolution doesn't apply.
   - **FP — other** — note specifically.
3. Compute FP rate. If <10%, declare R16 trustworthy. If 10–30%, narrow the
   rule to handle the dominant FP category. If >30%, treat as Phase 4a/4b
   work.

### Likely fix shapes (if FPs found)

- **Wildcard ignoreDifferences not crossed-with-selectors.** R16 may be
  matching `ignoreDifferences` entries by exact `group/kind`, missing
  wildcard entries that genuinely cover the resource. Fix in
  `kernel/r16-selector-needs-ignore-diff.shen` to honor `group: "*"` /
  `kind: "*"` matching.
- **`managementPolicies: [Observe]` exempts.** Add Go-side enrichment that
  marks selector-bearing resources with their effective managementPolicies;
  rule skips Observe-only resources. (R22 territory but cross-cutting.)

### Files (if action needed)
- `kernel/r16-selector-needs-ignore-diff.shen`
- `pkg/ir/trajectory_extract.go` (selector-usage facts)
- New fixtures

### Estimated effort
1 day audit + 1–2 days fix if needed.

---

## Phase 5 — `--focus=inc6-floor` mode

### Premise

Even after Phases 1–4, the report will be hundreds of findings (because R23/R16
have real backlog). To make xpc land for the team without first paying off the
backlog, ship a focused-mode flag that runs only the rules with verified-low
FP rate and verified-high-blast-radius coverage.

### Design

Add a `--focus=<preset>` flag to `xpc check`:

- `--focus=inc6-floor` runs **only** R23 (state-needs-orphan), R24
  (appset-finalizer-without-preserve), R25 (prod-appset-autosync). Optional
  add-on: R26 (destructive-delete) when invoked via `xpc plan`.
- `--focus=all` (default) is today's behavior.
- Future presets can ship as new ADRs.

Implementation: a small allowlist consulted in `pkg/checker/checker.go`
(or wherever rule dispatch lives) before `kl.Call(check-world, …)`. Pass
the allowlist down to the kernel as a `(rule-allowlist …)` fact and have
`check.shen` skip rules not on the allowlist.

### Acceptance

`xpc check --focus=inc6-floor deploy/facilitygrid/` against fg-manifold
produces only XPC.S, XPC.E.appset-finalizer-without-preserve, and
XPC.E.prod-appset-autosync findings. Total ~94 findings (69 + 23 + 2).

### Files touched
- `cmd/xpc/main.go` — flag plumbing
- `pkg/checker/checker.go` — allowlist propagation
- `kernel/check.shen` — allowlist gate

### Estimated effort
4–6 hours.

---

## Sequencing

| Phase | Effort      | Blocks              |
|-------|-------------|---------------------|
| 1 — kernel race        | 0.5 day  | All future use; CI integration |
| 2 — XPC.D nil-vs-empty | 1 day    | Trust on D rule      |
| 3 — XPC006 same-wave   | 1–2 days | Trust on 006         |
| 4 — XPC.E audit        | 1 day    | Trust on E selectors |
| 5 — focus flag         | 0.5 day  | (independent)        |

Phases 1, 2, 3, 5 are independent and can ship in any order. Phase 4 is an
audit and may collapse to a no-op if R16 turns out to be already precise.

Total: **3.5–5 working days** of one engineer.

## Out of scope (intentionally)

- **GitLab CI integration.** Lands in a follow-up plan once Phases 1–3 are
  green and either Phase 4 audit clears R16 or the team accepts a focus-flag
  CI gate.
- **Performance.** The 7m38s wall on a full fg-manifold render is workable for
  CI, terrible for editor/pre-commit. Out of scope here; revisit if pre-commit
  use becomes a goal.
- **Vendoring xpc into fg-manifold vs. installing the published binary.** A
  team-side decision; doesn't affect this plan.
- **R15 in-tree adoption beyond fg-manifold.** Other repos may have different
  AppProject conventions; this plan handles fg-manifold's correctly.

## Open questions

1. **Phase 3 Option A vs B.** Decide before starting. Option A is more
   precise; Option B is 4× cheaper and matches how Crossplane actually
   handles intra-App reconciliation. Recommend Option B unless the
   precision is wanted later.
2. **Phase 4 may not be needed.** If the 440 XPC.E findings hold up under
   audit, jump straight to Phase 5 + CI integration follow-up.
3. **Whether to land Phase 5 (`--focus=inc6-floor`) before Phase 2.**
   Doing Phase 5 first gives the team an immediately-actionable signal
   while Phase 2 is in flight. Recommend: yes — Phase 5 then 1, 2, 3, 4.
