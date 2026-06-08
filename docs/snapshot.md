# Capturing a portable type environment with `xpc snapshot`

A snapshot is a single content-addressed `.xpcsnap` file that captures the
type environment `xpc` built from a set of manifests: CRDs, XRDs, Schemas,
Compositions, Functions, Providers, Configurations, and (optionally) the
live resource instances and Argo objects. It carries a SHA-256 digest over
its content and is portable — you can capture it once and diff or re-check it
later without re-rendering the source tree.

Snapshots are the input to two other commands:

- `xpc plan --base=<ref> --head=<ref>` accepts `.xpcsnap` files on either
  side, so you can diff "what was" (or "what's running") against "what we
  propose."
- `xpc check --snapshot=<file> <path>` merges a snapshot's type environment
  into the world being checked, so a manifest tree can be linted against
  CRDs/XRDs that live in the snapshot rather than the tree.
- `xpc check --snapshot=<file>` with **no path** — the snapshot alone is the
  source of the world (see *Snapshot-only check* below).

## Usage

```sh
xpc snapshot [flags] [<path>]
```

`<path>` is a directory or file; it defaults to `.` when omitted (manifest
mode). The flags `xpc snapshot` recognizes:

| Flag | Effect |
|------|--------|
| `--output=<path>` | Write the `.xpcsnap` to `<path>`. Prints `snapshot written to <path>` to **stderr**. |
| `--cluster=<name>` | Name recorded in the snapshot. Default: `local`. |
| `--from-cluster` | Capture from a **live cluster** via `kubectl` instead of the filesystem (implies live resources). Requires `kubectl`; takes no path argument. |
| `--context=<name>` | kube-context for `--from-cluster` (default: kubeconfig current-context). |
| `--kubectl-bin=<path>` | Path to `kubectl` (default: first on `$PATH`). |
| `--include-resources` | In manifest mode, also capture resource instances and Argo objects (Applications, ApplicationSets, AppProjects), not just the type environment. (`--from-cluster` always includes them.) |
| `--diff=<a>,<b>` | Load two existing `.xpcsnap` files and print a human-readable diff; ignores `<path>`. |
| `--help`, `-h` | Print usage. |

Any other `-`-prefixed argument is rejected with `unknown flag`.

**Output contract:** the content digest is always printed to **stdout**
(one `sha256:...` line). With `--output=`, the artifact is written to disk
and the "snapshot written to ..." notice goes to **stderr**. Without
`--output=` you get only the digest — useful as a cheap fingerprint of a
tree.

### Examples

```sh
# Type-environment-only snapshot of the whole tree (digest to stdout)
xpc snapshot --cluster=fg-manifold-main --output=manifold.xpcsnap .

# Full manifest snapshot including resource instances + Argo objects
xpc snapshot --include-resources --cluster=fg-manifold-main \
  --output=manifold.xpcsnap .

# Live-cluster snapshot via kubectl (the "reality" side of git-vs-reality)
xpc snapshot --from-cluster --context=facilitygrid-ops \
  --cluster=facilitygrid-ops --output=live.xpcsnap

# Just fingerprint a tree without writing a file
xpc snapshot apps/aurora-preview/

# Diff two snapshots
xpc snapshot --diff=baseline.xpcsnap,today.xpcsnap
```

## Manifest mode vs. `--from-cluster`

- **Manifest mode (default):** reads the committed manifests off disk and
  builds the type environment from them — the *git* side of "git vs reality."
  Add `--include-resources` to also carry resource identities.
- **`--from-cluster`:** shells out to `kubectl` to capture what is actually
  running — CRDs, Crossplane apiextensions (XRDs/Compositions) and
  `pkg.crossplane.io` packages, Argo objects, and managed-resource instances
  (discovered dynamically by provider group). This is the *reality* side. It
  requires `kubectl` on `$PATH` (or `--kubectl-bin=`) and never silently
  falls back to the filesystem — an absent `kubectl` is a hard error, because
  a snapshot that quietly came from disk would corrupt every drift comparison.
  The captured kind list is logged to stderr so coverage is explicit.

## Consuming a snapshot

```sh
# Drift: diff a captured cluster against the proposed git ref.
xpc plan --base=live.xpcsnap --head=<git-ref> .

# Diff a proposed git ref against a manifest baseline (destructive-change
# report). Either side may be a .xpcsnap or a git ref / directory.
xpc plan --base=manifold.xpcsnap --head=<git-ref> .

# Lint a tree against the CRDs/XRDs carried in the snapshot.
xpc check --snapshot=manifold.xpcsnap .
```

`xpc check --snapshot=` warns if the snapshot is older than 15 minutes (the
default staleness TTL). `xpc plan --base=<snap>` does **not** emit that
warning, so a day-old nightly baseline is fine to diff against with `plan`.

## Snapshot-only check (in-cluster audit)

A snapshot captured with `--from-cluster --include-resources` carries the live
resource instances (`status.atProvider` and all). You can run `xpc check`
against **only** that snapshot, with no path argument and no git checkout:

```sh
# In-cluster audit: the snapshot is the sole source of the world.
xpc check --snapshot=live.xpcsnap --skip-render

# Scope to the dynamic-state rules (category M) and emit SARIF for upload.
xpc check --snapshot=live.xpcsnap --category=M --skip-render --format=sarif \
  > audit.sarif
```

This is the shape for an in-cluster audit CronJob: capture the cluster with
`xpc snapshot --from-cluster --include-resources --output=live.xpcsnap`, then
`xpc check --snapshot=live.xpcsnap` — no repository to clone. The snapshot's
merged resources populate `w.Resources`, so the dynamic rules that read live
state fire — notably **R32** (`XPC.M.observed-desired-fixed-point`), which
detects a `spec.forProvider` / `status.atProvider` divergence (the reconcile-
storm fingerprint) that is invisible from manifests on disk.

Control flow:

- **No path + `--snapshot=`** → the snapshot sources the world. The world is
  built from zero docs, the snapshot is merged, and resource-derived facts are
  recomputed over the merged world before the rules run.
- **No path + no `--snapshot=`** → defaults to `.` (the current directory), as
  before.
- **Zero docs *and* no snapshot** → still errors with `no YAML documents
  found` (exit 1). The only-empty-when-truly-empty gate is preserved.
- **A path *and* `--snapshot=`** → unchanged: manifests are the world and the
  snapshot's type environment is merged in.

Use `--skip-render` for snapshot-only runs: there are no Helm/Kustomize sources
to render when the input is a captured cluster.

## Nightly baseline in CI

A nightly `.xpcsnap` of `main` makes a good baseline that `xpc plan` (and the
`--from-cluster` compare) can diff against. Capture it with
`--include-resources` so resource identities are present, gate the job on the
GitLab scheduled-pipeline source, and publish it as an artifact. (Wiring this
job into fg-manifold's `.gitlab-ci.yml` is tracked under the adoption plan's
Part B and depends on a published `xpc` binary in CI.)

Two baselines are possible:

- **Manifest baseline** (works today): captured from the committed manifests
  in `main` — the git side.
- **Live-cluster baseline** (`--from-cluster`): captured from a running
  cluster — the reality side. `xpc plan` diffing the two surfaces true drift
  between what's deployed and what's in git.

## Related

- [`xpc dump-ir`](dump-ir.md) prints the same `World` as a one-shot
  s-expression for ad-hoc debugging.
- [`docs/ci-integration.md`](ci-integration.md) covers SARIF ingestion and
  exit-code semantics for `xpc check` in CI.
- [`docs/preview-diffs.md`](preview-diffs.md) covers the offline
  preview-diff flow that `xpc plan` powers.
