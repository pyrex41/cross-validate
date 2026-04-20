# ADR-003: ApplicationSet expansion as offline simulation

## Status

Accepted — 2026-04-20 (S5).

## Context

Argo CD `ApplicationSet` resources generate concrete `Application` objects
at runtime. The generator kinds most relevant to fg-manifold are:

- `list` — static enumeration of parameter sets
- `git` (directories) — one Application per subdirectory under a git path
- `matrix` — cartesian product of two child generators
- `merge` — deep-merge two generators by a shared key
- `pullRequest` / `scmProvider` — one per open PR on GitHub/GitLab

The first four are fully determined by the manifest tree + filesystem
layout. The last two require hitting a remote API.

xpc is an *offline* type checker. Without expanding ApplicationSets the
rules in categories D (kind-whitelisted), E (selector-needs-ignore-diff),
F (trajectory), and H (render) run only against statically authored
Applications. Most of fg-manifold's dynamic surface area — preview
environments, per-service apps — is authored as ApplicationSets, so
leaving them unexpanded produces a large false-negative hole.

## Decision

1. **Offline-expandable generators are expanded in pkg/ir**. The
   synthetic Applications are inserted into `World.ArgoApps` alongside
   authored ones. Every downstream rule (R15, R16, R18, R20, …) sees
   them without any rule-specific plumbing. `ExpandAppSet(as,
   ExpansionContext)` returns `{Applications, Diagnostics}`; the builder
   runs expansion between document ingestion and the enrichment passes
   so trajectory / field-validation / render hooks all see the expanded
   fleet.

2. **Hand-rolled `{{ .key }}` substitution, not `text/template`**.
   Argo's real controller uses Go's text/template plus Sprig. Reusing
   it would couple us to the exact Sprig function set the team's
   fg-manifold runtime has, and that set drifts. Instead
   `pkg/ir/appset_template.go` implements a minimal literal-replace
   engine for plain placeholders. Any template containing a range,
   conditional, pipeline, or Sprig helper is treated as opaque: the
   offending Application is skipped and one `XPC.H.appset-unsupported-generator`
   info diagnostic is emitted. That's a clean coverage gap rather than
   a silent half-render.

3. **PullRequest / scmProvider use a fixture file**. The CLI accepts
   `--appset-fixture=<file.yaml>` with shape
   `{appset-name: [{key: value, …}]}`. Each entry stands in for one
   live PR. Without the flag we emit one
   `XPC.H.appset-unsupported-generator` info diagnostic per
   remote-API generator and move on.

   Rationale:
     - Tests stay hermetic.
     - CI jobs that do have GitLab credentials can produce their own
       fixture with a preflight script.
     - The coverage gap is visible (an info diag) rather than a silent
       mismatch between xpc and what the controller would produce.

4. **`--skip-appset-expand` exists**. Expansion costs a filesystem walk
   per git-directories generator and some string substitution; for a
   full-repo run with dozens of AppSets it's noticeable. The flag lets
   slow CI jobs opt out while keeping the default behaviour opt-in.

## Consequences

- Downstream rules gain ApplicationSet coverage with zero per-rule code
  changes. The integration-point test
  `TestAppSetExpansion_PropagatesToR15` is the capstone proof.
- PR-based generators depend on a fixture file that someone has to
  curate. In practice we plan to emit that fixture from a short script
  that queries the GitLab API during the MR-check pipeline — i.e.
  xpc stays offline but gets the data it needs from a trusted place.
- Template features beyond `{{ .key }}` are not rendered. If fg-manifold
  grows a chart of truly range-based ApplicationSets, we'll need to
  reconsider — but today's usage is confined to simple substitutions
  and the gap is tracked explicitly by the info diagnostic.

## Alternatives considered

- **Shell out to `argocd appset generate` directly**. Rejected:
  introduces a hard dependency on argocd being on PATH and needs a
  live Argo CD server for some generators. Defeats the offline model.
- **Full `text/template` + Sprig**. Rejected: chasing Sprig drift
  across fg-manifold's controller versions makes the checker's
  behaviour non-deterministic across environments.
- **Record-and-replay from a live cluster**. Attractive for the PR
  case but shifts the trust boundary onto whoever captured the
  recording. The fixture approach gives the same benefit with a
  smaller surface area.

## Related

- Plan lines 342–402 of `plans/research-written-wiggly-nova.md`.
- Rule R15 (XPC.D.kind-whitelisted) and R16
  (XPC.E.selector-needs-ignore-diff) are the primary beneficiaries.
- ADR-001 (bounded obligation taxonomy) assigns category H to render
  and related coverage, which is where the `appset-unsupported-generator`
  info diag lives.
