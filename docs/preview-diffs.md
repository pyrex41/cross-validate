# Preview diffs: see what a Crossplane PR would change

## Why this exists

Crossplane already has [crossplane-plan](https://github.com/crossplane-contrib/crossplane-plan),
an in-cluster operator that previews the effect of a proposed change. It's
useful, but it requires the cluster to hold provider credentials with
sufficient permissions, deploys a controller, and produces output that lives
inside cluster-scoped CRs.

`xpc plan` solves the same problem from the opposite direction. It runs
offline, on a CI runner, with a read-only kubeconfig. There is no operator
to deploy, no in-cluster credentials to provision, and no privileged
controller in the data path. The capture step writes a single signed-able
artifact (`.xpcsnap`); the plan step diffs that artifact against the
PR's manifest tree and emits Markdown that you can post to the MR or PR.
The artifact is the audit trail.

## The two-step flow

1. Capture cluster state into a `.xpcsnap` artifact (or a directory).
2. Run `xpc plan --base=<artifact> --head=<PR's manifest dir>`.
3. (Optional) Post the resulting Markdown to the MR/PR via `--post-comment`.

## GitLab MR walkthrough

### Prerequisites

- kubectl >= 1.24, jq >= 1.6 on PATH.
- KUBECONFIG configured for read access to the cluster.
- glab installed; auth via `GITLAB_TOKEN`, `CI_JOB_TOKEN`, or `GLAB_TOKEN`.

### One-shot recipe

```bash
tools/xpc-capture-cluster-snap.sh --cluster-name=prod /tmp/cluster.xpcsnap
xpc plan --base=/tmp/cluster.xpcsnap --head=. \
         --post-comment=auto --dry-run
```

### What `--post-comment=auto` reads

- GitLab CI: `CI_PROJECT_PATH` + `CI_MERGE_REQUEST_IID`.
- Falls back to GitHub if GitLab vars are absent — see below.

### Removing `--dry-run`

- Drop `--dry-run` to actually post the comment.
- Add `--post-comment-required` to gate the pipeline on a successful post
  (default behaviour is best-effort: a posting failure logs but does not
  fail the job).

## GitHub PR walkthrough

### Prerequisites

- Same kubectl / jq versions as above.
- gh installed; auth via `GH_TOKEN` or `GITHUB_TOKEN`.
- For GitHub Actions: `permissions: pull-requests: write` on the workflow.

### One-shot recipe

```bash
tools/xpc-capture-cluster-snap.sh --cluster-name=prod /tmp/cluster.xpcsnap
xpc plan --base=/tmp/cluster.xpcsnap --head=. \
         --post-comment=auto --dry-run
```

### What `--post-comment=auto` reads (GitHub)

- `GITHUB_REPOSITORY` + `GITHUB_REF`. The ref must match
  `refs/pull/N/merge` — the form GitHub Actions sets for the synthetic
  merge ref on a `pull_request` event.

## What gets captured

- Argo objects: `Application`, `ApplicationSet`, `AppProject`. Pass
  `--no-argo` to skip them; the default is `--include-argo`.
- Crossplane managed resources whose CRD group matches one of the
  `--providers=` patterns (suffix match).
- Default `--providers=` list:
  - `aws.upbound.io`
  - `gcp.upbound.io`
  - `azure.upbound.io`
  - `crossplane.io`
- That default is intentionally broader than xpc's strict state-bearing
  list. It gives plans richer cluster context at the cost of capturing
  some kinds that don't strictly own provider state.

**Not captured** (by design):

- Secrets and ConfigMaps.
- Anything outside the configured `--providers=` groups.
- Anything in skipped namespaces. `--skip-namespaces=` is accepted for
  forward compatibility but is a no-op in v1; namespace filtering is
  on the roadmap.

## Snapshot caveats

- **Embedded timestamp.** `xpc plan` warns if a snapshot is older than
  the default TTL (15 minutes; see `pkg/snapshot.DefaultTTL`).
- **Digest stability.** The content hash includes only the data the
  snapshot carries, not the timestamp, so two captures of an unchanged
  cluster produce identical digests. The wrapper JSON differs
  byte-for-byte across runs because it includes the capture time.
- **Signature support.** Optional and not yet automated. Future work.
- **Incomplete captures.** Snapshots created without
  `--include-resources` (i.e. via the legacy 2-arg `snapshot.FromWorld`)
  will trigger an `XPC.P.snapshot-incomplete` info diagnostic on the
  base tip when used with `xpc plan`.

## Troubleshooting

### `XPC.P.snapshot-incomplete` on the base tip

The snapshot was captured without `--include-resources`. Use
`tools/xpc-capture-cluster-snap.sh` (which always passes the flag) or
re-capture manually with `xpc snapshot --include-resources`.

### `post-comment failed: required CLI not on PATH`

Install `glab` (GitLab) or `gh` (GitHub) on the runner. The capture
scripts do not install these — they're a runner-image concern. See the
`xpc:plan` job in [`docs/templates/gitlab-ci.yml`](templates/gitlab-ci.yml)
for one way to install glab from a release tarball.

### `error: kubectl unauthenticated; check KUBECONFIG / context`

The capture script's auth probe failed before fetching anything. Verify
that `kubectl auth can-i get crd` succeeds from the runner with the
same KUBECONFIG. CI runners frequently have a stale or wrong-cluster
kubeconfig baked in; pass `--kubeconfig=<path>` explicitly if needed.

### Empty diff on a PR you expect to be destructive

Confirm the resource's CRD group is in your `--providers=` list. The
default list is broad but explicit; private providers
(`*.example.com`, internal forks) must be added with
`--providers=aws.upbound.io,gcp.upbound.io,azure.upbound.io,crossplane.io,acme.example.com`.

### Plan succeeds but no comment shows up on the MR

You're probably still running with `--dry-run`. Drop the flag and
re-run; if you want the pipeline to fail when posting fails, add
`--post-comment-required`. For broader CI integration tips (auth
env vars, recommended job ordering), see
[`docs/ci-integration.md`](ci-integration.md).
