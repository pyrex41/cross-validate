# Design: `xpc.yaml` — user-facing config file

Status: draft, P6 scope.
Author notes: research-only, no implementation.
Related: `thoughts/shared/verify/replay-results-v8.md` (v7→v8 kernel-stability
baseline); `thoughts/shared/research/2026-04-18-fg-manifold-target-study.md`
(external-user friction log); `thoughts/shared/plans/2026-04-18-fg-manifold-coverage.md`
(P6 roadmap).

## 1. Current hardcoded surfaces

Each of the three knobs in scope is implemented in exactly one or two places
today. The code is uniformly commented with a pointer to "kernel config file
is a P1/P6 follow-up," so the shape of the extraction is already well-scoped.

### 1.a Prod-pattern detection (R25)

Rule R25 (`XPC.E.prod-appset-autosync`) is the only rule that classifies an
ArgoCD object as "prod" vs. "non-prod". The detection is substring-based on
the ApplicationSet `metadata.name` and happens entirely inside the kernel:

- `/Users/reuben/projects/cross-validate/kernel/r25-prod-appset-autosync.shen:32-36` —
  `r25-is-prod-name?` returns true when the name contains either `-prod` or
  `prod-`. Substrings were chosen over suffixes/prefixes specifically to
  avoid over-matching `prodrome-staging` (see the comment block at
  lines 16-23).
- `/Users/reuben/projects/cross-validate/kernel/r25-prod-appset-autosync.shen:58-64` —
  `r25-check-row` fires only when `r25-is-prod-name?` AND the fact carries
  the `auto-yes` discriminator (i.e. `spec.template.spec.syncPolicy.automated`
  present).

The fact itself is built in Go and carries no label/selector payload — R25
literally cannot see labels today:

- `/Users/reuben/projects/cross-validate/pkg/checker/bridge.go:693-704` —
  `appSetAutosyncToObj` emits `(appset-autosync-fact Name AutoSym Src)`.
  `types.ArgoApplicationSet` (`pkg/types/types.go:515-530`) has no `Labels`
  field, so any label-based prod rule would require an IR change too.

The header explain string at `cmd/xpc/main.go:1228-1230` advertises the
limitation explicitly: "Name patterns are currently hardcoded. A kernel
config file (xpc.yaml) that surfaces prodAppSetNamePatterns is a P1
follow-up."

No other rule does prod/non-prod classification today. If the user extends
the notion of prod to cover regular Argo Applications (as opposed to
AppSets), that's a genuine new code path — no existing rule will pick it up
for free.

### 1.b Immutable-field registry (R27)

The registry is a single pure Go function and the ~30 entries it returns are
consumed in exactly one place (R27 in plan-mode). R13 in the kernel is
retired (see `kernel/r13-no-immutable-change.shen:1-5`).

- `/Users/reuben/projects/cross-validate/pkg/ir/immutable_registry.go:19-99` —
  `ImmutableFieldRegistry()` returns the hardcoded slice. Entries are
  grouped by comments: core K8s, RDS, DocDB, S3, KMS, EC2/VPC, mysql.sql.
  Each entry is a `types.ImmutableField{Group, Kind, FieldPath, Reason}`.
- `/Users/reuben/projects/cross-validate/pkg/ir/trajectory_extract.go:41` —
  `w.ImmutableFields = ImmutableFieldRegistry()` copies the slice onto the
  World during IR enrichment.
- `/Users/reuben/projects/cross-validate/pkg/plan/r27.go:31-71` —
  `R27ImmutableChange` reads the registry directly (not from the World),
  iterates `delta.Modified`, and emits `XPC.P.immutable-change` per
  `(Group, Kind, FieldPath)` hit.
- `/Users/reuben/projects/cross-validate/pkg/checker/bridge.go:427` +
  `781-786` — the kernel also gets the registry serialized as
  `immutable-field-fact` tuples, but only because R13 used to consume them.
  After P4.d, `check.shen` never dispatches those facts to a live rule.

Scope caveat from the file comment (`immutable_registry.go:9-14`): only
scalar-leaf paths are supported. Arrays and object-block paths are a P5
concern, so an external `xpc.yaml` entry of `spec.forProvider.settings` (a
block) silently wouldn't fire — the config schema should either reject
block paths up front or document the limitation.

### 1.c Bypass-annotation keys

Every rule hardcodes annotation-string literals at its consumption site;
there is no central table. Load-bearing bypass keys (kernel actually
branches on them):

- `xpc.io/allow-delete` (primary) + `policy.facilitygrid.io/allow-delete`
  (alias) — R23 (check) and R26 (plan):
  - `pkg/ir/trajectory_extract.go:74-75` collapses both to the kernel's
    `bypass-yes` symbol before R23 sees it.
  - `pkg/plan/r26.go:106-128` (`hasBypassAnnotation`).
  - `kernel/r23-crossplane-state-needs-orphan.shen:25-27, 77` — prose
    only; the Shen rule matches the pre-collapsed symbol, not the
    annotation string.
- `xpc.io/allow-immutable-change` — R27: `pkg/plan/r27.go:73-92`
  (`hasImmutableChangeBypass`).

Diagnostic-text-only (annotation string appears only in Fix hints, no
actual branch on it today): `xpc.dev/accept-conversion-webhook` (R2),
`xpc.dev/accept-bootstrap-gap` (R9), `xpc.dev/declassify` (R10). These
can ride along with P6 but are not load-bearing.

### 1.d Adjacent knobs found during the audit

Sit next to the three in scope and would land naturally in `xpc.yaml`:

- R23 name carve-out `"alb-logs"` —
  `kernel/r23-crossplane-state-needs-orphan.shen:41-42`. Substring match.
- State-bearing kind registry —
  `pkg/ir/state_bearing_kinds.go` / `StateBearingKindsRegistry()`. P6
  should decide whether this is user-extensible or a kernel invariant.
- Selector-mapping / late-init-mapping registries (R16/R21, ~50 entries
  each, provider-coupled). Recommend leaving out of `xpc.yaml` v1.

## 2. Proposed `xpc.yaml` schema

Top-level keys are one per subsystem. Every section is optional; absent
keys fall back to the hardcoded defaults in §1. YAML is chosen over TOML
for consistency with the Kubernetes/ArgoCD surface the tool already reads.

```yaml
# xpc.yaml — user-extensible type-checker config.
# Every section is optional. Absent sections fall back to built-in defaults.

prod-patterns:
  # R25 (XPC.E.prod-appset-autosync) matcher. If this block is present and
  # non-empty, it REPLACES the built-in ["-prod", "prod-"] defaults.
  # To extend rather than replace, include the defaults explicitly.
  appset-name-substrings:
    - "-prod"
    - "prod-"
    - "-production-"      # example extension

immutable-fields:
  # R27 (XPC.P.immutable-change). Entries APPEND to the built-in registry;
  # duplicates (same Group+Kind+FieldPath) are deduped with user entry winning
  # on the Reason string. To suppress a built-in, set `suppress: true`.
  - gvk: apps/v1/StatefulSet
    paths:
      - spec.serviceName
    reason: "StatefulSet ServiceName is immutable after create"
  - gvk: mycorp.example.com/v1alpha1/Widget
    paths:
      - spec.forProvider.widgetId
    reason: "Widget ID is the external identity"
  - gvk: s3.aws.upbound.io/v1beta1/Bucket
    paths: [spec.forProvider.region]
    suppress: true        # opt out of the built-in region-immutability check

bypass-annotations:
  # Renames / extends the keys the bypass extractors recognize. Each logical
  # bypass has a primary key and zero-or-more aliases, matching today's
  # allow-delete shape. If `primary` is present it REPLACES the built-in;
  # aliases are additive.
  allow-delete:
    primary: "xpc.io/allow-delete"
    aliases:
      - "policy.facilitygrid.io/allow-delete"
      - "mycorp.example.com/allow-delete"
  allow-immutable-change:
    primary: "xpc.io/allow-immutable-change"
  # NB: accept-conversion-webhook, accept-bootstrap-gap, declassify are
  # diagnostic-text-only today — deferred until they become load-bearing.

name-carveouts:
  # R23 name carve-outs. Substring match, same semantics as the built-in
  # "alb-logs" check. Purely additive.
  crossplane-state-needs-orphan:
    - "alb-logs"
    - "temp-"
```

Notes on what was intentionally NOT added:

- No `prod-patterns.label-selectors` — `types.ArgoApplicationSet` has no
  `Labels` field today (`pkg/types/types.go:515-530`). Adding label-based
  matching is a separate IR extension, and is called out in §6.
- No `prod-patterns.name-suffixes` etc. as distinct from substrings —
  R25's comment explicitly picked substring matching to handle both
  `-prod` and `prod-` uniformly. Matching that in the schema keeps the
  mental model flat.
- No per-rule enable/disable list. That's a separate "rule registry"
  concern that doesn't belong in the same file.

## 3. Loader contract

### 3.a Location & precedence

Two resolution modes, checked in order:

1. `--config=<path>` flag (present on `xpc check` and `xpc plan`). An
   explicit path is honored verbatim — any error (file missing, YAML
   malformed, unknown keys) is fatal.
2. Implicit discovery: walk upwards from CWD looking for `xpc.yaml`, then
   — if not found and an xpc binary resolved via `os.Executable` —
   repeat the walk from `filepath.Dir(exe)`. This is the exact same
   pattern `pkg/checker/bridge.go:113-161` uses for `kernel/`, with the
   `P5.c` fallback. Reusing that shape keeps `xpc plan` in a temp
   worktree working: the plan temp dir has neither `xpc.yaml` nor
   `kernel/`, but the binary's install dir does.
3. Absent everywhere: load the built-in defaults and continue silently.
   This preserves backwards-compat for every existing user.

`XPC_CONFIG_PATH` env var mirrors `XPC_KERNEL_PATH` (`cmd/xpc/main.go:149,
654`). Precedence: `--config` > `XPC_CONFIG_PATH` > discovery > defaults.

### 3.b Error handling

- File exists but unreadable → fatal, exit 1, stderr "cannot read xpc.yaml
  at …: <err>".
- YAML parse error → fatal, exit 1, with line:col from `gopkg.in/yaml.v3`
  (the loader package already uses that module).
- Unknown top-level key → warning, continue. (Forward-compat: a newer
  `xpc.yaml` should not blow up an older binary.)
- Unknown key inside a known section → fatal. (Schema-within-section is
  stable per binary version.)
- Semantic errors (e.g. `gvk` not parseable as `group/version/Kind`,
  `primary: ""`) → fatal, with the YAML line number of the offending
  entry.

### 3.c Interaction with `xpc plan` temp worktrees

`pkg/plan/` clones the two refs into temp directories under `os.TempDir()`
and runs the checker against those trees. Two distinct questions:

1. Should `xpc.yaml` be read from the repo being checked, or from the
   invocation CWD?
2. If from the repo, BASE and HEAD may disagree. Which wins?

Recommendation: **from the repo being checked, HEAD wins**. Rationale:
`xpc.yaml` is a policy file; "this commit's view of policy" is the
reasonable mental model. A PR that adds a new immutable-field entry
should have that entry enforced on the PR's own diff. For `xpc check`
(no variants), the same walk applies against CWD, which is the repo. For
`xpc plan`, the walk targets the HEAD worktree specifically.

The executable-dir fallback stays as a last resort for dev/test
ergonomics but should emit a single info-level stderr line when it fires
("xpc.yaml discovered via exe-dir fallback at …") — silent fallback is the
sharp edge that made P5.c hard to debug.

### 3.d Caching

Config loads once per `xpc check` / `xpc plan` invocation — the Shen
runtime init (`initShen`, `bridge.go:48-108`) is already behind a
`sync.Once`, so the natural home for config-load is the first section of
that same path. No hot-reload.

## 4. Plumbing: config values reach the kernel as IR facts

### 4.a Choice: option (a) — Go injects values into the Shen world as facts

The other two options and why they lose:

- Option (b), pre-built Shen list generated from `xpc.yaml`: would require
  teaching `xpc.yaml` → Shen codegen, and the kernel would have to
  `(load "xpc-config.shen")` which breaks kernel-path portability and
  adds a build-time step. Also: malformed user YAML becomes a Shen
  parse error, which is unhelpful.
- Option (c), kernel stays static, Go filters diagnostics post-hoc: works
  for prod-patterns and bypass keys (filter-in, filter-out), but fails
  for immutable-fields because the kernel never emits the diagnostic R27
  produces — R27 runs in Go. It also means the Shen kernel permanently
  disagrees with the user-facing behavior, which is hostile to
  `xpc proof` consumers (proofs would include judgments that don't
  survive filtering).

Option (a) is already the model the codebase uses for every other
"registry" (`ImmutableFields`, `SelectorMappings`, `StateBearingKinds`
etc. all ride on `*types.World` and get serialized through
`pkg/checker/bridge.go`). Extending that is mechanical.

### 4.b Files that change (rough sizing)

New code:

- `pkg/config/` (new package): `Config` struct, `Load(path string) (*Config, error)`,
  `Discover() (string, error)` (cwd walk + exe-dir fallback), `Default()`
  (returns the struct populated from the current hardcoded defaults).
  ~200 LOC + tests.
- `pkg/types/types.go`: add `World.ProdPatterns []string`,
  `World.NameCarveouts map[string][]string`, `World.BypassKeys
  BypassKeySet`. `ImmutableFields` already exists, so that one is
  append-only at load time. ~30 LOC.

Edits:

- `pkg/checker/bridge.go`:
  - `worldToShenObj`: emit two new sections `prod-patterns` and
    `name-carveouts` (the bypass-keys never reach the kernel as data —
    see below). ~20 LOC.
  - New converters `prodPatternToObj`, `nameCarveoutToObj`. ~20 LOC.
- `cmd/xpc/main.go`: parse `--config`, read `XPC_CONFIG_PATH`, call
  `config.Load`, thread the result into `ir.Builder` (for bypass-key
  extraction) and into the `types.World` population. ~40 LOC across
  `runCheck` and `runPlan`.
- `pkg/ir/builder.go` + `pkg/ir/trajectory_extract.go` + `pkg/plan/r26.go`
  + `pkg/plan/r27.go`: swap the hardcoded annotation-string literals
  for `builder.BypassKeys.AllowDelete.All()` etc. The bypass-keys knob
  is Go-side-only because the Shen rules already receive the
  pre-collapsed `bypass-yes` / `bypass-no` symbol — the kernel never
  sees the annotation string. This is the least invasive of the three
  changes. ~25 LOC.
- `pkg/ir/immutable_registry.go`: parameterize `ImmutableFieldRegistry`
  to accept a user-supplied overlay. Built-ins stay in-file; user
  entries append; suppressions remove. ~15 LOC.
- `kernel/r25-prod-appset-autosync.shen`: replace the hardcoded
  `r25-is-prod-name?` body with a loop over a `ProdPatterns` fact list
  passed in from `check-world`. Add `ProdPatterns` to the
  `extract-section` bindings in `check.shen:84` block. Add a
  `check.shen:129` arg. ~15 Shen LOC + a new test fixture.
- `kernel/r23-crossplane-state-needs-orphan.shen`: replace hardcoded
  `"alb-logs"` with a `NameCarveouts` list lookup. Same extract-section
  shape. ~10 Shen LOC.

Test fixtures: one new top-level directory `testdata/config/` with a
handful of fixtures (empty, full, malformed) plus per-rule fixtures that
exercise the config knobs. Update the existing R25/R23 fixtures to point
at an implicit default-config path.

Total: ~350 Go LOC, ~25 Shen LOC, one new package, touches ~6 existing
files plus the kernel.

### 4.c Invariant: default config ≡ current behaviour

`config.Default()` must return exactly the structs that produce the
current compile-time defaults. This is the mechanical way to guarantee
migration safety (§5) — the loader's output for absent-file is
bit-identical to the hardcoded path, and a test (`TestDefault_Matches_Builtin`)
in `pkg/config/` asserts this against the actual hardcoded slices.

## 5. Migration and backwards-compat

### 5.a What existing tests assume (all unchanged if §4.c holds)

- `pkg/checker/check_test.go:762-808` (R25): asserts
  `crossplane-platform-aws-prod` fires and `some-nonprod-appset` doesn't.
- `pkg/plan/r27_test.go`: fixtures under
  `testdata/fixtures/plan-immutable-change/*` assume specific (Group,
  Kind, FieldPath) tuples from the current registry.
- `pkg/checker/check_test.go:723-724` (R23 bypass fixtures
  `bypass-primary`, `bypass-alias`, `alb-logs-carveout`): exercise the
  two allow-delete keys and the carve-out.
- `pkg/checker/bridge_serialization_test.go:14-60` already parameterizes
  the registry and is robust against the overlay change.

### 5.b Tests to add

- `pkg/config/TestDefault_Matches_Builtin` — default config bit-identical
  to the hardcoded path (§4.c).
- Extend/suppress for immutable-fields; rename bypass prefix; not-found
  returns Default(); malformed YAML is fatal with line number.

### 5.c Known break

- Any Shen test that pattern-asserts the literal `"alb-logs"` needs to
  update when R23 becomes config-driven. No external user has this
  dependency today.

## 6. Open questions

1. **Prod classification: substring, label, or both?** The current
   substring match is intentional (R25 comment block), but the task
   hints at label-based detection (e.g. `environment=prod`). Adding
   labels requires extending `types.ArgoApplicationSet` with a
   `Labels` map, extracting it in `pkg/ir/builder.go`, and defining
   match semantics (any label matches, all labels match, regex value?).
   Do we want label matching in v1 of `xpc.yaml`, or is substring
   enough?

2. **Immutable-fields: overlay vs. replacement?** The schema proposed in
   §2 is additive-with-explicit-suppression. An alternative is to make
   `xpc.yaml` a full replacement — present-means-authoritative. Upside:
   clearer contract; downside: users forget an entry and silently lose
   a check. Which does the user want as default?

3. **Should bypass-key renaming be per-rule or global?** §2 proposes
   per-logical-bypass (`allow-delete`, `allow-immutable-change`
   independently). A simpler alternative is a single `ignore-prefix`
   string that rewrites every `xpc.io/` key at once. Per-rule is more
   expressive; global is easier to migrate an org onto. Pick one.

4. **Does `xpc.yaml` need to be versioned (a top-level `version: 1`
   key)?** The loader contract in §3.b forbids unknown top-level keys;
   a version key is the canonical escape hatch for future
   incompatible schema changes. Small cost now; cheap insurance later.

5. **Where does `xpc.yaml` live in the repo?** Candidates: repo root,
   `.xpc/config.yaml`, `deploy/xpc.yaml`. Root is the simplest. The
   `.xpc/` dir is more conventional for tooling that might grow
   multiple files (snapshots, rule disable lists). The discovery walk
   in §3.a handles either, but "canonical location" should be
   documented so tooling (editors, CI) knows what to highlight.
