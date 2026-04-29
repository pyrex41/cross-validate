# CI integration guide

## Overview

`xpc check` emits a SARIF 2.1.0 report when invoked with `--format=sarif`. GitLab
auto-ingests SARIF via its SAST report artifact (`artifacts.reports.sast`), so
xpc findings surface as merge-request annotations with no custom tooling on the
GitLab side. The authoritative source for the SARIF schema we emit is
[`pkg/report/reporter.go` lines 400–456](../pkg/report/reporter.go) — the
`reportSARIF` function, which produces a `2.1.0` document pointing at
`https://json.schemastore.org/sarif-2.1.0.json`.

## Output formats

`xpc check` supports six output formats (see `pkg/report/reporter.go`, `Format`
constants at lines 19–26). Pick one with `--format=<fmt>`:

| Format  | When to use                                                            |
|---------|------------------------------------------------------------------------|
| `human` | Local dev; pretty-printed with source excerpts and fix hints.          |
| `agent` | Dense, LLM-optimised text (default). Use when piping into Claude/GPT.  |
| `json`  | Machine-readable raw diagnostic array. Good for custom tooling.        |
| `junit` | CI runners that understand JUnit XML (Jenkins, CircleCI test reports). |
| `sarif` | GitLab SAST, GitHub Code Scanning, any SARIF 2.1.0 consumer.           |
| `lsp`   | LSP diagnostic format for future IDE/editor plugins.                   |

Note: the top-level `xpc check --help` text lists only `agent, human, json,
sarif`, but the format parser (`cmd/xpc/main.go:142-143`) delegates directly to
`report.Format`, so `junit` and `lsp` also work.

## Exit-code semantics

Confirmed by reading `cmd/xpc/main.go:314-320`:

```go
// Exit non-zero if there are errors
for _, d := range diags {
    if d.Severity == types.SeverityError {
        return 1
    }
}
return 0
```

| Exit | Meaning                                                                |
|------|------------------------------------------------------------------------|
| 0    | Clean run. Diagnostics may include `warning` or `info`, but no `error`.|
| 1    | At least one `error`-severity diagnostic, OR a setup/IR failure.       |

Other `return 1` paths exist for operational failures (unknown flag, file not
found, IR build error, report writer error, missing YAML, proof save failure).
These are indistinguishable from finding-driven failures on the exit code
alone; use the SARIF report to tell them apart in CI.

Severity mapping in the SARIF output (`reporter.go:417-422`):

| xpc severity           | SARIF `level` |
|------------------------|---------------|
| `types.SeverityError`  | `error`       |
| `types.SeverityWarning`| `warning`     |
| `types.SeverityInfo`   | `note`        |

Known gap: there is currently no `--warnings-as-errors` flag. If you want a
hard gate on warnings, either add severity escalation in a wrapper script, or
use the SARIF consumer's own severity-threshold setting.

## GitLab SAST integration

A working template lives at [`docs/templates/gitlab-ci.yml`](templates/gitlab-ci.yml).
Include it from your pipeline with:

```yaml
include:
  - local: docs/templates/gitlab-ci.yml
```

When a job declares `artifacts.reports.sast: <path-to-sarif>`, GitLab parses
the file and surfaces findings in the merge-request "Security" tab and as
inline diff annotations. No GitLab Ultimate license is required to produce
the artifact — the merge-request widget shows under Free tier as well, though
some enterprise dashboards (Vulnerability Report, dependency scanning graph)
are Ultimate-only.

### `allow_failure` trade-off

The template defaults to `allow_failure: true`, which matches GitLab's own
SAST conventions (the bundled `SAST.gitlab-ci.yml` uses the same default).
Pick the value that matches the posture you want:

- **`allow_failure: true`** (default, recommended for onboarding): findings
  surface on the MR, but a non-zero xpc exit does not fail the pipeline.
  Teams can triage at their own pace while xpc builds trust.
- **`allow_failure: false`** (hard gate): any `error`-severity xpc finding
  blocks merge. Only flip this after you've triaged the backlog and are
  confident the ruleset matches your intent — otherwise every MR touching
  manifests will break.

A common staging path is: start with `allow_failure: true`, burn down the
backlog, then set `allow_failure: false` once the MR-by-MR incremental
surface is clean.

## CLI flags relevant to CI

All flags below are parsed in `cmd/xpc/main.go:118-155`.

| Flag                        | CI-facing rationale                                                                                     |
|-----------------------------|---------------------------------------------------------------------------------------------------------|
| `--format=sarif`            | Emit SARIF 2.1.0 on stdout for GitLab / GitHub / any SARIF 2.1.0 consumer.                              |
| `--helm-cache-dir=<dir>`    | Directory for remote Helm chart pulls + render cache. Point at a CI-cached path to avoid re-downloads. |
| `--kustomize-bin=<path>`    | Pin the kustomize binary. Useful when your image has kustomize at a non-standard location.              |
| `--helm-bin=<path>`         | Pin the helm binary. Same rationale as `--kustomize-bin`.                                              |
| `--skip-render`             | Skip Helm/Kustomize rendering entirely. Emits one info diag per skipped Application. Use as a last resort when you cannot install helm/kustomize in CI — you will lose coverage of rendered-manifest rules. |
| `--kernel-path=<dir>` / `XPC_KERNEL_PATH` | Pin the Shen kernel directory. Needed when the binary runs from a path where the default upward cwd search cannot find the kernel tree (e.g., scratch containers). |
| `--appset-fixture=<file>`   | YAML fixture for ApplicationSet `pullRequest`/`scmProvider` generators. Required in CI where xpc cannot reach GitHub/GitLab APIs to expand PR generators.  |

`--proof` and `--snapshot=<path>` are also valid and useful if you are wiring
the audit/proof pipeline — see the main README for those. They are not
required for basic SAST integration.

## GitHub Actions (optional)

A minimal GitHub Actions equivalent using the first-party SARIF upload action:

```yaml
name: xpc
on: [pull_request]
jobs:
  xpc:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - name: Install helm
        uses: azure/setup-helm@v4
      - name: Install xpc
        run: go install github.com/pyrex41/cross-validate-/cmd/xpc@latest
      - name: Run xpc
        run: xpc check --format=sarif --helm-cache-dir=$GITHUB_WORKSPACE/.xpc-cache . > xpc.sarif
        continue-on-error: true
      - name: Upload SARIF
        uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: xpc.sarif
```

`continue-on-error: true` is the GitHub Actions analogue of GitLab's
`allow_failure: true`. Flip it to `false` once you are ready to gate on
xpc findings.

## Preview-diff integration (capture + plan + post-comment)

`xpc plan` extends the lint workflow with a delta view: it diffs live
cluster state (captured into a `.xpcsnap` artifact) against the PR's
manifests, and optionally posts the resulting Markdown to the merge
request or pull request as a comment. Where `xpc check` finds bugs in
a single manifest tree, `xpc plan` shows what the cluster will look
like *after* the PR merges.

For the full workflow, see [`docs/preview-diffs.md`](preview-diffs.md).

### Prerequisites

- `kubectl` >= 1.24, `jq` >= 1.6 on the runner.
- A read-only KUBECONFIG that can `get crd` and list resources in the
  configured `--providers=` groups.
- `glab` (GitLab) or `gh` (GitHub) on the runner if using
  `--post-comment`.

### Recommended pipeline shape

- One job per environment (staging, prod) so each posts a separate
  comment scoped to that environment.
- Run after `xpc check` succeeds — a syntactically broken PR
  shouldn't waste a cluster fetch.

### Auth env vars by VCS

- **GitLab**: `glab` honours `GITLAB_TOKEN`, `CI_JOB_TOKEN`, and
  `GLAB_TOKEN`. `CI_JOB_TOKEN` is set automatically by GitLab and is
  the simplest option for most projects.
- **GitHub**: `gh` honours `GH_TOKEN` and `GITHUB_TOKEN`. In GitHub
  Actions, set `permissions: pull-requests: write` on the workflow.

xpc itself never reads or stores tokens — it shells out to the user's
own CLI, which sees its native auth env by inheritance.

### Caveats

- See [Snapshot caveats](preview-diffs.md#snapshot-caveats) in the
  preview-diff guide for TTL, digest stability, and the
  `XPC.P.snapshot-incomplete` info diagnostic.
- `--post-comment-required` flips posting from best-effort to a hard
  gate. Default is best-effort: a posting failure logs to stderr but
  does not change the plan's own exit code (which is driven only by
  `XPC.P.*` error diagnostics).

### Recipe

- See [`docs/templates/gitlab-ci.yml`](templates/gitlab-ci.yml) for a
  GitLab CI template; the `xpc:plan` job is the runnable shape.
- For end-to-end background, see
  [`docs/preview-diffs.md`](preview-diffs.md).
