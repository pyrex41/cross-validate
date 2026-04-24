# GitHub Actions integration for xpc

Status: design draft, 2026-04-24. Extends `docs/ci-integration.md` (GitLab
story). Answers the charter gap "variant-aware gate" on GitHub specifically.

## 1. What `docs/ci-integration.md` already covers

The existing doc is GitLab-centric and carries three pieces we reuse
verbatim:

- **SARIF 2.1.0 is the universal output.** `xpc check --format=sarif`
  emits a spec-conformant document (`pkg/report/reporter.go:400-456`)
  pointing at `https://json.schemastore.org/sarif-2.1.0.json`. GitLab
  ingests it via `artifacts.reports.sast`; GitHub ingests the same file
  via `github/codeql-action/upload-sarif@v3`. This is the single reusable
  paragraph — everything below assumes you have read it.
- **Exit-code semantics.** `xpc check` returns 1 iff any diagnostic has
  severity `error` (`cmd/xpc/main.go:348-354`). Non-finding failures
  (kernel-path not resolved, YAML parse, IR build) also return 1 and are
  indistinguishable on the exit code alone; use the SARIF report to tell
  them apart.
- **Minimal GitHub example.** `docs/ci-integration.md:120-150` already
  shows the `upload-sarif` step. It uses tip-mode `xpc check`, builds
  from `go install`, and treats `continue-on-error: true` as the GitHub
  analogue of `allow_failure`.

What the existing doc does **not** cover, and what this design adds:

1. **Variant-aware gating.** The snippet runs `xpc check`, which reports
   the full tip surface. For a PR that merely touches one Application,
   the reviewer sees dozens of pre-existing warnings that have nothing
   to do with their diff. The charter claim "variant-aware gate" wants
   a PR-local view — only diagnostics introduced by the PR, plus the
   destructive-change set.
2. **`xpc plan` invocation from a PR.** Plan mode checks out both refs
   and diffs the rendered Worlds (`pkg/plan/plan.go:1-10`); it is the
   right CLI for a PR gate. `docs/ci-integration.md` does not mention it.
3. **PR-comment story for repos without Advanced Security.**
   `upload-sarif` requires GitHub Advanced Security on private repos.
   fg-manifold, our primary consumer, is private. We need a native
   PR-comment path.
4. **Binary + kernel distribution.** `resolveKernelPath`
   (`pkg/checker/bridge.go:119-143`) searches cwd upward, then falls
   back to searching upward from the xpc executable. `replay-results-v8.md`
   confirmed that `go build -o /tmp/xpc` in CI **fails** because
   `/tmp/` has no `kernel/` ancestor. Distribution must preserve
   kernel-next-to-binary.

## 2. Invocation patterns from `cmd/xpc/main.go`

Confirmed against the source (do not take these from the help text —
the help text is slightly out of sync with the flag parser):

- `xpc check --format=sarif <path>` emits SARIF on stdout. The format
  flag is parsed at `cmd/xpc/main.go:163-164` and delegates directly to
  `report.Format` (`pkg/report/reporter.go:19-26`).
- `xpc plan --base=<ref> --head=<ref> --format=<json|markdown> <path>`.
  Flags parsed at `cmd/xpc/main.go:641-713`. Default head is `HEAD`
  (`:643`); `--base` is **required** (`:715-718`). The plan runs with
  `skipRender=true` by default (`:645`) — opt into rendering via
  `--render`.
- **Gap:** `plan.Format` only accepts `json` and `markdown`
  (`pkg/plan/output.go:12-18`, dispatched at `cmd/xpc/main.go:664-674`
  and `:764-775`). There is **no** `--format=sarif` for plan mode
  today. The task prompt assumes SARIF-for-plan exists; it does not.
  This design treats plan-mode SARIF as a prerequisite work item —
  see §7, open question 1 — and picks Option B (PR comment) as the
  path that does not block on that prerequisite.
- Exit for plan: returns 1 iff any diagnostic with code prefix
  `XPC.P.` has severity `error` (`cmd/xpc/main.go:778-782`). Tip-side
  diagnostics are reported but do not drive the exit code. This is the
  right shape for a PR gate: the PR fails only on destructive changes
  it actually introduces.

## 3. Design options

| Option | How | Where it fails |
|--------|-----|----------------|
| **A — Code Scanning** | `xpc check --format=sarif` → `github/codeql-action/upload-sarif`. Findings surface as PR annotations + Security tab. | Requires GitHub Advanced Security for private repos. fg-manifold is private without GHAS. Also: tip-mode, not variant-aware — the reviewer sees everything, not just the PR delta. |
| **B — PR comment** | Custom action runs `xpc plan --base=<base.sha> --head=<head.sha> --format=json`, renders Markdown, posts via `gh pr comment`. | Markdown comment does not give inline diff annotations. No Security tab integration. Must manage comment updates (sticky single comment vs. new comment per push). |
| **C — Both** | Composite action runs plan → Markdown comment, plus tip `check --format=sarif` → `upload-sarif`. | Most complex. Pays for itself only when a repo has Advanced Security AND wants the PR-local view. |

**Recommendation: Option B** for fg-manifold specifically, with Option C
as the upgrade path once an org has GHAS. Rationale:

- fg-manifold is private, no GHAS. Option A is unavailable.
- A PR gate should show diagnostics introduced by the PR, not the tip
  surface — that is exactly what `xpc plan` computes.
- The existing snippet in `docs/ci-integration.md:120-150` already
  covers Option A for users who have GHAS. No need to re-document it;
  point them at that section.
- Option B does not depend on adding SARIF to plan mode. It uses
  `--format=json` (which exists at `pkg/plan/output.go:57-80`) and
  renders Markdown in the Action.

## 4. Binary distribution

`xpc` ships as a single Go binary. The kernel (`kernel/*.shen`) is
**not** embedded — `checker.initShen` loads `check.shen` from disk at
runtime (`pkg/checker/bridge.go:55-107`). `resolveKernelPath`
(`bridge.go:119-143`) searches cwd upward, then falls back to walking
upward from the xpc executable's directory. Whatever we ship must put
the binary in a directory that has `kernel/` as a sibling (or
ancestor-sibling).

| Distribution | Pros | Cons |
|--------------|------|------|
| **Release archive** (`xpc-linux-amd64.tar.gz` containing `xpc` + `kernel/`) on `github.com/<org>/cross-validate/releases` | Fast CI (download + untar ≈ 2s). Pinnable by tag. Kernel+binary ship together → `resolveKernelPath` executable-fallback just works. | Requires a release workflow in this repo. Per-platform builds (linux-amd64 is enough for GHA ubuntu-latest). |
| **Docker image** (`ghcr.io/<org>/cross-validate:v0.1.0` with helm/kustomize/crossplane pre-installed) | Pins the entire rendering toolchain. Same image used in GitLab + GitHub + local. | Slow first pull in CI (~150MB). Image rebuilds on every xpc release. Auth to GHCR from private runners. |
| **Build from source in the Action** (`go install …` + `cp -r kernel/ <binpath>/`) | Zero release infra. Tracks HEAD. | Compile time on every PR (≈30s with warm module cache). Requires `actions/setup-go`. Kernel must be `cp -r`d from the checkout, because `go install` drops the binary in `$GOBIN` with no kernel sibling — exactly the replay-v8 failure mode. Fragile. |

**Recommendation: release archive.** The "build from source" snippet in
the current doc (`docs/ci-integration.md:138`) is *already* broken —
`go install github.com/pyrex41/cross-validate-/cmd/xpc@latest` places
the binary in `$GOBIN` with no kernel sibling, so
`resolveKernelPath` will fail unless the caller sets `--kernel-path`
or `XPC_KERNEL_PATH`. The snippet works by accident only because the
checkout provides a kernel tree and the binary happens to be invoked
from inside it. Fix the snippet in the same PR as the Action rollout.

## 5. Sketch of the Action YAML

Target: `~30 lines`. This is the Option B flow. It assumes a release
archive at `v0.1.0` with a stable tarball URL.

```yaml
# .github/workflows/xpc.yml
name: xpc
on:
  pull_request:
    paths:
      - '**.yaml'       # match the set of paths your manifests live under
      - '**.yml'
jobs:
  xpc-plan:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write  # required for `gh pr comment`
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0    # plan mode needs both base and head refs locally
      - name: Install helm (for rendering; optional — see §7 Q4)
        uses: azure/setup-helm@v4
      - name: Install xpc
        run: |
          # Release archive must contain `xpc` and `kernel/` as siblings,
          # so `resolveKernelPath`'s executable-fallback finds the kernel.
          curl -sSL https://github.com/<org>/cross-validate/releases/download/v0.1.0/xpc-linux-amd64.tar.gz \
            | tar -xz -C /opt
          echo "/opt/xpc" >> $GITHUB_PATH
      - name: Run xpc plan
        id: xpc
        run: |
          xpc plan \
            --base=${{ github.event.pull_request.base.sha }} \
            --head=${{ github.event.pull_request.head.sha }} \
            --format=json \
            --helm-cache-dir=$GITHUB_WORKSPACE/.xpc-cache \
            . > xpc-plan.json || true   # never fail here — posting the comment is the point
      - name: Post PR comment
        run: node .github/actions/xpc-comment.js xpc-plan.json
        env:
          GH_TOKEN: ${{ github.token }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
```

Notes:

- `fetch-depth: 0` is required. `plan.Run` (`pkg/plan/runner.go`)
  creates worktrees at the two refs; shallow clones miss `base.sha`.
- `continue-on-error` pattern is expressed via `|| true` on the xpc
  step. We always want to post the comment. The comment-posting
  script is responsible for setting the job's final status (exit 1
  if any `XPC.P.*` error was emitted).
- The `--helm-cache-dir` points at the workspace so `actions/cache`
  can persist Helm pulls across PR runs. Not shown; add
  `actions/cache@v4` keyed on a hash of `Chart.lock` files if Helm
  rendering is slow on first run.

## 6. PR-comment format sketch

Target: what a Claude-like reviewer (or a tired human) wants to see.
Group by rule prefix so the reader can scan the categories their PR
actually touches. Plan mode only surfaces `XPC.P.*` for destructive
changes; tip-side diagnostics are summarised but not per-listed.

```markdown
### xpc plan: main → feat/add-db-app (changed 2 files)

#### Destructive changes (2) — would fail gate

- **XPC.P.destructive-delete**   `deploy/apps/analytics/db.yaml:1`
  `rds.aws.upbound.io/Cluster:analytics-prod` removed on head,
  base declares `deletionPolicy: Delete` — applying the PR runs
  `DeleteCluster` against Aurora.
  _Bypass:_ add `xpc.io/allow-delete: "true"` to the base manifest,
  or set `spec.deletionPolicy: Orphan` on base before removing.

- **XPC.P.immutable-change**   `deploy/apps/analytics/db.yaml:14`
  `rds.aws.upbound.io/Cluster.spec.engineVersion` changed
  `15.4` → `16.2` — Aurora requires destroy+recreate to apply.
  _Bypass:_ `xpc.io/allow-immutable-change: "true"` on the head
  manifest.

#### Resource changes
- Added: 3   Modified: 1   Removed: 1

#### Per-tip static diagnostics (unchanged from base — not gating)
- base (main):   0 errors, 4 warnings, 12 info
- head (feat/add-db-app): 0 errors, 4 warnings, 12 info
```

Constraints that shaped this:

- Destructive section leads. A PR reviewer should see it before the
  resource-count summary. This matches the shape `WriteMarkdown`
  produces today (`pkg/plan/output.go:86-119`) — we are reusing the
  renderer, only wrapping it in the sticky-comment harness.
- Bypass annotations are included inline. `XPC.P.destructive-delete`
  and `XPC.P.immutable-change` have concrete bypass annotations on
  base and head respectively (see `cmd/xpc/main.go:1104-1155`). The
  comment surfaces those so a reviewer can decide without leaving the
  PR.
- Per-tip static diagnostics are collapsed to severity counts. This
  is the "don't show me tip noise" line. If the reviewer wants to
  drill in, they read the full SARIF (Option C) or run `xpc check`
  locally.

## 7. Open decisions

Decide these before implementing the action.

1. **Add `--format=sarif` to `xpc plan`?** Today plan mode emits only
   `json` and `markdown` (`pkg/plan/output.go:12-18`). Option A
   (`upload-sarif`) implicitly wants plan-mode SARIF; without it,
   Option A can only consume tip-mode SARIF, which is not
   variant-aware. Add a `FormatSARIF` plan output that wraps
   plan-side `XPC.P.*` diagnostics + both-tip diagnostics into a
   single SARIF doc? Or keep Option A strictly tip-mode and
   accept the noise?

2. **Host the Action where?** Options: (a) check the action YAML into
   this repo under `.github/actions/xpc-pr-comment/`, reusable via
   `uses: <org>/cross-validate/.github/actions/xpc-pr-comment@v0.1.0`.
   (b) Spin up a separate `cross-validate-action` repo for the
   marketplace listing. (a) is faster, keeps releases coupled, but
   ties every action consumer to the main repo's visibility. (b) is
   Marketplace-friendly. Default: (a) until we need Marketplace.

3. **Fail the PR on XPC.P.* errors, or comment-only?** Analogous to
   `allow_failure` in the GitLab story (`docs/ci-integration.md:84-101`).
   Default recommendation: comment-only for the first 30 days so the
   team can triage false-positives on `XPC.P.immutable-change` (the
   immutable-field catalog is still growing per
   `pkg/ir/immutable_registry.go`). Then flip to gating.

4. **Render Helm/Kustomize in CI?** `xpc plan` defaults to
   `skipRender=true` (`cmd/xpc/main.go:645`). Rendering picks up
   `XPC.H.*` diagnostics but adds 20–60s per Application. A PR gate
   probably wants fast iteration → keep `skipRender`. But then we
   miss e.g. `XPC.H.values-well-typed` drift across the PR. Decision:
   which rules does the gate care about? If only the `XPC.P.*`
   destructive family, skip render. If the answer is "all rules we
   run in a tip check", enable render and pay the time.

5. **Sticky comment vs. one comment per push?** Sticky (find comment
   with known marker, edit it) keeps PR threads tidy but hides the
   history of what used to be wrong. Per-push leaves a trail but
   noise. Default recommendation: sticky, with the last comment's
   body including a collapsed `<details>` block of the prior N=3
   comment bodies so history is preserved but collapsed.

## 8. Scope notes

Out of scope for this design:

- **ApplicationSet fixtures.** `--appset-fixture=<file>` is needed for
  `pullRequest` / `scmProvider` generators (`docs/ci-integration.md:114`).
  CI callers that use those generators will need a committed fixture
  file; this is already documented for GitLab and the GitHub flow
  inherits it unchanged.
- **Snapshot / proof.** `xpc snapshot` and `xpc proof` are cluster-
  adjacent features that do not belong in a PR gate. Cover them in a
  separate design if/when we wire continuous compliance evidence.
- **Bisect.** `xpc bisect` (`cmd/xpc/main.go:597-639`) is a local
  developer tool — no CI integration planned.
