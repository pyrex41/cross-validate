# Inspecting the typed IR with `xpc dump-ir`

When a finding looks wrong — a false positive, a rule that should have
fired but didn't, or a diagnostic pointing at a field you don't recognize —
the fastest way to understand it is to look at the **input the rules
actually see**. That input is the typed IR (intermediate representation):
the `World` that `xpc` builds from your manifests after YAML parsing,
Helm/Kustomize/Composition rendering, and ApplicationSet generator
expansion.

`xpc dump-ir` prints that IR as an `.xpcir` s-expression so you can read
exactly what the kernel rules consume.

## Usage

```sh
xpc dump-ir <path>
```

`<path>` is a single directory or file. There are no other flags (only
`--help`). If you omit the path you get `error: no path specified`.

```sh
# Dump the IR for the whole manifold tree
xpc dump-ir . > manifold.xpcir

# Dump the IR for just one app's directory
xpc dump-ir apps/aurora-preview/

# Dump the IR for a single file you're debugging
xpc dump-ir apps/aurora-preview/cluster-instance.yaml
```

The IR goes to **stdout**; redirect it to a file for large trees.

## What you get

The output is a flat list of s-expressions, one per typed entity, starting
with a version header:

```lisp
(xpcir-version 1)

(crd
  (group "rds.aws.upbound.io")
  (kind "ClusterInstance")
  ...)

(composition
  (name "aurora-xpostgresqlinstance")
  ...)

(function
  (name "function-patch-and-transform")
  (package "..."))
```

Entities are emitted in this order: CRDs, XRDs, content-addressed Schemas,
Compositions, Functions, Providers, Configurations, and the Argo / managed
resource entities. CRDs and XRDs share the same `(crd ...)` shape (XRDs are
emitted with the XRD flag set in `writeCRDSExpr`).

## Debugging workflow

1. Reproduce the finding: `xpc check <path>` (add `--skip-render` to rule
   out a renderer difference, or `--focus=<category>` to isolate).
2. Dump the IR for the same path: `xpc dump-ir <path> > debug.xpcir`.
3. Search the IR for the entity named in the diagnostic. The diagnostic's
   `rule:` line names the offending Kind/name (e.g.
   `ClusterInstance/aurora-preview-cluster-instance-1`); grep the `.xpcir`
   for that name.
4. Confirm the IR matches your mental model:
   - **False positive?** The IR usually shows the field the rule keyed on
     is missing/misparsed (e.g. an `ignoreDifferences` block that didn't
     propagate from an AppSet template, or a `deletionPolicy` that rendered
     under a different path than expected).
   - **Missing finding?** The entity may be absent entirely — a Helm/
     Kustomize render that silently produced nothing, or an Application
     whose source didn't expand. Two distinct under-capture paths to keep in
     mind: running with `--skip-render` (or a missing `helm`/`kustomize`
     binary) emits an **info**-level skip diagnostic per skipped Application,
     while an absent `crossplane` binary degrades the composition-render pass
     to a **warning**-severity `XPC.H.composition-renders`. In both cases the
     rendered resources never enter the IR.
5. If render is involved, compare `xpc dump-ir <path>` with and without
   `XPC_CACHE_DIR` set / with the renderer binary on `$PATH` to see whether
   the difference is in rendering or in rule logic.

## Related

- The same `World` digest that backs the IR is recorded in `xpc check
  --proof` artifacts (`IR digest`, surfaced by `xpc verify`).
- To capture the IR as a portable, content-addressed artifact (rather than
  a one-shot text dump), use [`xpc snapshot`](snapshot.md), which serializes
  the same `World` plus a SHA-256 digest into a `.xpcsnap`.
