# Perf floor after kernel-call optimisation, and the AOT tradeoff

**Date:** 2026-04-28
**Author:** session w/ Claude (Opus 4.7)
**Status:** Research note. No implementation pending.

## Where we landed

After the shen-go v1.1.1 upgrade (native reader + load cache + kernel 41.1)
and the empty-section guards in R6 / R6c / R7 / R15, `xpc check` cold timings
look like this on a Mac M-series:

| Workload                | kernel-call | total wallclock |
| ----------------------- | ----------- | --------------- |
| basic (3 docs)          | ~3 ms       | ~0.5–1.0 s      |
| 100 ArgoApp-only        | ~2 ms       | ~0.9 s          |
| 1000 ArgoApp-only       | ~2.5 ms     | ~1.0–1.4 s      |

`kernel-call` is essentially flat — the rules complete in single-digit
milliseconds even for 1000 apps. The remaining wallclock is dominated by
**`init-shen`: ~800 ms cold**, which is the parse + load of the 28
`kernel/*.shen` files via shen-go's native reader. That's the perf floor.

The phases below `init-shen` (loader, IR build, fact serialization, kernel
call, output) total <50 ms across our workloads. They're not levers any more.

## Lever 1 — persistent parsed-form cache (tabled)

shen-go v1.1.1 ships an in-process `loadCache` that maps `(file path,
mtime, size, content hash)` → parsed `kl.Obj` forms. It does nothing for us
today because each `xpc` invocation is a fresh process; the cache populates
during `(load …)` and dies on exit.

**The shape of the fix:** extend `loadCache` (or wrap it) so the parsed
forms also persist to disk under `$TMPDIR/xpc-shen-cache-<digest>/<file>.kl`
or similar. On startup, before calling `installLoadCache`, hydrate the
in-memory map from disk. Files whose content hash matches the on-disk entry
skip the reader entirely; mismatches re-parse and re-write.

**Expected payoff:** init-shen ~800 ms → ~50 ms once the cache is warm.
First-run-after-kernel-edit pays the full 800 ms; subsequent runs see the
fast path. That brings cold `xpc check basic` from ~1 s to ~0.1–0.2 s.

**Why we tabled it:**
- 1 s cold for a CI-style tool is already fine. The user-felt latency
  from "I committed and CI returned" is dominated by network + container
  startup, not xpc itself.
- The serialization format for parsed `kl.Obj` is non-trivial. shen-go's
  parsed forms reference interned symbols; restoring them requires
  re-interning during deserialize. Doable but fiddly.
- Cache invalidation needs to be airtight — a stale cache that survives
  a `kernel/r11-api-deprecation.shen` edit would silently run the old
  rules. Content-hash + version stamp solves this but is more code than
  it sounds.
- It's a downstream-only fix (or a fork patch). Carrying it out-of-tree
  costs us the next time we resync from upstream.

**When to revisit:** if interactive xpc use becomes a thing (a contributor
running `xpc check` in a tight edit loop while iterating on a rule), the
~800 ms cold start gets annoying fast. At that point, do this. Or skip
straight to a daemon — same perf, simpler invalidation.

## Lever 2 — real AOT compilation of kernel/*.shen → Go

The dream end-state: `kernel/*.shen` is compiled to Go at build time,
linked into the binary. `init-shen` becomes ~30 ms (just the runtime
register sequence we already pay), no parsing on cold start. Total cold
wallclock for the typical workload: ~100 ms.

### Benefit

- **Cold start ~10× faster.** ~1 s → ~0.1 s for typical workloads.
- **Removes the parse-time variance.** init-shen currently swings 500
  ms–1 s depending on filesystem cache state; AOT removes that entirely.
- **Reproducibility wins.** The "ruleset digest" in `pkg/audit/proof.go`
  could be a real hash over the compiled artifact, fixing the hardcoded-
  string bug from the code review (finding #1).
- **No runtime parsing means** the chdir hazard, the embedded-FS
  workaround, and the temp-dir extraction all collapse into a single
  `kernelaot.Init(&cf)` call.
- **Aligns xpc with shen-go's own model.** shen-go's prelude is AOT
  (that's what `internal/shenfull/` is). AOT'ing our kernel makes us
  consistent with the design.

### Cost — why our earlier attempt didn't ship

shen-go's `compile-to-go.shen` is a `.kl` → Go compiler (the bootstrap
artifact for shen-go's own kernel). When we feed it `kernel/*.shen`, the
Shen-source `(define f X -> ...)` macro is **not lowered** by
`compile-file`'s `macroexpand` step — `define` is processed by Shen's
*loader*, not by the macro table that `macroexpand` walks. The result: the
generated Go calls `define` as if it were a regular runtime function, and
rule bodies end up evaluated at top-level instead of wrapped in lambdas.
We hit this at the very first `(map fn list)` inside a generated rule
file: the recursive call evaluated before the `define` for the function
had completed.

Three ways past it, in increasing engineering cost:

1. **Upstream the fix to compile-to-go.shen / compile-file.** The right
   answer is probably: register Shen's `define` macro before
   `macroexpand` runs, so the lowering happens in the codegen pass. If
   the maintainer agrees this is a missing feature rather than out-of-
   scope, this could be a 1–2 day patch (mostly understanding why the
   loader and the macro table diverged in the first place). The pyrex41
   fork already carries the kernel 41.1 + native reader + load cache
   patches; another patch on top is plausible.

2. **Drive shen-go's runtime to emit lowered KL.** Skip `compile-file`
   entirely. At build time: spin up a kl REPL, `(load "kernel/check.shen")`
   normally — Shen's loader correctly lowers `define`. Then walk the
   symbol table and serialize each bound function as KL bytecode using
   shen-go's existing `bc->go` step. Estimated 2–3 days. The risk is in
   the symbol-table-walk: shen-go doesn't expose this as a clean API,
   so we'd be reaching into internals.

3. **Reimplement `define` lowering in Go.** Pre-process each `.shen`
   file before passing to `compile-file`, transforming `(define f
   <type-sig>? clauses…)` into `(defun f args body)` with the
   pattern-match clauses lowered to a `case` tree. Have to handle:
   multi-clause patterns, where-guards, type signatures (we already
   strip these), nested sub-patterns, and the `[X | Y]` cons-list
   destructuring. Estimated a week of careful work plus thorough test
   coverage. Diverges from upstream and adds a maintenance surface.

### Other ongoing costs

- **Build pipeline complexity.** AOT requires the `kl` REPL at build
  time. We can build it from the local checkout (we already do for the
  attempt), but CI now needs `~/projects/shen/shen-go/cmd/kl`
  buildable. Manageable but real.

- **Generated-code blast radius.** Our 28 .shen files would produce
  tens of thousands of lines of generated Go. Either committed (large
  diffs on every kernel edit) or `make`-regenerated each build (build
  graph complexity, harder reproducibility for downstream consumers
  using `go install`).

- **Maintenance friction.** Every kernel change becomes a two-step
  edit + regenerate workflow. Forgetting the second step ships stale
  rules. CI guard mitigates but doesn't eliminate the friction.

- **Validation surface.** AOT-generated bytecode can have subtle
  semantics differences from source-loaded code (we saw exactly this
  with `(define …)` body placement). Each kernel change requires
  running the full test suite against the AOT'd output, not just the
  source-loaded one.

### When AOT pays off

The typical xpc invocation is a one-shot CI gate: someone pushes a PR,
CI runs `xpc check`, returns a result. That flow doesn't care about 1 s
vs 100 ms — the surrounding network + container startup is the
dominant cost.

AOT pays off in a different shape of use:

- **Pre-commit / git hook integration**, where every commit pays a
  cold start. 1 s feels slow when committing every few minutes.
- **Interactive editing of rules**, where a contributor runs xpc in a
  loop while iterating. (Persistent cache or daemon also fixes this.)
- **Massive monorepo CI** running xpc many times per build (per-app
  parallel checks). A daemon or a long-running batch runner is the
  better answer here, but AOT compounds with both.
- **The reproducibility / ruleset digest story.** A meaningful
  ruleset attestation needs a stable artifact to hash. AOT gives us
  one. Source-load doesn't.

### Recommendation

**Don't do AOT yet.** The 1 s cold-start floor is acceptable for the
current use case (CI gate, occasional local check). The work is
non-trivial — at minimum 2–3 days for option (2), more for the others —
and the alternative levers (persistent cache, daemon mode) get us most
of the way there for less.

**Do the AOT work if and when** the ruleset-digest reproducibility
becomes a hard requirement (fixing audit finding #1 properly), or
interactive use becomes a real workflow. At that point, the right entry
is option (1): try to upstream the fix to `compile-to-go.shen`. If the
maintainer takes the patch, AOT becomes a one-line Make target on top
of upstream tooling. If not, fall back to option (2).

## Summary

| Lever                                  | Effort       | Cold start | Worth it now?               |
| -------------------------------------- | ------------ | ---------- | --------------------------- |
| (current state)                        | —            | ~1 s       | yes                         |
| Persistent parsed-form cache           | half day     | ~0.1 s     | tabled — revisit if needed  |
| Daemon mode (`xpc serve`)              | 1–2 days     | ~10 ms     | tabled — revisit if needed  |
| Real AOT (option 1, upstream patch)    | 1–2 days     | ~0.1 s     | when ruleset-digest matters |
| Real AOT (option 2, runtime walk)      | 2–3 days     | ~0.1 s     | only if option 1 fails      |
| Real AOT (option 3, reimplement)       | week+        | ~0.1 s     | only if 1+2 both fail       |
